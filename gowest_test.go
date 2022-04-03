package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestPatchSetChanges(t *testing.T) {

	e1 := Event{Change: Change{ID: "I001"}, PatchSet: PatchSet{Number: 1}}
	e2 := Event{Change: Change{ID: "I001"}, PatchSet: PatchSet{Number: 2}}
	e3 := Event{Change: Change{ID: "I002"}, PatchSet: PatchSet{Number: 2}}

	assert.True(t, isUpdatedPatchset(e1, e2), "Second patchset SHOULD be updated.")
	assert.False(t, isUpdatedPatchset(e2, e1), "Second patchset SHOULD NOT be updated.")
	assert.False(t, isUpdatedPatchset(e1, e1), "Second patchset SHOULD NOT be updated.")
	assert.False(t, isUpdatedPatchset(e2, e3), "Second patchset SHOULD NOT be updated.")
}

func TestProjectDirectory(t *testing.T) {
	assertProjectPath(t, Change{Project: "test-project"}, "test-project/HEAD")
	assertProjectPath(t, Change{Project: "test-project", Branch: "master"}, "test-project/master")
	assertProjectPath(t, Change{Project: "test-project", Branch: "master", Topic: "my-topic"}, "test-project/master-my-topic")
}

func assertProjectPath(t *testing.T, change Change, expected string) {
	assert.Equal(t, expected, getProjectSubDirectory(&change), "Expected path not found")
}
