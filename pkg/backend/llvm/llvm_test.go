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

// TestEmit_IntegerLiteralBody: a u8 entry whose body is an integer literal
// returns that literal — the source value reaches the exit code. `@main`
// itself is i32 (the actual C ABI signature, not Lyra's u8 return type — see
// lowerEntry's doc comment), so the u8 body value is coerced (trunc, since a
// bare literal still lowers to i64 — literal-width inference isn't
// context-directed yet) and then zero-extended into that i32 slot.
func TestEmit_IntegerLiteralBody(t *testing.T) {
	got, err := emitSource(t, "let main = () -> u8 => 42\n")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"define i32 @main()", "trunc i64 42 to i8", "zext i8", "ret i32"} {
		if !strings.Contains(got, want) {
			t.Errorf("emitted IR missing %q:\n%s", want, got)
		}
	}
}

// TestEmit_VoidEntry: a void entry exits 0 (as i32, `@main`'s actual C ABI
// return type regardless of Lyra's own void/u8 entry-point convention).
func TestEmit_VoidEntry(t *testing.T) {
	got, err := emitSource(t, "let main = () -> void => {}\n")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "ret i32 0") {
		t.Errorf("void entry should `ret i32 0`:\n%s", got)
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
		{"let main() -> u8 => 42\n", 42},
		{"let main() -> u8 => 1 + 2\n", 3},
		{"let main() -> u8 => 20 - 6\n", 14},
		{"let main() -> u8 => 2 * 3 + 4\n", 10},
		{"let main() -> u8 => 6 / 2\n", 3},
		{"let main() -> u8 => 10 % 3\n", 1},
	}
	for _, c := range cases {
		if got := buildAndRun(t, c.src); got != c.want {
			t.Errorf("%q exited %d; want %d", c.src, got, c.want)
		}
	}
}

// TestExec_SignedArithmetic checks signed division and Mod (%) against
// C-style truncating semantics (sign follows the dividend) — LLVM's sdiv/srem
// give this natively. Expected values are verified against a real C program,
// not computed by hand: -1/2 truncates toward zero (0, not floor's -1), and
// 11 % -3's sign follows the positive dividend (2, not floor's -1). Covers
// every sign combination for both operators: -/- and +/- division, and
// -/+ and -/- for Mod (-11 % 3 = -2 directly contrasts with
// TestExec_FlooredRemainder's -11 %% 3 = 1 on the same operands).
//
// Each body is wrapped in an outer u8(...): negating a literal produces
// UntypedSignedInt (isAssignable correctly rejects assigning that directly to
// unsigned u8 — a negative literal genuinely can't implicitly become
// unsigned), so an explicit conversion is required regardless of main's
// return type.
func TestExec_SignedArithmetic(t *testing.T) {
	cases := []struct {
		src  string
		want int
	}{
		{"let main() -> u8 => u8(-1 / 2)\n", 0},
		{"let main() -> u8 => u8(11 % -3)\n", 2},
		// -/- division truncates toward zero to a positive quotient: -7/-2 =
		// 3.5 truncated = 3 (no wrap; fits u8 directly).
		{"let main() -> u8 => u8(-7 / -2)\n", 3},
		// +/- division: 7/-2 = -3.5 truncated toward zero = -3, which as an
		// 8-bit exit code wraps to 253 (256-3).
		{"let main() -> u8 => u8(7 / -2)\n", 253},
		// Mod's sign follows the (negative) dividend here, not the (positive)
		// divisor: -11 % 3 = -2, wrapping to 254 (256-2). The direct sign
		// contrast with -11 %% 3 = 1 (TestExec_FlooredRemainder) is the point.
		{"let main() -> u8 => u8(-11 % 3)\n", 254},
		// Both operands negative: -7 % -2 = -1 (sign follows the dividend,
		// itself negative), wrapping to 255 (256-1).
		{"let main() -> u8 => u8(-7 % -2)\n", 255},
	}
	for _, c := range cases {
		if got := buildAndRun(t, c.src); got != c.want {
			t.Errorf("%q exited %d; want %d", c.src, got, c.want)
		}
	}
}

