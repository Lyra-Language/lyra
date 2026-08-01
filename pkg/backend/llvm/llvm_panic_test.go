package llvm

import (
	"errors"
	"os/exec"
	"strings"
	"testing"
)

// buildAndRunPanic compiles src, runs it, and returns stderr plus the exit code —
// a panic's observable behavior is the message it writes to fd 2 and the status it
// exits with, neither of which buildAndRun (exit code) or buildAndRunCapture
// (stdout) reports.
func buildAndRunPanic(t *testing.T, src string) (string, int) {
	t.Helper()
	clang := lookClang(t)
	ir, err := emitSource(t, src)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	cmd := exec.Command(compileCached(t, clang, ir))
	var stderr strings.Builder
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	code := 0
	if runErr != nil {
		var ee *exec.ExitError
		if !errors.As(runErr, &ee) {
			t.Fatalf("running the binary failed: %v", runErr)
		}
		code = ee.ExitCode()
	}
	return stderr.String(), code
}

// A `panic(msg)` writes "lyra: panic: <msg>" to stderr and exits 101 — the same
// status and stream as the traps the compiler inserts for overflow, divide by zero
// and a failed bounds check, because a panic the programmer wrote and one the
// compiler inserted are the same event to whatever is watching the process.
func TestExec_PanicMessageAndExitCode(t *testing.T) {
	t.Parallel()
	stderr, code := buildAndRunPanic(t, `
data Opt<t> = Nil | Just(t)
let expect = (m: Opt<i64>, msg: string) -> i64 => match m {
  Just(v) => v,
  Nil => panic(msg),
}
let main = () -> u8 => {
  let m: Opt<i64> = Nil
  u8(expect(m, "the config port was missing"))
}`)
	if code != trapExitCode {
		t.Errorf("exit code: got %d, want %d", code, trapExitCode)
	}
	if want := "lyra: panic: the config port was missing\n"; stderr != want {
		t.Errorf("stderr: got %q, want %q", stderr, want)
	}
}

// The non-panicking path is unaffected: the arm that produces a value still does.
func TestExec_PanicNotTaken(t *testing.T) {
	t.Parallel()
	stderr, code := buildAndRunPanic(t, `
data Opt<t> = Nil | Just(t)
let expect = (m: Opt<i64>, msg: string) -> i64 => match m {
  Just(v) => v,
  Nil => panic(msg),
}
let main = () -> u8 => {
  let m: Opt<i64> = Just(7)
  u8(expect(m, "not reached"))
}`)
	if code != 7 {
		t.Errorf("exit code: got %d, want 7", code)
	}
	if stderr != "" {
		t.Errorf("nothing should be written to stderr, got %q", stderr)
	}
}

// The message is a runtime `string`, not a literal baked into the trap function like
// the compiler's four — which is the point, since an interpolated message is what
// makes a panic worth writing.
func TestExec_PanicInterpolatedMessage(t *testing.T) {
	t.Parallel()
	stderr, code := buildAndRunPanic(t, `
let at = pure (i: i64) -> i64 => if i < 0 { panic("negative index ${i}") } else { i }
let main = () -> u8 => u8(at(-3))`)
	if code != trapExitCode {
		t.Errorf("exit code: got %d, want %d", code, trapExitCode)
	}
	if want := "lyra: panic: negative index -3\n"; stderr != want {
		t.Errorf("stderr: got %q, want %q", stderr, want)
	}
}

// `never` in the positions where a diverging operand meets code that wants a value.
// Each of these dereferenced a nil before the `diverged` guards went in — a Go crash
// out of lyrac, not the loud error the backend is supposed to produce. They all
// reach the panic, so the assertion is that the program traps rather than dying in
// the compiler.
func TestExec_PanicInValueConsumingPositions(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
	}{
		{"binding initializer", `let main = () -> u8 => { let x = panic("bound"); 0 }`},
		{"var reassignment", `let main = () -> u8 => { var x = 1; x = panic("reassigned"); 0 }`},
		{
			"call argument",
			`let f = (n: i64) -> i64 => n
let main = () -> u8 => u8(f(panic("argument")))`,
		},
		{
			"nested call under a conversion",
			`let f = (n: i64) -> i64 => n
let main = () -> u8 => u8(f(f(panic("deep"))))`,
		},
		{"array element", `let main = () -> u8 => { let xs = [1, panic("element")]; print(xs[0]); 0 }`},
		{"statement position", `let main = () -> u8 => { panic("statement") }`},
		{"whole function body", `let boom = (why: string) -> i64 => panic(why)
let main = () -> u8 => u8(boom("every path"))`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			stderr, code := buildAndRunPanic(t, c.src)
			if code != trapExitCode {
				t.Errorf("exit code: got %d, want %d (stderr %q)", code, trapExitCode, stderr)
			}
			if !strings.HasPrefix(stderr, "lyra: panic: ") {
				t.Errorf("stderr should carry the panic message, got %q", stderr)
			}
		})
	}
}

// One `lyra_panic` per module however many sites call it, like the other traps —
// each site is a `call`+`unreachable`, not a copy of the write-and-exit sequence.
func TestEmit_PanicRuntimeIsEmittedOnce(t *testing.T) {
	t.Parallel()
	got, err := emitSource(t, `
let a = (n: i64) -> i64 => if n == 0 { panic("zero") } else { n }
let b = (n: i64) -> i64 => if n == 1 { panic("one") } else { n }
let main = () -> u8 => u8(a(2) + b(3))`)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(got, "define void @lyra_panic("); n != 1 {
		t.Errorf("want exactly 1 definition of @lyra_panic, got %d:\n%s", n, got)
	}
	if n := strings.Count(got, "call void @lyra_panic("); n != 2 {
		t.Errorf("want 2 call sites, got %d", n)
	}
}
