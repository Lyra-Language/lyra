package collector

import (
	"fmt"
	"strings"
)

const diffMaxOutputLines = 100

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

// normLine normalizes a single line for comparison (trim + collapse whitespace).
func normLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// diffStrings returns a line diff of got vs want showing only lines that differ
// after normalizing whitespace (so indentation-only changes are not shown).
// Leading newlines are trimmed from both so a newline on the first line of want is ignored.
// Each line is prefixed with its line number and "- " (want) or "+ " (got).
func diffStrings(got, want string) string {
	gotLines := strings.Split(strings.TrimLeft(got, "\n"), "\n")
	wantLines := strings.Split(strings.TrimLeft(want, "\n"), "\n")

	ng := len(gotLines)
	nw := len(wantLines)

	var b strings.Builder
	b.WriteString("--- want\n+++ got\n")

	shown := 0
	for i := 0; (i < ng || i < nw) && shown < diffMaxOutputLines; i++ {
		var gl, wl string
		if i < ng {
			gl = gotLines[i]
		}
		if i < nw {
			wl = wantLines[i]
		}

		// Only show lines that differ in content, not just whitespace
		if normLine(gl) != normLine(wl) {
			if i < nw {
				fmt.Fprintf(&b, "%4d | - %s\n", i+1, wl)
				shown++
			}
			if i < ng {
				fmt.Fprintf(&b, "%4d | + %s\n", i+1, gl)
				shown++
			}
		}
	}
	if shown >= diffMaxOutputLines {
		b.WriteString("... (truncated)\n")
	}
	return b.String()
}

// cmpOutput compares got and want and returns an empty string if equal, otherwise a diff message.
// Comparison ignores whitespace: lines are trimmed and runs of spaces/tabs are collapsed,
// so you can write the expected string without matching indentation exactly.
// Use in tests: if msg := cmpOutput(got, want); msg != "" { t.Error(msg) }
func cmpOutput(got, want string) string {
	if normalizeWhitespace(got) == normalizeWhitespace(want) {
		return ""
	}
	returnString := fmt.Sprintf("\n\ngot:\n%s\nwant:\n%s", got, want)
	returnString += fmt.Sprintf("\n\noutput mismatch:\n%s", diffStrings(got, want))
	return returnString
}