// TestExec_FlooredRemainder checks Remainder (%%) against Odin's "remainder
// (floored)" semantics (odin-lang.org/docs/overview/#arithmetic-operators):
// sign follows the divisor, distinct from Mod (%) above, whose sign follows
// the dividend. 11 %% -3 = -1 (vs Mod's 2 for the same operands — the
// floor(11/-3) = -4, so -4*-3 + (-1) = 11); -11 %% 3 = 1 (vs Mod's -2).
// Covers every sign combination that distinguishes floored from truncated
// (both-positive is identical either way, so it isn't informative here).
// Expected values are verified against the floor(a/b) definition — the
// neg/neg case additionally cross-checked via Python, whose `%` is genuinely
// floored — not just asserted from whatever the implementation produces.
//
// Each body is wrapped in an outer u8(...) for the same reason as
// TestExec_SignedArithmetic: negating a literal produces UntypedSignedInt,
// which isAssignable correctly refuses to assign directly to unsigned u8.
func TestExec_FlooredRemainder(t *testing.T) {
	cases := []struct {
		src  string
		want int
	}{
		{"let main() -> u8 => u8(-11 %% 3)\n", 1},
		// 11 %% -3 = -1, which as an 8-bit process exit code wraps to 255
		// (256 - 1) — same pairing as TestExec_SignedArithmetic's `11 % -3`
		// above (= 2), so the two tests directly contrast Mod vs Remainder on
		// identical operands.
		{"let main() -> u8 => u8(11 %% -3)\n", 255},
		// Both operands negative: floor(-7/-2) = floor(3.5) = 3, so
		// -7 %% -2 = -7 - (3 * -2) = -1 (sign follows the negative divisor),
		// wrapping to 255 (256-1) — same wrapped byte as the case above, but a
		// distinct source expression contrasted against
		// TestExec_SignedArithmetic's -7 % -2 (also -1, since sign-follows-
		// dividend and sign-follows-divisor happen to agree when both operands
		// share a sign).
		{"let main() -> u8 => u8(-7 %% -2)\n", 255},
	}
	for _, c := range cases {
		if got := buildAndRun(t, c.src); got != c.want {
			t.Errorf("%q exited %d; want %d", c.src, got, c.want)
		}
	}
}

// TestExec_If checks if/else lowering by running the compiled program: the
// right branch must actually be taken (the phi selects on the predecessor
// block, so this verifies both the cond-br and the phi). Conditions are bool
// literals for now — comparisons aren't lowered yet, so a non-constant
// condition isn't expressible; -O0 doesn't fold the branch, so the cond-br is
// still exercised at runtime.
func TestExec_If(t *testing.T) {
	cases := []struct {
		src  string
		want int
	}{
		{"let main() -> u8 => if true { 1 } else { 2 }\n", 1},
		{"let main() -> u8 => if false { 1 } else { 2 }\n", 2},
		// Arithmetic in the branches — the branch value is a real computation,
		// not just a literal, so this also confirms instructions land in the
		// correct branch block.
		{"let main() -> u8 => if false { 1 } else { 3 * 4 }\n", 12},
		{"let main() -> u8 => if false { 1 } else if true { 2 } else { 3 }\n", 2},
		// Nested if inside a branch: the branch's control ends in the INNER
		// merge block, so the outer phi must reference that, not the outer
		// branch block. A wrong predecessor here would be invalid IR (clang
		// rejects) or the wrong value — this is the block-threading payoff.
		{"let main() -> u8 => if true { if false { 10 } else { 20 } } else { 30 }\n", 20},
		{"let main() -> u8 => if false { 10 } else { if true { 40 } else { 50 } }\n", 40},
	}
	for _, c := range cases {
		if got := buildAndRun(t, c.src); got != c.want {
			t.Errorf("%q exited %d; want %d", c.src, got, c.want)
		}
	}
}

