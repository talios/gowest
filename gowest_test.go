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
	assertProjectPath(t, Change{Project: "test-project", ID: "I1234"}, PatchSet{Number: 1}, "test-project/HEAD-I1234-1")
	assertProjectPath(t, Change{Project: "test-project", ID: "I1234", Branch: "master"}, PatchSet{Number: 1}, "test-project/master-I1234-1")
	assertProjectPath(t, Change{Project: "test-project", ID: "I1234", Branch: "master", Topic: "my-topic"}, PatchSet{Number: 1}, "test-project/master-my-topic-I1234-1")
}

func assertProjectPath(t *testing.T, change Change, patchSet PatchSet, expected string) {
	assert.Equal(t, expected, getProjectSubDirectory(&change, &patchSet), "Expected path not found")
}
