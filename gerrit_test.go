package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
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
