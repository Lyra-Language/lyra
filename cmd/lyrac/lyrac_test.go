package main

import (
	"os"
	"os/exec"
	"path/filepath"
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

// TestBuild_Clean is the default build: an executable, no IR left behind.
func TestBuild_Clean(t *testing.T) {
	requireCC(t)
	path := copyFixtureToTemp(t, "ok.lyra")
	stdout, stderr, code := captureRun(t, "build", path)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\nstderr: %s", code, stderr)
	}
	exe := replaceExt(path, "")
	if !strings.Contains(stdout, "wrote "+exe) {
		t.Errorf("stdout missing build summary naming %s:\n%s", exe, stdout)
	}
	// The artifact is an executable that runs, not just a file that exists.
	if err := exec.Command(exe).Run(); err != nil {
		t.Errorf("running %s: %v", exe, err)
	}
	// The .ll is scratch, so it must not be left in the user's source tree.
	if _, err := os.Stat(replaceExt(path, ".ll")); !os.IsNotExist(err) {
		t.Errorf("default build left an .ll beside the source (stat err = %v)", err)
	}
}

func TestBuild_OutputPath(t *testing.T) {
	requireCC(t)
	path := copyFixtureToTemp(t, "ok.lyra")
	exe := filepath.Join(filepath.Dir(path), "renamed")
	if _, stderr, code := captureRun(t, "build", "-o", exe, path); code != 0 {
		t.Fatalf("exit code = %d, want 0\nstderr: %s", code, stderr)
	}
	if err := exec.Command(exe).Run(); err != nil {
		t.Errorf("running %s: %v", exe, err)
	}
}

func TestBuild_KeepLL(t *testing.T) {
	requireCC(t)
	path := copyFixtureToTemp(t, "ok.lyra")
	if _, stderr, code := captureRun(t, "build", "--keep-ll", path); code != 0 {
		t.Fatalf("exit code = %d, want 0\nstderr: %s", code, stderr)
	}
	assertIsIR(t, replaceExt(path, ".ll"))
	if _, err := os.Stat(replaceExt(path, "")); err != nil {
		t.Errorf("--keep-ll should still link an executable: %v", err)
	}
}

// TestBuild_EmitLLVM stops at the IR, which is the one build that needs no C
// compiler at all — hence no requireCC.
func TestBuild_EmitLLVM(t *testing.T) {
	path := copyFixtureToTemp(t, "ok.lyra")
	stdout, stderr, code := captureRun(t, "build", "--emit-llvm", path)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\nstderr: %s", code, stderr)
	}
	if !strings.Contains(stdout, "compile with: clang") {
		t.Errorf("stdout missing manual compile hint:\n%s", stdout)
	}
	assertIsIR(t, replaceExt(path, ".ll"))
	if _, err := os.Stat(replaceExt(path, "")); !os.IsNotExist(err) {
		t.Errorf("--emit-llvm should not link an executable (stat err = %v)", err)
	}
}

// TestBuild_MissingCC covers the compiler-not-found path: it must fail loudly
// and still leave the IR behind, since that is all the user has to compile once
// they install one.
func TestBuild_MissingCC(t *testing.T) {
	path := copyFixtureToTemp(t, "ok.lyra")
	_, stderr, code := captureRun(t, "build", "--cc", "definitely-not-a-compiler", path)
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr, "definitely-not-a-compiler") {
		t.Errorf("stderr missing the compiler name:\n%s", stderr)
	}
	assertIsIR(t, replaceExt(path, ".ll"))
}

// --- run --------------------------------------------------------------------

// TestRunCmd_Clean runs a program end to end: its stdout is the command's, and
// nothing is left in the source directory.
func TestRunCmd_Clean(t *testing.T) {
	requireCC(t)
	path := copyFixtureToTemp(t, "hello.lyra")
	stdout, stderr, code := captureRun(t, "run", path)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\nstderr: %s", code, stderr)
	}
	if stdout != "hello\n" {
		t.Errorf("stdout = %q, want the program's output alone (%q)", stdout, "hello\n")
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "hello.lyra" {
			t.Errorf("run left %s behind in the source directory", e.Name())
		}
	}
}

// TestRunCmd_ExitCode is the reason run cannot report build failures with a
// status of its own: the program's `main` picks it.
func TestRunCmd_ExitCode(t *testing.T) {
	requireCC(t)
	path := copyFixtureToTemp(t, "exit7.lyra")
	if _, stderr, code := captureRun(t, "run", path); code != 7 {
		t.Errorf("exit code = %d, want 7 (the program's own)\nstderr: %s", code, stderr)
	}
}

func TestRunCmd_Diagnostics(t *testing.T) {
	// A program that doesn't compile fails before anything is executed, and the
	// diagnostic is the compiler's, on stderr.
	stdout, stderr, code := captureRun(t, "run", fixture("typeerr.lyra"))
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want empty (nothing ran)", stdout)
	}
	if !strings.Contains(stderr, "error") {
		t.Errorf("stderr missing the type error:\n%s", stderr)
	}
}

