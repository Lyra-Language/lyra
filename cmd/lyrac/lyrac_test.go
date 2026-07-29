package main

import (
	"os"
	"strings"
	"testing"

	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
)

// --- dispatch & usage -------------------------------------------------------

func TestRun_NoArgs(t *testing.T) {
	stdout, stderr, code := captureRun(t)
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr, "usage: lyrac") {
		t.Errorf("stderr missing usage text:\n%s", stderr)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty", stdout)
	}
}

func TestRun_TooFewArgs(t *testing.T) {
	// A command with no file path still fails the len(args) < 2 guard.
	_, stderr, code := captureRun(t, "check")
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr, "usage: lyrac") {
		t.Errorf("stderr missing usage text:\n%s", stderr)
	}
}

func TestRun_UnknownCommand(t *testing.T) {
	_, stderr, code := captureRun(t, "frobnicate", "x.lyra")
	if code != 2 {
		t.Errorf("exit code = %d, want 2", code)
	}
	if !strings.Contains(stderr, `unknown command "frobnicate"`) {
		t.Errorf("stderr missing unknown-command notice:\n%s", stderr)
	}
	if !strings.Contains(stderr, "usage: lyrac") {
		t.Errorf("stderr missing usage text:\n%s", stderr)
	}
}

// --- check ------------------------------------------------------------------

func TestCheck_Clean(t *testing.T) {
	stdout, stderr, code := captureRun(t, "check", fixture("ok.lyra"))
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	if stdout != "" || stderr != "" {
		t.Errorf("clean check produced output: stdout=%q stderr=%q", stdout, stderr)
	}
}

func TestCheck_TypeError(t *testing.T) {
	_, stderr, code := captureRun(t, "check", fixture("typeerr.lyra"))
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	checkGolden(t, stderr, "check_typeerr.golden")
}

func TestCheck_SyntaxError(t *testing.T) {
	_, stderr, code := captureRun(t, "check", fixture("syntax.lyra"))
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	checkGolden(t, stderr, "check_syntax.golden")
}

func TestCheck_WarningIsNotAnError(t *testing.T) {
	// A warning-only program prints the warning but still exits 0.
	_, stderr, code := captureRun(t, "check", fixture("warn.lyra"))
	if code != 0 {
		t.Errorf("exit code = %d, want 0 (warnings are not errors)", code)
	}
	checkGolden(t, stderr, "check_warn.golden")
}

func TestCheck_MissingFile(t *testing.T) {
	_, stderr, code := captureRun(t, "check", fixture("does_not_exist.lyra"))
	if code != 2 {
		t.Errorf("exit code = %d, want 2 (IO failure)", code)
	}
	if !strings.Contains(stderr, "lyrac:") || !strings.Contains(stderr, "no such file") {
		t.Errorf("stderr missing read-error notice:\n%s", stderr)
	}
}

// --- build ------------------------------------------------------------------

func TestBuild_Clean(t *testing.T) {
	path := copyFixtureToTemp(t, "ok.lyra")
	stdout, stderr, code := captureRun(t, "build", path)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\nstderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "wrote") || !strings.Contains(stdout, "compile with: clang") {
		t.Errorf("stdout missing build summary:\n%s", stdout)
	}

	// The .ll artifact is written next to the source and is real LLVM IR.
	llPath := replaceExt(path, ".ll")
	ir, err := os.ReadFile(llPath)
	if err != nil {
		t.Fatalf("expected emitted IR at %s: %v", llPath, err)
	}
	if !strings.Contains(string(ir), "define i32 @main()") {
		t.Errorf("emitted IR missing @main definition:\n%s", ir)
	}
}

func TestBuild_MissingMain(t *testing.T) {
	// A library (no `main`) is a valid check but not a valid build. This also
	// exercises printDiagnostics' no-source-location branch: the "no entry
	// point" diagnostic has no line:col, so it prints as "<path>: error: …".
	_, stderr, code := captureRun(t, "build", fixture("nomain.lyra"))
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	checkGolden(t, stderr, "build_nomain.golden")
}

func TestBuild_BackendError(t *testing.T) {
	// unsupported.lyra type-checks and has a valid entry point but uses struct
	// record-update syntax, which the LLVM backend cannot lower yet, so
	// lowerAndEmit reports a backend error and no .ll is written. The assertion is
	// deliberately loose (exit code + "llvm backend:" prefix) so it survives
	// changes to the exact message — or a future backend that learns to lower
	// this, at which point it fails loudly and should be repointed at a
	// still-unsupported form. (Interpolation, then array literals, then `newtype`,
	// then passing a function as a value all used to be that form; all four lower
	// now.)
	_, stderr, code := captureRun(t, "build", fixture("unsupported.lyra"))
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr, "llvm backend:") {
		t.Errorf("stderr missing backend-error notice:\n%s", stderr)
	}
}

// --- small pure helpers -----------------------------------------------------

func TestReplaceExt(t *testing.T) {
	cases := []struct {
		path, ext, want string
	}{
		{"prog.lyra", ".ll", "prog.ll"},
		{"prog.lyra", "", "prog"},
		{"dir/prog.lyra", ".ll", "dir/prog.ll"},
		{"noext.txt", ".ll", "noext.txt.ll"}, // only a trailing .lyra is replaced
		{"prog.lyra", ".s", "prog.s"},
	}
	for _, c := range cases {
		if got := replaceExt(c.path, c.ext); got != c.want {
			t.Errorf("replaceExt(%q, %q) = %q, want %q", c.path, c.ext, got, c.want)
		}
	}
}

func TestSeverityLabel(t *testing.T) {
	cases := []struct {
		sev  diag.Severity
		want string
	}{
		{diag.SeverityError, "error"},
		{diag.SeverityWarning, "warning"},
		{diag.SeverityInfo, "info"},
		{diag.Severity(99), "error"}, // unknown severities fall back to "error"
	}
	for _, c := range cases {
		if got := severityLabel(c.sev); got != c.want {
			t.Errorf("severityLabel(%v) = %q, want %q", c.sev, got, c.want)
		}
	}
}
