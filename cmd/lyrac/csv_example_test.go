package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// `examples/csv/csv.lyra` is the target program for `Result`: every way a CSV file can be
// malformed is a value carrying the line and column it happened at, and `?` carries one
// out of the scan that found it.
//
// The error cases are the half worth pinning. A parser that reports the *wrong* position
// still reports an error, so a test that only checked the exit code would pass on one
// that had lost count of the newlines — every expectation below names the place.
func TestExample_CsvReader(t *testing.T) {
	root := repoRoot(t)
	t.Setenv("LYRA_STD", root)
	dir := t.TempDir()
	bin := filepath.Join(dir, "csv")
	if _, stderr, code := captureRun(t, "build", "-o", bin, filepath.Join(root, "examples", "csv", "csv.lyra")); code != 0 {
		t.Fatalf("building the example exited %d\nstderr: %s", code, stderr)
	}
	run := func(t *testing.T, contents string, args ...string) (string, int) {
		t.Helper()
		path := filepath.Join(t.TempDir(), "in.csv")
		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
		out, err := exec.Command(bin, append([]string{"-f", path}, args...)...).CombinedOutput()
		code := 0
		if err != nil {
			ee, ok := err.(*exec.ExitError)
			if !ok {
				t.Fatalf("running the example failed: %v\n%s", err, out)
			}
			code = ee.ExitCode()
		}
		return string(out), code
	}

	t.Run("the dialect", func(t *testing.T) {
		for _, c := range []struct{ name, in, want string }{
			{
				"a quoted field may hold the separator",
				"name,city\nada,\"New York, NY\"\n",
				"2 columns, 1 rows\nname | city\nada | New York, NY\n",
			},
			{
				"a doubled quote is one literal quote",
				"a,b\n\"say \"\"hi\"\"\",2\n",
				"2 columns, 1 rows\na | b\nsay \"hi\" | 2\n",
			},
			{"carriage returns", "a,b\r\n1,2\r\n", "2 columns, 1 rows\na | b\n1 | 2\n"},
			{"no trailing newline", "a,b\n1,2", "2 columns, 1 rows\na | b\n1 | 2\n"},
			{"a blank line is not a record", "a,b\n1,2\n\n", "2 columns, 1 rows\na | b\n1 | 2\n"},
			{"an empty file is an empty sheet", "", "0 columns, 0 rows\n"},
		} {
			t.Run(c.name, func(t *testing.T) {
				if out, code := run(t, c.in); out != c.want || code != 0 {
					t.Errorf("%q gave (exit %d):\n%s\nwant:\n%s", c.in, code, out, c.want)
				}
			})
		}
	})

	t.Run("every error names its place", func(t *testing.T) {
		for _, c := range []struct{ name, in, want string }{
			{"a quoted field never closes", "a,b\n1,\"oops\n", "csv: line 2, column 3: a quoted field is never closed\n"},
			{"a quote inside a bare field", "a,b\nab\"cd,2\n", "csv: line 2, column 3: a quote inside an unquoted field\n"},
			{"text after a closing quote", "a,b\n\"ab\"cd,2\n", "csv: line 2, column 5: text after a closing quote\n"},
			{"a row narrower than the header", "a,b,c\n1,2\n", "csv: line 2, column 1: expected 3 fields, found 2\n"},
		} {
			t.Run(c.name, func(t *testing.T) {
				if out, code := run(t, c.in); out != c.want || code != 1 {
					t.Errorf("%q gave (exit %d):\n%s\nwant:\n%s", c.in, code, out, c.want)
				}
			})
		}
	})

	// The sample file shipped beside the example, read as a user would read it. It is in
	// the test because a sample nobody runs is a sample that rots: it exists to exercise
	// the parts of the dialect a split on commas gets wrong, and an edit that flattened
	// it into plain fields would still look fine on the page.
	t.Run("the sample file beside the example", func(t *testing.T) {
		sample := filepath.Join(root, "examples", "csv", "people.csv")
		out, err := exec.Command(bin, "-f", sample).CombinedOutput()
		if err != nil {
			t.Fatalf("reading %s failed: %v\n%s", sample, err, out)
		}
		got := string(out)
		for _, want := range []string{
			"4 columns, 4 rows",
			"New York, NY",                                 // a quoted field holding the separator
			`Coined "debugging" after a moth`,              // a doubled quote, read as one
			"Alan Turing | alan@example.com | Wilmslow | ", // an empty field at the end
		} {
			if !strings.Contains(got, want) {
				t.Errorf("the sample is missing %q:\n%s", want, got)
			}
		}
	})

	t.Run("one column by name", func(t *testing.T) {
		sheet := "name,email\nada,ada@example.com\ngrace,grace@example.com\n"
		if out, code := run(t, sheet, "-c", "email"); out != "ada@example.com\ngrace@example.com\n" || code != 0 {
			t.Errorf("-c email gave (exit %d):\n%s", code, out)
		}
		out, code := run(t, sheet, "-c", "phone")
		if code != 1 || !strings.Contains(out, "no column named phone") {
			t.Errorf("-c phone gave (exit %d):\n%s", code, out)
		}
	})
}
