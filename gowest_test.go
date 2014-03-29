package main

import (
	"testing"
)

func TestPlainProjectDirectory(t *testing.T) {
	assertProjectPath(t, Change{Project: "test-project"}, "test-project/HEAD")
	assertProjectPath(t, Change{Project: "test-project", Branch: "master"}, "test-project/master")
	assertProjectPath(t, Change{Project: "test-project", Branch: "master", Topic: "my-topic"}, "test-project/master-my-topic")
}

func assertProjectPath(t *testing.T, change Change, expected string) {
	path := getProjectSubDirectory(&change)
	if path != (expected) {
		t.Errorf("expected %s, path was: %s", expected, path)
	}
}
