package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeModule puts a source file in a temp directory and returns its path. The prelude
// is switched off so a test's expected output is only what the file itself declares.
func writeModule(t *testing.T, name, src string) string {
	t.Helper()
	t.Setenv("LYRA_NO_PRELUDE", "1")
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
	return path
}

func TestDoc_WritesAPagePerModule(t *testing.T) {
	src := "module demo.math\n" +
		"//! Arithmetic helpers.\n\n" +
		"/// Adds two numbers.\n" +
		"///\n" +
		"/// # Panics\n" +
		"///\n" +
		"/// Traps if the sum overflows.\n" +
		"pub let add = pure (a: i64, b: i64) -> i64 => a + b\n"
	path := writeModule(t, "math.lyra", src)
	out := filepath.Join(t.TempDir(), "docs")

	stdout, stderr, code := captureRun(t, "doc", path, "-o", out)
	if code != 0 {
		t.Fatalf("exit %d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}

	page, err := os.ReadFile(filepath.Join(out, "demo-math.md"))
	if err != nil {
		t.Fatalf("expected a page for demo.math: %v", err)
	}
	got := string(page)
	for _, want := range []string{
		`title: "demo.math"`,
		`description: "Arithmetic helpers."`,
		"pub let add = pure (a: i64, b: i64) -> i64",
		"Adds two numbers.",
		"#### Panics",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("page is missing %q:\n%s", want, got)
		}
	}
	if !strings.Contains(stdout, "documented 1/1") {
		t.Errorf("expected a coverage summary, got: %s", stdout)
	}
}

// The summary prints on every run, not only under --strict: a run that documented half
// its surface and said nothing is the silent incompleteness this command exists to
// avoid.
func TestDoc_ReportsCoverageWithoutStrict(t *testing.T) {
	path := writeModule(t, "m.lyra", "module m\n/// Documented.\npub let a = pure () -> i64 => 1\npub let b = pure () -> i64 => 2\n")
	stdout, _, code := captureRun(t, "doc", path, "-o", filepath.Join(t.TempDir(), "docs"))
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(stdout, "documented 1/2") {
		t.Errorf("expected 1/2 coverage, got: %s", stdout)
	}
}

func TestDoc_StrictFailsOnAGap(t *testing.T) {
	path := writeModule(t, "m.lyra", "module m\npub let undocumented = pure () -> i64 => 1\n")
	_, stderr, code := captureRun(t, "doc", path, "-o", filepath.Join(t.TempDir(), "docs"), "--strict")
	if code == 0 {
		t.Error("--strict should fail when a public declaration is undocumented")
	}
	if !strings.Contains(stderr, "undocumented: m.undocumented") {
		t.Errorf("expected the gap to be named, got: %s", stderr)
	}
}

// An undocumented declaration is still on the page. Dropping it would make the page
// misrepresent what the module exports.
func TestDoc_UndocumentedIsStillListed(t *testing.T) {
	path := writeModule(t, "m.lyra", "module m\npub let bare = pure () -> i64 => 1\n")
	out := filepath.Join(t.TempDir(), "docs")
	if _, _, code := captureRun(t, "doc", path, "-o", out); code != 0 {
		t.Fatalf("exit %d", code)
	}
	page, err := os.ReadFile(filepath.Join(out, "m.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(page), "pub let bare") {
		t.Errorf("an undocumented declaration was dropped from the page:\n%s", page)
	}
}

func TestDoc_PrivateNeedsTheFlag(t *testing.T) {
	src := "module m\npub let shown = pure () -> i64 => 1\nlet hidden = pure () -> i64 => 2\n"
	path := writeModule(t, "m.lyra", src)

	out := filepath.Join(t.TempDir(), "public")
	if _, _, code := captureRun(t, "doc", path, "-o", out); code != 0 {
		t.Fatalf("exit %d", code)
	}
	page, _ := os.ReadFile(filepath.Join(out, "m.md"))
	if strings.Contains(string(page), "hidden") {
		t.Errorf("a private declaration appeared without --private:\n%s", page)
	}

	out = filepath.Join(t.TempDir(), "all")
	if _, _, code := captureRun(t, "doc", path, "-o", out, "--private"); code != 0 {
		t.Fatalf("exit %d", code)
	}
	page, _ = os.ReadFile(filepath.Join(out, "m.md"))
	if !strings.Contains(string(page), "hidden") {
		t.Errorf("--private did not include the private declaration:\n%s", page)
	}
}

// Documenting a program that does not type-check would print `?` wherever a type failed
// to resolve and publish the result as though it were the API.
func TestDoc_RefusesAProgramThatDoesNotTypeCheck(t *testing.T) {
	path := writeModule(t, "m.lyra", "module m\npub let broken = pure () -> i64 => \"not an int\"\n")
	_, stderr, code := captureRun(t, "doc", path, "-o", filepath.Join(t.TempDir(), "docs"))
	if code == 0 {
		t.Error("doc should refuse a program with type errors")
	}
	if !strings.Contains(stderr, "does not type-check") {
		t.Errorf("expected an explanation, got: %s", stderr)
	}
}

func TestDoc_UsageOnMissingPath(t *testing.T) {
	_, _, code := captureRun(t, "doc")
	if code != 2 {
		t.Errorf("exit %d, want 2 for a missing path", code)
	}
}