// TestExec_IntWidthConversions checks Lyra's one conversion syntax (`i8(x)`,
// `u32(x)`, …, Pit-of-Success #5) narrows/widens to the actual bit width, not
// just the right value in the common case. A signed result (from `i8(...)`
// arithmetic) is wrapped in an outer `u8(...)` since main's own return type is
// u8 (unsigned) and isAssignable doesn't permit an implicit i8→u8 conversion
// between two concrete, differently-signed types — a conversion call is the
// only vehicle for a width or signedness change like this.
func TestExec_IntWidthConversions(t *testing.T) {
	cases := []struct {
		src  string
		want int
	}{
		// Narrowing, no overflow: fits cleanly in the target width. u8(...)
		// arithmetic is already main's declared return type, no wrapper
		// needed; i8(...) arithmetic needs an outer u8(...) (signedness
		// differs, even though both are 8 bits).
		{"let main() -> u8 => u8(200) + u8(50)\n", 250},
		{"let main() -> u8 => u8(i8(100) + i8(20))\n", 120},
		// Narrowing WITH overflow — the width-proving cases. If the add
		// silently stayed at i64 width (i.e. the conversion were a no-op
		// instead of a real trunc), these would give the un-wrapped sums
		// (300, 200) instead.
		{"let main() -> u8 => u8(200) + u8(100)\n", 44},      // 300 wraps mod 256 = 44
		{"let main() -> u8 => u8(i8(100) + i8(100))\n", 200}, // 200 (=100+100) doesn't fit signed i8 (-128..127); wraps to the bit pattern for -56, which as an exit code (mod 256, i.e. 256-56) reads back as 200
		// Widening: zext (unsigned source) vs sext (signed source) must pick
		// correctly, not just "a wide value of the right magnitude" — sign-
		// extending vs zero-extending a negative i8 differ by exactly 256,
		// which an additive exit-code check can't see (invisible mod 256), so
		// this uses division to actually distinguish them: sign-extended -1
		// truncate-divides to 0; zero-extended (bug) would give 255/2 = 127.
		// The outer u8(...) brings the i64 division result back to main's
		// declared return type.
		{"let main = () -> u8 => u8(i64(i8(-1)) / 2)\n", 0},
	}
	for _, c := range cases {
		if got := buildAndRun(t, c.src); got != c.want {
			t.Errorf("%q exited %d; want %d", c.src, got, c.want)
		}
	}
}

func TestEmit_BoolBinaryOp(t *testing.T) {
	cases := []struct {
		src  string
		want int
	}{
		{"let main() -> u8 => if true == false { 1 } else { 0 }\n", 0},
		{"let main() -> u8 => if true != false { 1 } else { 0 }\n", 1},
		{"let main() -> u8 => if 2 == 3 { 1 } else { 0 }\n", 0},
		{"let main() -> u8 => if 2 == 2 { 1 } else { 0 }\n", 1},
		{"let main() -> u8 => if 2 != 2 { 1 } else { 0 }\n", 0},
		{"let main() -> u8 => if 2 != 3 { 1 } else { 0 }\n", 1},
		{"let main() -> u8 => if 2 < 3 { 1 } else { 0 }\n", 1},
		{"let main() -> u8 => if 2 < 2 { 1 } else { 0 }\n", 0},
		{"let main() -> u8 => if 2 > 3 { 1 } else { 0 }\n", 0},
		{"let main() -> u8 => if 2 > 2 { 1 } else { 0 }\n", 0},
		{"let main() -> u8 => if 2 <= 3 { 1 } else { 0 }\n", 1},
		{"let main() -> u8 => if 2 <= 2 { 1 } else { 0 }\n", 1},
		{"let main() -> u8 => if 2 >= 3 { 1 } else { 0 }\n", 0},
		{"let main() -> u8 => if 2 >= 2 { 1 } else { 0 }\n", 1},
	}
	for _, c := range cases {
		if got := buildAndRun(t, c.src); got != c.want {
			t.Errorf("%q exited %d; want %d", c.src, got, c.want)
		}
	}
}

// TestEmit_ComparisonMixedWidth_Error: comparing operands of different integer
// widths (`i8(5) < 3` mixes i8 and i64, since a bare literal still lowers to
// i64) is deferred pending context-directed literal-width inference. It type-
// checks (the typechecker considers i8 and an untyped literal compatible) but
// the backend errors explicitly rather than emitting an invalid `icmp` clang
// would reject.
func TestEmit_ComparisonMixedWidth_Error(t *testing.T) {
	_, err := emitSource(t, "let main() -> u8 => if i8(5) < 3 { 1 } else { 0 }\n")
	if err == nil {
		t.Fatal("expected an error for a mismatched-width comparison")
	}
	if !strings.Contains(err.Error(), "mismatched integer widths") {
		t.Errorf("expected a width-mismatch error, got: %v", err)
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
