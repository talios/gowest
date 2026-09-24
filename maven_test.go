package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBuild(t *testing.T) {

	server := ServerDetails{Username: "markd", Keyfile: "/Users/amrk/.ssh/id_rsa", Location: "gerrit.smxemail.com:29418"}
	project := BuildProject{Name: "dev/smx3/smx3-monitoring", Version: "1.1", ProjectPath: "/tmp/gowest/workspace/dev/smx3/smx3-monitoring/develop-I1ed4922f4183d71442024e1c645709db36a15d1e-5", Server: server}

	event := Event{
		PatchSet: PatchSet{Number: 29201, Revision: "3"},
	}
	project.buildMaven(event)

}

func TestPomParsing(t *testing.T) {

	var xmlStr = `
	<?xml version="1.0" encoding="UTF-8"?>
	<project
			xmlns="http://maven.apache.org/POM/4.0.0"
			xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"
			xsi:schemaLocation="http://maven.apache.org/POM/4.0.0 http://maven.apache.org/xsd/maven-4.0.0.xsd">

		<modelVersion>4.0.0</modelVersion>

		<groupId>com.smxemail</groupId>
		<artifactId>selfrelease</artifactId>
		<version>1.0.1-SNAPSHOT</version>
		<packaging>jar</packaging>
		<name>Azure Java Functions</name>
	</project>`

	pomProject, _ := resolvePomProjectFromString(xmlStr)
	assert.Equal(t, pomProject.ArtifactId, "selfrelease", "Invalid artifact id")

}

func TestTailWriterKeepsLastCompleteLines(t *testing.T) {
	w := newTailWriter(64)
	for i := 0; i < 100; i++ {
		fmt.Fprintf(w, "line %03d\n", i)
	}
	tail := w.String()
	assert.LessOrEqual(t, len(tail), 64)
	assert.True(t, strings.HasPrefix(tail, "line "), "tail should start on a complete line: %q", tail)
	assert.True(t, strings.HasSuffix(tail, "line 099\n"))
}

func TestTailWriterConcurrentWrites(t *testing.T) {
	w := newTailWriter(1024)
	var wg sync.WaitGroup
	for g := 0; g < 4; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 1000; i++ {
				w.Write([]byte("output\n"))
			}
		}()
	}
	wg.Wait()
	assert.LessOrEqual(t, len(w.String()), 1024)
}

func TestFormatBuildOutputTail(t *testing.T) {
	output := "\n[INFO] one\r\n\x1b[1;31m[ERROR]\x1b[m two\n\n[ERROR] three  \n\n\n"
	assert.Equal(t, "    [INFO] one\n    [ERROR] two\n\n    [ERROR] three\n", formatBuildOutputTail(output, 10))
	assert.Equal(t, "    [ERROR] two\n\n    [ERROR] three\n", formatBuildOutputTail(output, 3))
	assert.Equal(t, "", formatBuildOutputTail("\n  \n", 10))
}

func TestBuildWithCapturesOutputOnFailure(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "build.sh")
	os.WriteFile(script, []byte("#!/bin/sh\necho \"[INFO] compiling\"\necho \"[ERROR] it broke\"\necho \"sh: oops\" >&2\nprintf \"[ERROR] no newline\"\nexit 1\n"), 0755)

	project := &BuildProject{Name: "test", ProjectPath: dir, RepoType: "local", BuildCommand: script}
	output := newBuildOutput()
	err := buildWith(context.Background(), project, Event{}, output)

	assert.Error(t, err)
	assert.Equal(t, "Errors from build output:\n\n    [ERROR] it broke\n    [ERROR] no newline\n", output.failureSummary())
}

func TestFailureSummaryFallsBackToTail(t *testing.T) {
	output := newBuildOutput()
	fmt.Fprint(output.stdoutWriter(), "[INFO] compiling\ncommand not found\n")
	output.flush()
	assert.Equal(t, "Last lines of build output:\n\n    [INFO] compiling\n    command not found\n", output.failureSummary())

	assert.Equal(t, "", newBuildOutput().failureSummary())
}

