package collector

import (
	"fmt"
	"strings"

	"github.com/sergi/go-diff/diffmatchpatch"
)

const diffMaxOutputLines = 500

// normalizeWhitespace normalizes a string for comparison: leading and trailing
// newlines are removed; each line is trimmed and runs of whitespace (spaces/tabs)
// are collapsed to a single space. This lets tests start the want string with a
// newline for formatting and ignore indentation differences.
func normalizeWhitespace(s string) string {
	s = strings.TrimLeft(s, "\n")
	s = strings.TrimRight(s, "\n")
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = strings.Join(strings.Fields(line), " ")
	}
	return strings.Join(lines, "\n")
}

// cmpOutput compares got and want and returns an empty string if equal, otherwise a diff message.
// Comparison ignores whitespace: lines are trimmed and runs of spaces/tabs are collapsed,
// so you can write the expected string without matching indentation exactly.
// The diff is produced by github.com/sergi/go-diff (DiffPrettyText).
// Use in tests: if msg := cmpOutput(got, want); msg != "" { t.Error(msg) }
func cmpOutput(got, want string) string {
	if normalizeWhitespace(got) == normalizeWhitespace(want) {
		return ""
	}
	dmp := diffmatchpatch.New()
	diffs := dmp.DiffMain(want, got, false)
	diffStr := dmp.DiffPrettyText(diffs)
	if lines := strings.Count(diffStr, "\n"); lines > diffMaxOutputLines {
		split := strings.SplitN(diffStr, "\n", diffMaxOutputLines+1)
		diffStr = strings.Join(split[:diffMaxOutputLines], "\n") + "\n... (truncated)\n"
	}
	return fmt.Sprintf("got:\n%s\n\nwant:\n%s\n\noutput mismatch:\n%s", got, want, diffStr)
}