// TestRunCmd_RejectsBuildFlags: run keeps no artifact, so a flag choosing where
// one lands is refused rather than silently ignored.
func TestRunCmd_RejectsBuildFlags(t *testing.T) {
	for _, flag := range []string{"-o", "--emit-llvm", "--keep-ll"} {
		args := []string{"run", flag, "x.lyra"}
		if flag == "-o" {
			args = []string{"run", "-o", "out", "x.lyra"}
		}
		_, stderr, code := captureRun(t, args...)
		if code != 2 {
			t.Errorf("run %s: exit code = %d, want 2", flag, code)
		}
		if !strings.Contains(stderr, "build flag") {
			t.Errorf("run %s: stderr missing the build-flag notice:\n%s", flag, stderr)
		}
	}
}

func TestBuild_BadFlags(t *testing.T) {
	cases := [][]string{
		{"build", "--frobnicate", "x.lyra"},
		{"build", "-o"},               // -o with no value
		{"build", "a.lyra", "b.lyra"}, // two sources
		{"build", "--emit-llvm"},      // no source at all
	}
	for _, args := range cases {
		if _, stderr, code := captureRun(t, args...); code != 2 {
			t.Errorf("run(%q) exit code = %d, want 2\nstderr: %s", args, code, stderr)
		}
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

// The optimization level defaults to -O2 and is overridable per build.
//
// **-O2 rather than clang's own -O0 default**, because the tradeoff this compiler
// faces is not the usual one: it emits no debug info at any level, so shipping
// unoptimized buys no debuggability — only build time. Measured, -O0 costs around
// 3x on ordinary code (15925us against 5087us on a string scan) for roughly 50ms
// of link time on a 2000-line module.
func TestBuild_OptLevel(t *testing.T) {
	t.Run("defaults to -O2", func(t *testing.T) {
		path := copyFixtureToTemp(t, "ok.lyra")
		stdout, stderr, code := captureRun(t, "build", "--emit-llvm", path)
		if code != 0 {
			t.Fatalf("exit code = %d, want 0\nstderr: %s", code, stderr)
		}
		// The hint has to name the level lyrac would actually have passed, or it
		// reproduces a different build than the one it is standing in for.
		if !strings.Contains(stdout, "clang -O2 ") {
			t.Errorf("compile hint should carry the default -O2:\n%s", stdout)
		}
	})

	t.Run("an explicit level wins and reaches the hint", func(t *testing.T) {
		path := copyFixtureToTemp(t, "ok.lyra")
		stdout, stderr, code := captureRun(t, "build", "--emit-llvm", "-O0", path)
		if code != 0 {
			t.Fatalf("exit code = %d, want 0\nstderr: %s", code, stderr)
		}
		if !strings.Contains(stdout, "clang -O0 ") {
			t.Errorf("explicit -O0 should reach the compile hint:\n%s", stdout)
		}
	})

	t.Run("a non-numeric level is still passed through", func(t *testing.T) {
		// -Os and -Oz are real clang levels, so recognising only digits would
		// refuse a build that works. The level is matched loosely on purpose and
		// clang is left as the authority on which ones exist.
		path := copyFixtureToTemp(t, "ok.lyra")
		stdout, _, code := captureRun(t, "build", "--emit-llvm", "-Os", path)
		if code != 0 || !strings.Contains(stdout, "clang -Os ") {
			t.Errorf("-Os should pass through; code=%d stdout:\n%s", code, stdout)
		}
	})

	t.Run("run accepts a level, since it builds too", func(t *testing.T) {
		path := copyFixtureToTemp(t, "ok.lyra")
		_, stderr, code := captureRun(t, "run", "-O0", path)
		if code != 0 {
			t.Errorf("run should accept an optimization level; code=%d stderr: %s", code, stderr)
		}
	})
}

// ---------------------------------------------------------------------------
// `-o` reaches the IR
// ---------------------------------------------------------------------------
//
// One rule behind both modes: **the `.ll` is written beside the executable `-o` names**,
// and under `--emit-llvm` — which links no executable at all — `-o` names the `.ll`
// itself. Until 09/11 it reached neither, so a build wrote artifacts somewhere other than
// where the flag said, silently. `run` refuses these flags outright to avoid exactly that.

// `--emit-llvm -o` writes the IR where it was told and nowhere else.
func TestBuild_EmitLLVM_OutNamesTheIR(t *testing.T) {
	path := copyFixtureToTemp(t, "ok.lyra")
	out := filepath.Join(t.TempDir(), "chosen.ll")
	stdout, stderr, code := captureRun(t, "build", "--emit-llvm", "-o", out, path)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\nstderr: %s", code, stderr)
	}
	assertIsIR(t, out)
	if _, err := os.Stat(replaceExt(path, ".ll")); !os.IsNotExist(err) {
		t.Errorf("nothing should be written beside the source (stat err = %v)", err)
	}
	if !strings.Contains(stdout, out) {
		t.Errorf("stdout should report where the IR went:\n%s", stdout)
	}
}

// **The hint must not name the `.ll` as the executable to build.** With `-o` naming the
// IR, reporting it as the output would suggest compiling the file into itself — which is
// what the old code printed, since it reused the executable path unconditionally.
func TestBuild_EmitLLVM_HintDoesNotCompileTheIRIntoItself(t *testing.T) {
	path := copyFixtureToTemp(t, "ok.lyra")
	out := filepath.Join(t.TempDir(), "chosen.ll")
	stdout, stderr, code := captureRun(t, "build", "--emit-llvm", "-o", out, path)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\nstderr: %s", code, stderr)
	}
	if strings.Contains(stdout, "-o "+out) {
		t.Errorf("the hint offers the .ll as its own output:\n%s", stdout)
	}
	if !strings.Contains(stdout, "-o "+replaceExt(path, "")) {
		t.Errorf("the hint should offer the name an ordinary build would produce:\n%s", stdout)
	}
}

