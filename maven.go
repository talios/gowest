package main

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/Unknwon/goconfig"
	"github.com/gen2brain/beeep"
	log "github.com/sirupsen/logrus"
)

type BuildProject struct {
	Name         string
	Version      string
	ProjectPath  string
	RepoType     string
	BuildCommand string
	Server       ServerDetails
}

type MavenProject struct {
	BuildProject
}

type PomProject struct {
	XMLName        xml.Name `xml:"project"`
	Text           string   `xml:",chardata"`
	Xmlns          string   `xml:"xmlns,attr"`
	Xsi            string   `xml:"xsi,attr"`
	SchemaLocation string   `xml:"schemaLocation,attr"`
	ModelVersion   string   `xml:"modelVersion"`
	GroupId        string   `xml:"groupId"`
	ArtifactId     string   `xml:"artifactId"`
	Version        string   `xml:"version"`
	Packaging      string   `xml:"packaging"`
	Name           string   `xml:"name"`
}

// BuildProcess Enum type for build status
type BuildProcess int

const (
	// Success The build was successful
	Success BuildProcess = iota
	// Failure The build failed
	Failure
)

func isGitRepo(projectPath string) bool {
	if _, err := os.Stat(projectPath + "/.git"); err == nil {
		return true
	}
	return false
}

// resolveProject reads the project's build settings from its config section, e.g. [project.foo].
func resolveProject(config *goconfig.ConfigFile, projectKey string, server ServerDetails, projectPath string) (*BuildProject, error) {
	if isMavenProject(projectPath) {

		pomProject, err := resolvePomProject(projectPath)
		if err != nil {
			return nil, err
		}

		defaultType := "owner"
		repoType, err := config.GetValue(projectKey, "repo")

		if err != nil {
			repoType = defaultType
		}

		defaultBuildCommand := "mvn clean verify"
		buildCommand, err := config.GetValue(projectKey, "build-command")

		if err != nil {
			buildCommand = defaultBuildCommand
		}

		return &BuildProject{
			Name: pomProject.Name, Version: pomProject.Version, ProjectPath: projectPath,
			RepoType: repoType, BuildCommand: buildCommand, Server: server,
		}, nil
	}
	return nil, errors.New("unknown project type")
}

func isMavenProject(projectPath string) bool {
	if _, err := os.Stat(projectPath + "/pom.xml"); err == nil {
		return true
	}
	return false
}

func resolvePomProject(projectPath string) (*PomProject, error) {
	fullPomFile := projectPath + "/pom.xml"
	_, err := os.Stat(fullPomFile)
	if err != nil {
		log.Error("Missing pom.xml")
		return nil, err
	}
	xml, err := os.ReadFile(fullPomFile)
	if err != nil {
		log.Error("Unable to read pom.xml")
		return nil, err
	}
	return resolvePomProjectFromString(string(xml))
}

func resolvePomProjectFromString(pom string) (*PomProject, error) {
	var pomProject PomProject
	data := []byte(pom)
	err := xml.Unmarshal(data, &pomProject)
	if err != nil {
		log.Error("Unable to parse pom.xml")
		return nil, err
	}
	return &pomProject, nil

}