func TestLineWriterHandlesSplitAndColouredLines(t *testing.T) {
	c := newErrorLineCollector(1024)
	w := &lineWriter{onLine: c.addIfError}
	w.Write([]byte("[INFO] fine\n[ERR"))
	w.Write([]byte("OR] split across writes\r\n\x1b[1;31m[ERROR]\x1b[m coloured\n[ERROR]\n[ERROR] "))
	w.Write([]byte("\n[WARNING] not an error\n"))
	w.flush()
	assert.Equal(t, []string{"[ERROR] split across writes", "[ERROR] coloured"}, c.lines())
}

func TestLineWriterTruncatesLongLines(t *testing.T) {
	c := newErrorLineCollector(4096)
	w := &lineWriter{onLine: c.addIfError}
	fmt.Fprintf(w, "[ERROR] %s\n", strings.Repeat("x", 2000))
	assert.Len(t, c.lines()[0], maxBuildLineBytes)
}

func TestErrorLineCollectorKeepsHeadAndTail(t *testing.T) {
	c := newErrorLineCollector(200)
	for i := 0; i < 100; i++ {
		c.addIfError(fmt.Sprintf("[ERROR] line %02d", i))
	}
	lines := c.lines()
	assert.Equal(t, "[ERROR] line 00", lines[0])
	assert.Equal(t, "[ERROR] line 99", lines[len(lines)-1])

	var marker string
	kept := 0
	for _, line := range lines {
		if strings.HasPrefix(line, "...") {
			marker = line
		} else {
			kept++
		}
	}
	assert.Equal(t, fmt.Sprintf("... %d more [ERROR] lines ...", 100-kept), marker)
	assert.LessOrEqual(t, len(formatPreformatted(lines)), 200+len(marker)+5)
}

func TestErrorLineCollectorDropsMavenBoilerplate(t *testing.T) {
	output := `[ERROR] Reproducible Build not activated by project.build.outputTimestamp property: see https://maven.apache.org/guides/mini/guide-reproducible-builds.html
[ERROR] Reproducible Build not activated by project.build.outputTimestamp property: see https://maven.apache.org/guides/mini/guide-reproducible-builds.html
[ERROR] COMPILATION ERROR :
[ERROR] /src/main/java/Foo.java:[12,5] cannot find symbol
[ERROR] Failed to execute goal org.apache.maven.plugins:maven-compiler-plugin:3.11.0:compile (default-compile) on project foo: Compilation failure -> [Help 1]
[ERROR] -> [Help 1]
[ERROR] 
[ERROR] To see the full stack trace of the errors, re-run Maven with the -e switch.
[ERROR] Re-run Maven using the -X switch to enable full debug logging.
[ERROR] 
[ERROR] For more information about the errors and possible solutions, please read the following articles:
[ERROR] [Help 1] http://cwiki.apache.org/confluence/display/MAVEN/MojoFailureException
[ERROR] 
[ERROR] After correcting the problems, you can resume the build with the command
[ERROR]   mvn <args> -rf :foo
[ERROR] To see the full stack trace of the errors, re-run Maven with the '-e' switch
[ERROR] Re-run Maven using the '-X' switch to enable verbose output
`
	c := newErrorLineCollector(4096)
	w := &lineWriter{onLine: c.addIfError}
	fmt.Fprint(w, output)
	assert.Equal(t, []string{
		"[ERROR] COMPILATION ERROR :",
		"[ERROR] /src/main/java/Foo.java:[12,5] cannot find symbol",
		"[ERROR] Failed to execute goal org.apache.maven.plugins:maven-compiler-plugin:3.11.0:compile (default-compile) on project foo: Compilation failure",
	}, c.lines())
}

// Output from a real mvnd build, which reports each compile error twice.
const mvndCompileFailure = `[INFO] Compiling 1 source file with javac [debug release 17] to target/classes
[INFO] -------------------------------------------------------------
[ERROR] COMPILATION ERROR : 
[INFO] -------------------------------------------------------------
[ERROR] /repo/src/main/java/Foo.java:[1,31] cannot find symbol
  symbol:   method nope()
  location: class Foo
[INFO] 1 error
[INFO] -------------------------------------------------------------
[INFO] BUILD FAILURE
[ERROR] Failed to execute goal org.apache.maven.plugins:maven-compiler-plugin:3.15.0:compile (default-compile) on project broken: Compilation failure
[ERROR] /repo/src/main/java/Foo.java:[1,31] cannot find symbol
[ERROR]   symbol:   method nope()
[ERROR]   location: class Foo
[ERROR] -> [Help 1]
`

