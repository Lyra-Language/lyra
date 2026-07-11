package llvm

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Lyra-Language/lyra/pkg/driver"
)

// emitSource analyzes src, resolves its entry point, and returns the emitted IR.
func emitSource(t *testing.T, src string) (string, error) {
	t.Helper()
	res := driver.Analyze([]byte(src))
	if res.HasErrors() {
		t.Fatalf("unexpected analysis errors: %v", res.Diagnostics)
	}
	ep, diags := driver.ResolveEntryPoint(res)
	if ep == nil {
		t.Fatalf("no entry point: %v", diags)
	}
	ir, err := New().Emit(res, ep)
	return string(ir), err
}

// TestEmit_IntegerLiteralBody: an i64 entry whose body is an integer literal
// returns that literal — the source value reaches the exit code.
func TestEmit_IntegerLiteralBody(t *testing.T) {
	got, err := emitSource(t, "let main = () -> i64 => 42\n")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"define i64 @main()", "ret i64 42"} {
		if !strings.Contains(got, want) {
			t.Errorf("emitted IR missing %q:\n%s", want, got)
		}
	}
}

// TestEmit_VoidEntry: a void entry exits 0.
func TestEmit_VoidEntry(t *testing.T) {
	got, err := emitSource(t, "let main = () -> void => {}\n")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "ret i64 0") {
		t.Errorf("void entry should `ret i64 0`:\n%s", got)
	}
}

// buildAndRun emits IR for src, compiles it with clang, runs the binary, and
// returns its exit code. This is the honest test of codegen: it observes what
// the program *does*, not what the IR text looks like — so it survives IR
// spelling changes (add nsw, optimizations) and, unlike a string match, actually
// catches invalid IR (clang rejects it). Skips when clang isn't on PATH so the
// suite still passes in environments without a toolchain.
//
// Note: a process exit code is only 8 bits on Unix (0–255), so keep expected
// values small.
func buildAndRun(t *testing.T, src string) int {
	t.Helper()
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Skip("clang not found on PATH; skipping behavioral test")
	}

	ir, err := emitSource(t, src)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}

	dir := t.TempDir() // auto-removed when the test finishes
	llPath := filepath.Join(dir, "prog.ll")
	binPath := filepath.Join(dir, "prog")
	if err := os.WriteFile(llPath, []byte(ir), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(clang, llPath, "-o", binPath).CombinedOutput(); err != nil {
		t.Fatalf("clang rejected the IR: %v\n%s\n--- IR ---\n%s", err, out, ir)
	}

	runErr := exec.Command(binPath).Run()
	if runErr == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(runErr, &ee) {
		return ee.ExitCode() // non-zero exit is reported as an *exec.ExitError
	}
	t.Fatalf("running the binary failed: %v", runErr)
	return -1
}

// TestExec_Arithmetic checks that arithmetic bodies compute the right value by
// running the compiled program, not by inspecting the IR (LLVM emits an `add`,
// not a folded `3` — folding is an optimizer pass).
func TestExec_Arithmetic(t *testing.T) {
	cases := []struct {
		src  string
		want int
	}{
		{"let main = () -> i64 => 42\n", 42},
		{"let main = () -> i64 => 1 + 2\n", 3},
		{"let main = () -> i64 => 20 - 6\n", 14},
		{"let main = () -> i64 => 2 * 3 + 4\n", 10},
		{"let main = () -> i64 => 6 / 2\n", 3},
		{"let main = () -> i64 => 10 % 3\n", 1},
	}
	for _, c := range cases {
		if got := buildAndRun(t, c.src); got != c.want {
			t.Errorf("%q exited %d; want %d", c.src, got, c.want)
		}
	}
}

// TestExec_SignedArithmetic checks signed division/negation/mod against
// C-style truncating semantics (sign follows the dividend) — decided over a
// floored/Python-style alternative, since LLVM's sdiv/srem give truncating
// behavior natively. Expected values are verified against a real C program,
// not computed by hand: -1/2 truncates toward zero (0, not floor's -1), and
// 11 % -3's sign follows the positive dividend (2, not floor's -1).
func TestExec_SignedArithmetic(t *testing.T) {
	cases := []struct {
		src  string
		want int
	}{
		{"let main = () -> i64 => -1 / 2\n", 0},
		{"let main = () -> i64 => 11 % -3\n", 2},
		// Mod (%) and Remainder (%%) are distinct operators that deliberately
		// lower identically (see the MathBinaryOpMod/Remainder case comment in
		// llvm.go) — this pins that they actually agree, not just that each
		// independently matches C semantics.
		{"let main = () -> i64 => 11 %% -3\n", 2},
	}
	for _, c := range cases {
		if got := buildAndRun(t, c.src); got != c.want {
			t.Errorf("%q exited %d; want %d", c.src, got, c.want)
		}
	}
}

func TestEmit_NilArgs(t *testing.T) {
	if _, err := New().Emit(nil, nil); err == nil {
		t.Fatal("expected an error for nil program/entry point")
	}
}

// TestBackend_SatisfiesContract is a compile-time-ish check that New() is usable
// as the backend.Backend the compiler expects.
func TestBackend_Name(t *testing.T) {
	if got := New().Name(); got != "llvm" {
		t.Fatalf("Name() = %q, want %q", got, "llvm")
	}
}