func (project *BuildProject) buildMaven(event Event) {
	ctx := context.Background()

	msg := "Building " + event.Project + " change " + event.Change.ID + "@" + event.PatchSet.Revision
	beeep.Notify("GoWest", msg, "assets/information.png")

	verifiedScore := ""
	if project.Server.ClearVerificationLabel {
		verifiedScore = "-1"
	}
	project.Server.ReviewGerrit(event.PatchSet.Revision, "0", verifiedScore, "0", "Verifying build...")

	processChannel := make(chan BuildProcess)
	output := newBuildOutput()
	var buildErr error

	go func() {
		// buildErr and output are only read after the channel receive below.
		if buildErr = buildWith(ctx, project, event, output); buildErr != nil {
			processChannel <- Failure
		} else {
			processChannel <- Success
		}
	}()

	select {
	case processStatus := <-processChannel:
		switch processStatus {
		case Success:
			approvals, err := project.Server.HighestApprovals(event.Change.Number, event.PatchSet.Revision)
			if err != nil {
				log.WithError(err).WithField("changeID", event.Change.ID).Warn("Unable to fetch existing votes, voting +1 on every label")
			}
			project.Server.ReviewGerrit(event.PatchSet.Revision, successScore(approvals, "Code-Review"),
				successScore(approvals, "Verified"), successScore(approvals, "Merge"), "Build Verification Success!")
		case Failure:
			message := "Build Verification Failure!\n\n" + buildErr.Error()
			if summary := output.failureSummary(); summary != "" {
				message += "\n\n" + summary
			}
			comments := inlineComments(project.ProjectPath, event.PatchSet.Revision, output.compileErrors.list())
			project.Server.ReviewGerritWithComments(event.PatchSet.Revision, "0", "-1", "-1", message, comments)
		}
		break
		//	case gerritEvent := <-eventChannel:
		//		if isUpdatedPatchset(event, gerritEvent) {
		//			log.Printf("Cancelling build of patchset %d of change %s on project %s.", event.PatchSet.Number, event.Change.ID, event.Change.Project)
		//			// server.ReviewGerrit(event.PatchSet.Revision, "0", "0", "-1", "such sadness - much building aborted :(!")
		//		}
	}
}

// successScore is the vote for a label on a successful build: +1, unless someone has already
// given it a +2. That gets 0 rather than being left out, so it still clears any "Verifying" -1.
func successScore(approvals map[string]int, label string) string {
	if approvals[strings.ToLower(label)] >= 2 {
		return "0"
	}
	return "+1"
}

// buildWith runs the project's build command, copying its output to both the terminal and output.
func buildWith(ctx context.Context, project *BuildProject, event Event, output *buildOutput) error {

	commandParts := strings.Fields(project.BuildCommand)
	if len(commandParts) == 0 {
		return errors.New("empty build command")
	}

	// The "local" repo type passes no flag, leaving maven to use its own default (or whatever
	// localRepository settings.xml specifies). The command runs without a shell, so a "~" path
	// here would never be expanded.
	var args []string
	if project.RepoType == "project" {
		args = append(args, fmt.Sprintf("-Dmaven.repo.local=/tmp/gowest/repo/%s", project.Name))
	}
	if project.RepoType == "change" {
		args = append(args, fmt.Sprintf("-Dmaven.repo.local=/tmp/gowest/repo/%s", event.Change.ID))
	}
	if project.RepoType == "owner" {
		args = append(args, fmt.Sprintf("-Dmaven.repo.local=/tmp/gowest/repo/%s", event.Change.Owner.Username))
	}

	args = append(args, commandParts[1:]...)
	mvnCmd := exec.Command(commandParts[0], args...)
	mvnCmd.Dir = project.ProjectPath
	mvnCmd.Stderr = io.MultiWriter(os.Stderr, output.stderrWriter())
	mvnCmd.Stdout = io.MultiWriter(os.Stdout, output.stdoutWriter())
	// Once Wait returns nothing more is written, so any unterminated last line can be collected.
	defer output.flush()

	if err := mvnCmd.Start(); err != nil {
		return err
	}

	if err := mvnCmd.Wait(); err != nil {
		return err
	} else {
		return nil
	}
}

const (
	// Gerrit rejects messages over change.commentSizeLimit (16KB by default), so the review only
	// gets maven's [ERROR] lines, or failing those the end of the build output, within 8KB.
	buildOutputTailBytes = 8 * 1024
	buildOutputTailLines = 60
	buildErrorLinesBytes = 8 * 1024
	// Longer lines are cut short, so no single line can use up the error budget.
	maxBuildLineBytes = 500
	// Beyond this many compile errors the rest are usually knock-on errors, so aren't commented on.
	maxCompileErrors = 50
)

// buildOutput captures what the review needs from a build's output: the [ERROR] lines, and the
// tail of the output as a fallback for builds that print none.
type buildOutput struct {
	tail           *tailWriter
	errors         *errorLineCollector
	compileErrors  *compileErrorCollector
	stdout, stderr *lineWriter
}