func TestCompileErrorCollectorDropsRepeats(t *testing.T) {
	output := newBuildOutput()
	fmt.Fprint(output.stdoutWriter(), mvndCompileFailure)
	output.flush()

	errors := output.compileErrors.list()
	assert.Equal(t, []compileError{{
		path:    "/repo/src/main/java/Foo.java",
		line:    1,
		message: "cannot find symbol",
		details: []string{"symbol:   method nope()", "location: class Foo"},
	}}, errors)
	assert.Equal(t, "cannot find symbol\n\n    symbol:   method nope()\n    location: class Foo", errors[0].commentMessage())
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	output, err := gitOutput(dir, append([]string{"-c", "user.name=test", "-c", "user.email=test@example.com"}, args...)...)
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(output)
}

func TestInlineCommentsOnlyForFilesTheRevisionChangedAndBuilt(t *testing.T) {
	// t.TempDir is under a symlink on macOS (/var -> /private/var), like maven's /private/tmp paths.
	repo := t.TempDir()
	write := func(name string, content string) {
		os.MkdirAll(filepath.Dir(filepath.Join(repo, name)), 0755)
		os.WriteFile(filepath.Join(repo, name), []byte(content), 0644)
	}
	runGit(t, repo, "init", "-q")
	write("src/Unchanged.java", "class Unchanged {}\n")
	write("src/Changed.java", "class Changed {}\n")
	write("src/Merged.java", "class Merged {}\n")
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-q", "-m", "base")

	write("src/Changed.java", "class Changed { void x() { nope(); } }\n")
	write("src/Merged.java", "class Merged { void x() { nope(); } }\n")
	write("src/Added.java", "class Added { void x() { nope(); } }\n")
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-q", "-m", "patchset")
	revision := runGit(t, repo, "rev-parse", "HEAD")

	// As if the merge with the branch changed Merged.java after checking out the revision.
	write("src/Merged.java", "// from the branch\nclass Merged { void x() { nope(); } }\n")

	resolved, _ := filepath.EvalSymlinks(repo)
	errorIn := func(name string, line int) compileError {
		return compileError{path: filepath.Join(resolved, name), line: line, message: "cannot find symbol " + name}
	}
	comments := inlineComments(repo, revision, []compileError{
		errorIn("src/Changed.java", 1),
		errorIn("src/Added.java", 1),
		errorIn("src/Unchanged.java", 1),
		errorIn("src/Merged.java", 2),
		{path: "/somewhere/else/Other.java", line: 1, message: "outside the repo"},
		errorIn("src/Missing.java", 1),
	})

	assert.Equal(t, map[string][]reviewComment{
		"src/Changed.java": {{Line: 1, Message: "cannot find symbol src/Changed.java", Unresolved: true}},
		"src/Added.java":   {{Line: 1, Message: "cannot find symbol src/Added.java", Unresolved: true}},
	}, comments)
}

func TestInlineCommentsSkipsMergeRevisions(t *testing.T) {
	repo := t.TempDir()
	runGit(t, repo, "init", "-q", "-b", "main")
	os.WriteFile(filepath.Join(repo, "A.java"), []byte("a\n"), 0644)
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-q", "-m", "base")
	runGit(t, repo, "checkout", "-q", "-b", "side")
	os.WriteFile(filepath.Join(repo, "B.java"), []byte("b\n"), 0644)
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-q", "-m", "side")
	runGit(t, repo, "checkout", "-q", "main")
	os.WriteFile(filepath.Join(repo, "A.java"), []byte("a2\n"), 0644)
	runGit(t, repo, "commit", "-q", "-am", "main")
	runGit(t, repo, "merge", "-q", "--no-edit", "side")
	revision := runGit(t, repo, "rev-parse", "HEAD")

	resolved, _ := filepath.EvalSymlinks(repo)
	comments := inlineComments(repo, revision, []compileError{{path: filepath.Join(resolved, "B.java"), line: 1, message: "x"}})
	assert.Empty(t, comments)
}