// `--keep-ll -o` puts **both** artifacts where they were asked for, which is what that
// flag's own help has always promised ("beside the executable").
func TestBuild_KeepLL_OutTakesTheIRAlong(t *testing.T) {
	requireCC(t)
	path := copyFixtureToTemp(t, "ok.lyra")
	exe := filepath.Join(t.TempDir(), "prog")
	stdout, stderr, code := captureRun(t, "build", "--keep-ll", "-o", exe, path)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\nstderr: %s", code, stderr)
	}
	assertIsIR(t, exe+".ll")
	if _, err := os.Stat(exe); err != nil {
		t.Errorf("--keep-ll should still link an executable: %v", err)
	}
	if _, err := os.Stat(replaceExt(path, ".ll")); !os.IsNotExist(err) {
		t.Errorf("the IR should not also land beside the source (stat err = %v)", err)
	}
	if !strings.Contains(stdout, "kept "+exe+".ll") {
		t.Errorf("stdout should report where the IR was kept:\n%s", stdout)
	}
}

// A plain build with `-o` still leaves no IR anywhere: the executable is the artifact.
func TestBuild_OutLeavesNoIR(t *testing.T) {
	requireCC(t)
	path := copyFixtureToTemp(t, "ok.lyra")
	exe := filepath.Join(t.TempDir(), "prog")
	if _, stderr, code := captureRun(t, "build", "-o", exe, path); code != 0 {
		t.Fatalf("exit code = %d, want 0\nstderr: %s", code, stderr)
	}
	for _, stray := range []string{exe + ".ll", replaceExt(path, ".ll")} {
		if _, err := os.Stat(stray); !os.IsNotExist(err) {
			t.Errorf("a plain build should leave no IR at %s (stat err = %v)", stray, err)
		}
	}
}

// Without `-o`, both modes are exactly as they were — the default is what most builds use
// and the fix must not move it.
func TestBuild_NoOutIsUnchanged(t *testing.T) {
	path := copyFixtureToTemp(t, "ok.lyra")
	if _, stderr, code := captureRun(t, "build", "--emit-llvm", path); code != 0 {
		t.Fatalf("exit code = %d, want 0\nstderr: %s", code, stderr)
	}
	assertIsIR(t, replaceExt(path, ".ll"))
}

// ---------------------------------------------------------------------------------------
// Layout across modules
// ---------------------------------------------------------------------------------------

// **A struct's layout does not depend on whether its fields' types are exported.**
//
// The backend's `resolveForLayout` looked each field's named type up by bare name from the
// module being lowered, so a public struct with a field of a type *private to its own
// module* could not be sized from any other module — "cannot size dynamic array element
// type" the moment one was held in a `[]T` elsewhere. The typechecker's `hasCLayout` had
// the same bug and was fixed the same day (09/11); this is the backend's copy of it, found
// by `examples/raylib/models.lyra` holding a `Model` — which has a private
// `ModelSkeleton` — in a `[]Placed`.
//
// A CLI build rather than a backend test, because the shape needs two modules and the
// backend harness takes one source string. `--emit-llvm` is the whole assertion: the bug
// was a refusal to lower, and emitting IR needs no C compiler.
func TestBuild_APrivateNestedStructSizesFromAnotherModule(t *testing.T) {
	dir := t.TempDir()
	lib := `module lib
struct Inner { a: i32, b: i32 }
pub struct Outer { inner: Inner, c: f32 }
pub let make = pure (c: f32) -> Outer => Outer { inner: Inner { a: 1, b: 2 }, c: c }
`
	main := `module main
import lib.{ Outer, make }
struct Held { name: string, o: Outer }
let main = () -> void => {
  var all: []Held = []
  all.push(Held { name: "x", o: make(1.5) })
  // And the public struct held directly, which reaches layout as an already-resolved
  // struct type rather than as a name — the other of the two paths the fix covers.
  let outs: []Outer = [make(2.5)]
  println("${all.len()} ${outs.len()}")
}
`
	if err := os.WriteFile(filepath.Join(dir, "lib.lyra"), []byte(lib), 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "main.lyra")
	if err := os.WriteFile(path, []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "main.ll")
	_, stderr, code := captureRun(t, "build", "--emit-llvm", "-o", out, path)
	if code != 0 {
		t.Fatalf("a struct holding a private nested struct from another module must lower; exit %d\nstderr: %s", code, stderr)
	}
	assertIsIR(t, out)
}
