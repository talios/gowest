package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"golang.org/x/crypto/ssh"
)

// gerritSplit is a port of Gerrit's CommandFactoryProvider.split, which gerrit uses to turn an
// ssh command line into arguments.
func gerritSplit(commandLine string) []string {
	var list []string
	inQuote, inDblQuote := false, false
	var r strings.Builder
	chars := []rune(commandLine)
	for ip := 0; ip < len(chars); {
		b := chars[ip]
		ip++
		switch b {
		case '\t', ' ':
			if inQuote || inDblQuote {
				r.WriteRune(b)
			} else if r.Len() > 0 {
				list = append(list, r.String())
				r.Reset()
			}
		case '"':
			if inQuote {
				r.WriteRune(b)
			} else {
				inDblQuote = !inDblQuote
			}
		case '\'':
			if inDblQuote {
				r.WriteRune(b)
			} else {
				inQuote = !inQuote
			}
		case '\\':
			if inQuote || ip == len(chars) {
				r.WriteRune(b)
			} else {
				r.WriteRune(chars[ip])
				ip++
			}
		default:
			r.WriteRune(b)
		}
	}
	if r.Len() > 0 {
		list = append(list, r.String())
	}
	return list
}

func TestReviewCommandPreservesMessage(t *testing.T) {
	messages := []string{
		"oh hai - you much legend!",
		"it's broken",
		`say "hello"`,
		`" --submit "`,
		"' --submit '",
		`back\slash and trailing \`,
		"multi\nline\toutput",
		"$(rm -rf /) `id` $HOME",
	}
	for _, message := range messages {
		args := gerritSplit(reviewCommand("abc123", "0", "-1", "-1", message))
		assert.Equal(t, []string{"gerrit", "review", "-t", "gowest", "-m", message,
			"--verified", "-1", "--merge", "-1", "--code-review", "0", "abc123"}, args, "message %q", message)
	}
}

func TestReviewCommandOmitsEmptyMessage(t *testing.T) {
	args := gerritSplit(reviewCommand("abc123", "+1", "+1", "+1", ""))
	assert.Equal(t, []string{"gerrit", "review", "-t", "gowest",
		"--verified", "+1", "--merge", "+1", "--code-review", "+1", "abc123"}, args)
}

func TestReviewCommandLeavesEmptyScoresUnchanged(t *testing.T) {
	args := gerritSplit(reviewCommand("abc123", "0", "", "0", "Verifying build..."))
	assert.Equal(t, []string{"gerrit", "review", "-t", "gowest", "-m", "Verifying build...",
		"--merge", "0", "--code-review", "0", "abc123"}, args)
}

func TestReviewJSONLeavesEmptyScoresUnchanged(t *testing.T) {
	input, err := reviewJSON("0", "", "0", "x", nil)
	assert.NoError(t, err)
	assert.JSONEq(t, `{"message": "x", "tag": "gowest", "labels": {"Code-Review": 0, "Merge": 0}, "omit_duplicate_comments": true}`, string(input))
}

func TestReviewJSON(t *testing.T) {
	input, err := reviewJSON("0", "-1", "+1", "it broke", map[string][]reviewComment{
		"src/Foo.java": {{Line: 18, Message: "cannot find symbol", Unresolved: true}},
	})
	assert.NoError(t, err)
	assert.JSONEq(t, `{
		"message": "it broke",
		"tag": "gowest",
		"labels": {"Code-Review": 0, "Verified": -1, "Merge": 1},
		"comments": {"src/Foo.java": [{"line": 18, "message": "cannot find symbol", "unresolved": true}]},
		"omit_duplicate_comments": true
	}`, string(input))

	_, err = reviewJSON("0", "nope", "0", "x", nil)
	assert.Error(t, err)
}

func TestSendReviewFallsBackWithoutInlineComments(t *testing.T) {
	s := ServerDetails{}
	request := reviewRequest{revision: "abc123", message: "it broke", command: "gerrit review --json abc123", stdin: []byte("{}"), plainCommand: "gerrit review -m 'it broke' abc123"}

	var posted []string
	s.sendReviewUsing(request, func(command string, stdin []byte) ([]byte, error) {
		posted = append(posted, command)
		if stdin != nil {
			return []byte("fatal: file src/Foo.java not found in revision 1,1"), &ssh.ExitError{}
		}
		return nil, nil
	})
	assert.Equal(t, []string{"gerrit review --json abc123", "gerrit review -m 'it broke' abc123"}, posted)
}

func TestSendReviewDoesNotRetryRejectedPlainReview(t *testing.T) {
	s := ServerDetails{}
	posts := 0
	s.sendReviewUsing(reviewRequest{command: "gerrit review abc123"}, func(string, []byte) ([]byte, error) {
		posts++
		return nil, &ssh.ExitError{}
	})
	assert.Equal(t, 1, posts)
}

func TestHighestApprovals(t *testing.T) {
	output := []byte(`{"number":42,"patchSets":[` +
		`{"number":1,"revision":"old","approvals":[{"type":"Verified","value":"2"}]},` +
		`{"number":2,"revision":"abc123","approvals":[` +
		`{"type":"Code-Review","value":"1"},{"type":"Code-Review","value":"2"},{"type":"Verified","value":"-1"},{"type":"merge","value":"2"}]}]}
{"type":"stats","rowCount":1}
`)
	approvals, err := highestApprovals(output, "abc123")
	assert.NoError(t, err)
	assert.Equal(t, map[string]int{"code-review": 2, "verified": -1, "merge": 2}, approvals)

	assert.Equal(t, "0", successScore(approvals, "Code-Review"))
	assert.Equal(t, "+1", successScore(approvals, "Verified"))
	assert.Equal(t, "0", successScore(approvals, "Merge"))
	assert.Equal(t, "+1", successScore(nil, "Merge"))
}