func newBuildOutput() *buildOutput {
	o := &buildOutput{
		tail:          newTailWriter(buildOutputTailBytes),
		errors:        newErrorLineCollector(buildErrorLinesBytes),
		compileErrors: &compileErrorCollector{seen: map[string]bool{}},
	}
	onLine := func(line string) {
		o.errors.addIfError(line)
		o.compileErrors.add(line)
	}
	// stdout and stderr each get their own lineWriter, so lines from the two never interleave.
	o.stdout = &lineWriter{onLine: onLine}
	o.stderr = &lineWriter{onLine: onLine}
	return o
}

func (o *buildOutput) stdoutWriter() io.Writer { return io.MultiWriter(o.tail, o.stdout) }
func (o *buildOutput) stderrWriter() io.Writer { return io.MultiWriter(o.tail, o.stderr) }

func (o *buildOutput) flush() {
	o.stdout.flush()
	o.stderr.flush()
}

// failureSummary formats the build output for the failure review, or returns "" if there was none.
func (o *buildOutput) failureSummary() string {
	if lines := o.errors.lines(); len(lines) > 0 {
		return "Errors from build output:\n\n" + formatPreformatted(lines)
	}
	if tail := formatBuildOutputTail(o.tail.String(), buildOutputTailLines); tail != "" {
		return "Last lines of build output:\n\n" + tail
	}
	return ""
}

// lineWriter splits what is written to it into lines, passing each complete line to onLine.
// It is not safe for concurrent writes; exec copies each output stream from a single goroutine.
type lineWriter struct {
	partial []byte
	onLine  func(string)
}

func (w *lineWriter) Write(p []byte) (int, error) {
	n := len(p)
	for {
		i := bytes.IndexByte(p, '\n')
		if i < 0 {
			w.appendPartial(p)
			return n, nil
		}
		w.appendPartial(p[:i])
		w.onLine(string(w.partial))
		w.partial = w.partial[:0]
		p = p[i+1:]
	}
}

func (w *lineWriter) appendPartial(p []byte) {
	if room := maxBuildLineBytes - len(w.partial); room > 0 {
		if len(p) > room {
			p = p[:room]
		}
		w.partial = append(w.partial, p...)
	}
}

func (w *lineWriter) flush() {
	if len(w.partial) > 0 {
		w.onLine(string(w.partial))
		w.partial = w.partial[:0]
	}
}

/*
 * errorLineCollector keeps maven's [ERROR] lines within maxBytes of formatted message. Once full
 * it keeps the first half - usually the root cause, such as the compile errors or failing tests -
 * and the last half - maven's failure summary - and counts the lines dropped in between.
 */
type errorLineCollector struct {
	mu        sync.Mutex
	maxBytes  int
	skipNext  bool
	head      []string
	headBytes int
	tail      []string
	tailBytes int
	omitted   int
}

func newErrorLineCollector(maxBytes int) *errorLineCollector {
	return &errorLineCollector{maxBytes: maxBytes}
}

// cleanBuildLine strips colour codes, invalid UTF-8 and trailing whitespace from a line of output.
func cleanBuildLine(line string) string {
	return strings.TrimRight(strings.ToValidUTF8(ansiEscape.ReplaceAllString(line, ""), ""), "\r \t")
}

