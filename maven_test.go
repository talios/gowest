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
