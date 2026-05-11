package collector_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

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

// checkGolden compares got against the golden file at goldenPath. If the file
// does not exist or is empty, it is created with got as its content and the
// test is failed so it can be re-run for verification. Otherwise got is
// compared against the file contents using cmpOutput.
func checkGolden(t *testing.T, got, goldenPath string) {
	t.Helper()
	expected, err := os.ReadFile(goldenPath)
	if err != nil {
		if os.IsNotExist(err) {
			if err := os.MkdirAll(filepath.Dir(goldenPath), 0755); err != nil {
				t.Fatalf("MkdirAll: %v", err)
			}
			if err := os.WriteFile(goldenPath, []byte(got), 0644); err != nil {
				t.Fatalf("Write golden file: %v", err)
			}
			t.Fatalf("Wrote initial golden file at %s; re-run test to verify", goldenPath)
		}
		t.Fatalf("Read golden file: %v", err)
	}
	if len(expected) == 0 {
		if err := os.WriteFile(goldenPath, []byte(got), 0644); err != nil {
			t.Fatalf("Write golden file: %v", err)
		}
		t.Fatal("Golden file was empty; wrote current output. Re-run test to verify.")
	}
	if msg := cmpOutput(got, string(expected)); msg != "" {
		if os.Getenv("UPDATE_GOLDEN") == "1" {
			if err := os.WriteFile(goldenPath, []byte(got), 0644); err != nil {
				t.Fatalf("Update golden file: %v", err)
			}
			t.Logf("updated golden file: %s", goldenPath)
			return
		}
		t.Errorf("Print output mismatch (golden file %s): %s", goldenPath, msg)
	}
}

// cmpOutput compares got and expected and returns an empty string if equal, otherwise a diff message.
// Comparison ignores whitespace: lines are trimmed and runs of spaces/tabs are collapsed,
// so you can write the expected string without matching indentation exactly.
// The diff is produced by github.com/sergi/go-diff (DiffPrettyText).
// Use in tests: if msg := cmpOutput(got, expected); msg != "" { t.Error(msg) }
func cmpOutput(got, expected string) string {
	if normalizeWhitespace(got) == normalizeWhitespace(expected) {
		return ""
	}
	dmp := diffmatchpatch.New()
	diffs := dmp.DiffMain(expected, got, false)
	diffStr := dmp.DiffPrettyText(diffs)
	if lines := strings.Count(diffStr, "\n"); lines > diffMaxOutputLines {
		split := strings.SplitN(diffStr, "\n", diffMaxOutputLines+1)
		diffStr = strings.Join(split[:diffMaxOutputLines], "\n") + "\n... (truncated)\n"
	}
	return fmt.Sprintf("\n\ngot:\n%s\n\nexpected:\n%s\n\noutput mismatch:\n%s", got, expected, diffStr)
}