// addIfError collects line if it is a maven [ERROR] line.
func (c *errorLineCollector) addIfError(line string) {
	line = cleanBuildLine(line)
	if !strings.HasPrefix(line, "[ERROR]") || line == "[ERROR]" {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	message := strings.TrimSpace(strings.TrimPrefix(line, "[ERROR]"))
	if c.skipNext {
		// The line after "After correcting the problems..." is the resume command itself.
		c.skipNext = false
		return
	}
	if mavenResumeHint.MatchString(message) {
		c.skipNext = true
		return
	}
	if isMavenBoilerplate(message) {
		return
	}
	line = mavenHelpSuffix.ReplaceAllString(line, "")
	if line == "[ERROR]" {
		// mvnd prints "-> [Help 1]" on a line of its own.
		return
	}
	// Count the indent and newline formatPreformatted adds, so maxBytes bounds the message itself.
	size := len(line) + 5
	half := c.maxBytes / 2
	if len(c.tail) == 0 && c.headBytes+size <= half {
		c.head = append(c.head, line)
		c.headBytes += size
		return
	}
	c.tail = append(c.tail, line)
	c.tailBytes += size
	for c.tailBytes > half {
		c.tailBytes -= len(c.tail[0]) + 5
		c.tail = c.tail[1:]
		c.omitted++
	}
}

var (
	// Lines maven logs as errors that say nothing about why this build failed: the advice printed
	// after every failure, and the per-module reproducible build warning.
	mavenBoilerplate = []*regexp.Regexp{
		regexp.MustCompile(`^To see the full stack trace of the errors, re-run Maven with the`),
		regexp.MustCompile(`^Re-run Maven using the '?-X'? switch`),
		regexp.MustCompile(`^For more information about the errors and possible solutions, please read the following articles`),
		regexp.MustCompile(`^\[Help \d+\] `),
		regexp.MustCompile(`^Reproducible Build not activated by project\.build\.outputTimestamp property`),
	}
	mavenResumeHint = regexp.MustCompile(`^After correcting the problems, you can resume the build with the command`)
	mavenHelpSuffix = regexp.MustCompile(`\s*-> \[Help \d+\]$`)
)

func isMavenBoilerplate(message string) bool {
	for _, pattern := range mavenBoilerplate {
		if pattern.MatchString(message) {
			return true
		}
	}
	return false
}

func (c *errorLineCollector) lines() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	lines := append([]string(nil), c.head...)
	if c.omitted > 0 {
		lines = append(lines, fmt.Sprintf("... %d more [ERROR] lines ...", c.omitted))
	}
	return append(lines, c.tail...)
}

// tailWriter keeps only the last maxBytes written to it. It is safe for concurrent writes, as
// the build's stdout and stderr are copied into the same tailWriter.
type tailWriter struct {
	mu       sync.Mutex
	maxBytes int
	buf      []byte
}

func newTailWriter(maxBytes int) *tailWriter {
	return &tailWriter{maxBytes: maxBytes}
}

func (w *tailWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf = append(w.buf, p...)
	// Trim only once the buffer doubles, so large outputs aren't copied on every write.
	if len(w.buf) > 2*w.maxBytes {
		w.buf = append([]byte(nil), w.buf[len(w.buf)-w.maxBytes:]...)
	}
	return len(p), nil
}

func (w *tailWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.buf) <= w.maxBytes {
		return string(w.buf)
	}
	tail := w.buf[len(w.buf)-w.maxBytes:]
	// The cut usually lands mid-line, so start from the first complete line.
	if i := bytes.IndexByte(tail, '\n'); i >= 0 {
		tail = tail[i+1:]
	}
	return string(tail)
}

