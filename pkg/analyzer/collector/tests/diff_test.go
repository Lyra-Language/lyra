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

// cmpOutput compares got and expected **byte for byte**, answering an empty string when
// they are equal and a diff message otherwise. The diff is produced by
// github.com/sergi/go-diff (DiffPrettyText).
//
// It used to trim each line and collapse runs of spaces and tabs, so that an inline
// `want` string need not match the printer's indentation. Nothing writes one any more —
// every caller is a golden file, which the printer wrote — and the leniency was hiding
// drift rather than absorbing formatting: five goldens had been hand-edited into shapes
// the printer does not produce, three with mangled indentation and two having lost the
// leading space of a string's value, and the comparison said nothing. The Lyra collector
// reading these same files byte for byte is what found them (09/24).
func cmpOutput(got, expected string) string {
	if got == expected {
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