var ansiEscape = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]`)

/*
 * Format the last maxLines of build output for a gerrit message: colour codes are stripped, and
 * each line is indented so gerrit renders the block as preformatted text. Returns "" when there
 * is no output.
 */
func formatBuildOutputTail(output string, maxLines int) string {
	output = strings.ToValidUTF8(ansiEscape.ReplaceAllString(output, ""), "")
	lines := strings.Split(output, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, "\r \t")
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}
	for len(lines) > 0 && lines[0] == "" {
		lines = lines[1:]
	}
	return formatPreformatted(lines)
}

// formatPreformatted indents each line so gerrit renders the block as preformatted text.
func formatPreformatted(lines []string) string {
	var formatted strings.Builder
	for _, line := range lines {
		if line != "" {
			formatted.WriteString("    " + line)
		}
		formatted.WriteString("\n")
	}
	return formatted.String()
}

// compileError is a compiler error against a file and line, e.g. javac's
// "[ERROR] /path/Foo.java:[18,31] cannot find symbol", with any detail lines that follow it.
type compileError struct {
	path    string
	line    int
	message string
	details []string
}

var (
	compileErrorLine = regexp.MustCompile(`^\[ERROR\] (/.+?):\[(\d+),(\d+)\] (.+)$`)
	// javac's "  symbol: ..." and "  location: ..." lines, with or without an [ERROR] prefix.
	compileErrorDetail = regexp.MustCompile(`^(?:\[ERROR\])?\s{2,}(\S.*)$`)
)

/*
 * compileErrorCollector collects compile errors from a build's output. Maven reports each one
 * twice - as the compiler runs and again in its failure summary - so repeats are dropped.
 */
type compileErrorCollector struct {
	mu     sync.Mutex
	errors []*compileError
	seen   map[string]bool
	// The error that detail lines belong to; nil after any other line, or after a repeat.
	current *compileError
}

// add collects line if it is part of a compile error.
func (c *compileErrorCollector) add(line string) {
	line = cleanBuildLine(line)
	c.mu.Lock()
	defer c.mu.Unlock()
	if m := compileErrorLine.FindStringSubmatch(line); m != nil {
		c.current = nil
		key := strings.Join(m[1:], ":")
		if c.seen[key] || len(c.errors) >= maxCompileErrors {
			return
		}
		c.seen[key] = true
		lineNumber, _ := strconv.Atoi(m[2])
		c.current = &compileError{path: m[1], line: lineNumber, message: m[4]}
		c.errors = append(c.errors, c.current)
		return
	}
	if m := compileErrorDetail.FindStringSubmatch(line); m != nil && c.current != nil {
		c.current.details = append(c.current.details, m[1])
		return
	}
	c.current = nil
}

func (c *compileErrorCollector) list() []compileError {
	c.mu.Lock()
	defer c.mu.Unlock()
	errors := make([]compileError, len(c.errors))
	for i, e := range c.errors {
		errors[i] = *e
		errors[i].details = append([]string(nil), e.details...)
	}
	return errors
}

// commentMessage formats the error for an inline comment, with its details preformatted.
func (e compileError) commentMessage() string {
	if len(e.details) == 0 {
		return e.message
	}
	return e.message + "\n\n" + strings.TrimRight(formatPreformatted(e.details), "\n")
}

/*
 * Turn compile errors into inline comments on the revision being reviewed, keyed by path within
 * the repository. Gerrit rejects the whole review if a comment is on a file the revision doesn't
 * change, and the build ran on the revision merged with its branch, so an error only becomes a
 * comment when the revision changes its file and the build compiled the revision's version of it.
 * Anything else is only reported in the review message.
 */
func inlineComments(projectPath string, revision string, errors []compileError) map[string][]reviewComment {
	if len(errors) == 0 {
		return nil
	}
	logger := log.WithFields(log.Fields{"path": projectPath, "revision": revision})

	// Maven reports paths with symlinks resolved, e.g. /private/tmp rather than /tmp on macOS.
	root, err := filepath.EvalSymlinks(projectPath)
	if err != nil {
		logger.WithError(err).Warn("Unable to resolve project path, skipping inline comments")
		return nil
	}
	changed, err := revisionChangedFiles(projectPath, revision)
	if err != nil {
		logger.WithError(err).Warn("Unable to list files changed by revision, skipping inline comments")
		return nil
	}

	comments := map[string][]reviewComment{}
	builtFromRevision := map[string]bool{}
	for _, e := range errors {
		path, err := filepath.EvalSymlinks(e.path)
		if err != nil {
			continue
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			continue
		}
		relative = filepath.ToSlash(relative)
		if !changed[relative] {
			continue
		}
		same, checked := builtFromRevision[relative]
		if !checked {
			// The merge with the branch may have changed the file, and with it the line numbers.
			_, err := gitOutput(projectPath, "diff", "--quiet", revision, "--", relative)
			same = err == nil
			builtFromRevision[relative] = same
		}
		if same {
			comments[relative] = append(comments[relative], reviewComment{Line: e.line, Message: e.commentMessage(), Unresolved: true})
		}
	}
	return comments
}

/*
 * List the files a revision changes against its parent - the files gerrit accepts comments on.
 * Merge commits are refused: gerrit compares those against an auto-merge rather than a parent.
 */
func revisionChangedFiles(projectPath string, revision string) (map[string]bool, error) {
	parents, err := gitOutput(projectPath, "rev-list", "--parents", "-n", "1", revision)
	if err != nil {
		return nil, err
	}
	if len(strings.Fields(parents)) != 2 {
		return nil, fmt.Errorf("revision %s does not have exactly one parent", revision)
	}

	names, err := gitOutput(projectPath, "diff", "--name-only", "--no-renames", "-z", revision+"^", revision)
	if err != nil {
		return nil, err
	}
	changed := map[string]bool{}
	for _, name := range strings.Split(names, "\x00") {
		if name != "" {
			changed[name] = true
		}
	}
	return changed, nil
}
