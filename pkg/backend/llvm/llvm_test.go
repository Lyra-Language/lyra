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
// lowerEntry's doc comment). Context-directed literal-width inference types the
// body literal `42` as u8 (the declared return type), so it lowers directly as
// an i8 constant and is zero-extended into the i32 slot — no i64→i8 trunc, since
// the literal was never i64 in the first place.
func TestEmit_IntegerLiteralBody(t *testing.T) {
	got, err := emitSource(t, "let main = () -> u8 => 42\n")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"define i32 @main()", "zext i8 42 to i32", "ret i32"} {
		if !strings.Contains(got, want) {
			t.Errorf("emitted IR missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "trunc") {
		t.Errorf("expected no trunc (literal is typed u8 directly), got:\n%s", got)
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
	// -lm links libm: the float backend lowers floor/ceil/round to llvm.*
	// intrinsics and truncated/floored float remainder to fmod, which the
	// linker resolves against the math library.
	if out, err := exec.Command(clang, llPath, "-lm", "-o", binPath).CombinedOutput(); err != nil {
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

// TestExec_IfDivergingBranch is the regression guard for the two-armed-`if` panic:
// a branch ending in return/break/continue seals its own block, so it must NOT get
// an edge to the merge nor a phi incoming. The previous lowerIf unconditionally
// branched both ends to merge (clobbering the seal's terminator) and fed a nil
// value into NewPhi → a compiler nil-pointer panic on entirely idiomatic control
// flow. Each case here used to crash `lyrac`; now it compiles to valid IR and runs.
func TestExec_IfDivergingBranch(t *testing.T) {
	cases := []struct {
		src  string
		want int
	}{
		// then-branch returns, else yields the value (value position, 1 phi incoming).
		{"let f = (x: u8) -> u8 => if x > 1 { return 1 } else { x }\nlet main() -> u8 => f(5)\n", 1},
		{"let f = (x: u8) -> u8 => if x > 1 { return 1 } else { x }\nlet main() -> u8 => f(0)\n", 0},
		// else-branch returns, then yields the value (the mirror).
		{"let f = (x: u8) -> u8 => if x > 1 { x } else { return 9 }\nlet main() -> u8 => f(3)\n", 3},
		// both branches return (statement position, 0 phi incomings — the merge is
		// unreachable and downstream lowering terminates it).
		{"let g = (x: u8) -> u8 => {\n  if x > 1 { return 7 } else { return 2 }\n  0\n}\nlet main() -> u8 => g(5)\n", 7},
		{"let g = (x: u8) -> u8 => {\n  if x > 1 { return 7 } else { return 2 }\n  0\n}\nlet main() -> u8 => g(0)\n", 2},
		// both branches break inside a loop (both seal; the loop back-edge terminates
		// the orphan merge).
		{"let main() -> u8 => {\n  var i: u8 = 0\n  for { if i > 3 { break } else { break } }\n  i\n}\n", 0},
		// a diverging branch in a value-position `let` binding.
		{"let main() -> u8 => {\n  let y: u8 = if false { return 9 } else { 5 }\n  y\n}\n", 5},
	}
	for _, c := range cases {
		if got := buildAndRun(t, c.src); got != c.want {
			t.Errorf("%q exited %d; want %d", c.src, got, c.want)
		}
	}
}

// TestExec_IfVoidBranches: a two-armed `if` used as a *statement* with void
// branches (both side-effecting) is legal, but previously funneled through the
// value path and errored "block has no value". It now lowers for effect (no phi).
func TestExec_IfVoidBranches(t *testing.T) {
	cases := []struct {
		src  string
		want int
	}{
		// else-branch runs (i=0, not > 3): i += 1 → 1.
		{"let main() -> u8 => {\n  var i: u8 = 0\n  if i > 3 { i += 2 } else { i += 1 }\n  i\n}\n", 1},
		// then-branch runs (i=5 > 3): i += 2 → 7.
		{"let main() -> u8 => {\n  var i: u8 = 5\n  if i > 3 { i += 2 } else { i += 1 }\n  i\n}\n", 7},
		// void branches with a heap-string side effect (temps freed in-branch — ASan
		// coverage is in the ownership suite; here we just confirm it runs).
		{"let main() -> u8 => {\n  var i: u8 = 2\n  if i > 3 { println(\"a\" ++ \"b\") } else { println(\"c\" ++ \"d\") }\n  i\n}\n", 2},
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
		// Narrowing WITH overflow — the width-proving cases. Checked arithmetic
		// (Pit-of-Success #2) traps on overflow instead of wrapping, and the trap
		// itself proves the narrow width: the `+` happens at u8/i8 width (300 and
		// 200 overflow there), so it traps (exit 101) — whereas if the conversion
		// were a no-op and the add stayed at i64 width, neither sum would overflow
		// and it would exit with the un-wrapped value.
		{"let main() -> u8 => u8(200) + u8(100)\n", overflowTrapExitCode},     // 300 overflows u8 → trap
		{"let main() -> u8 => u8(i8(100) + i8(100))\n", overflowTrapExitCode}, // 200 overflows signed i8 (max 127) → trap
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

// TestExec_BoolShortCircuit checks && and || by running the compiled program.
// It covers the full truth table for each (so both the taken and the skipped
// branch are exercised), plus chained comparisons that feed real i1 values in
// (`2 < 3 && 5 > 4`) rather than bare literals. Short-circuit itself — that the
// right operand's block is only entered conditionally — is visible in the IR
// (the rhs block is unreachable when the left operand decides the result); a
// faulting right operand to prove it observably isn't expressible yet (no
// runtime-conditional faulting expression), so this pins the values.
func TestExec_BoolShortCircuit(t *testing.T) {
	cases := []struct {
		src  string
		want int
	}{
		// && truth table
		{"let main() -> u8 => if true && true { 1 } else { 0 }\n", 1},
		{"let main() -> u8 => if true && false { 1 } else { 0 }\n", 0},
		{"let main() -> u8 => if false && true { 1 } else { 0 }\n", 0},
		{"let main() -> u8 => if false && false { 1 } else { 0 }\n", 0},
		// || truth table
		{"let main() -> u8 => if true || true { 1 } else { 0 }\n", 1},
		{"let main() -> u8 => if true || false { 1 } else { 0 }\n", 1},
		{"let main() -> u8 => if false || true { 1 } else { 0 }\n", 1},
		{"let main() -> u8 => if false || false { 1 } else { 0 }\n", 0},
		// operands from comparisons, not literals
		{"let main() -> u8 => if 2 < 3 && 5 > 4 { 1 } else { 0 }\n", 1},
		{"let main() -> u8 => if 2 < 3 && 5 < 4 { 1 } else { 0 }\n", 0},
		{"let main() -> u8 => if 2 > 3 || 5 > 4 { 1 } else { 0 }\n", 1},
	}
	for _, c := range cases {
		if got := buildAndRun(t, c.src); got != c.want {
			t.Errorf("%q exited %d; want %d", c.src, got, c.want)
		}
	}
}

func TestExec_VarDecl(t *testing.T) {
	cases := []struct {
		src  string
		want int
	}{
		// A value passed through a `let` binding becomes a concrete i64 (untyped
		// literals default to i64), so — unlike a bare-literal body — it needs an
		// explicit u8(...) to return: Lyra rejects the implicit i64→u8 narrowing.
		{`
let main() -> u8 => {
  let x = 40
  let y = 2
  u8(x + y)
}
`, 42},
		// Sequential rebinding whose RHS reads the prior value (VarDeclStmt.Shadows):
		// the second `x` must see the first x's value (5), not itself.
		{`
let main() -> u8 => {
  let x = 5
  let x = x + 1
  u8(x)
}
`, 6},
		// `var` reassignment: the store updates the alloca and a later read loads
		// the new value. The locals entry must stay the alloca slot across the
		// store, so reading `x` after `x = x + 20` loads through it (not a stale
		// value or a non-alloca).
		{`
let main() -> u8 => {
  var x = 22
  x = x + 20
  u8(x)
}
`, 42},
	}
	for _, c := range cases {
		if got := buildAndRun(t, c.src); got != c.want {
			t.Errorf("%q exited %d; want %d", c.src, got, c.want)
		}
	}
}

// TestExec_ForLoop exercises the cond/body/post/exit CFG end to end. These cases
// use the infinite (`for {}`) and condition-only (`for cond {}`) forms with the
// loop variable as an outer `var`; the three-clause `for var i=…; …; i+=…` form
// is covered separately in TestExec_ForLoopThreeClause. Each case's exit code
// distinguishes a correct CFG from a broken one:
//   - accumulator: proves the back-edge runs the body N times (0+1+2+3+4 = 10).
//   - break: an infinite loop that would never exit without break leaves at 4.
//   - continue: `continue` still runs the loop step, so odd i are skipped and
//     the even values 2+4+6+8+10 = 30 accumulate (proves continue targets the
//     post/step path, not straight back to the condition losing the increment).
//   - nested + labeled break: `break outer` from the inner loop exits both;
//     control reaches it after exactly two inner increments, so 2.
func TestExec_ForLoop(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want int
	}{
		{
			"accumulator",
			`
let main() -> u8 => {
  var s = 0
  var i = 0
  for i < 5 {
    s = s + i
    i = i + 1
  }
  u8(s)
}
`,
			10,
		},
		{
			"infinite + break",
			`
let main() -> u8 => {
  var s = 0
  for {
    if s > 3 { break }
    s = s + 1
  }
  u8(s)
}
`,
			4,
		},
		{
			"continue",
			`
let main() -> u8 => {
  var s = 0
  var i = 0
  for i < 10 {
    i = i + 1
    if i % 2 == 1 { continue }
    s = s + i
  }
  u8(s)
}
`,
			30,
		},
		{
			"nested + labeled break",
			`
let main() -> u8 => {
  var c = 0
  var i = 0
  var j = 0
  outer: for i < 3 {
    j = 0
    for j < 3 {
      if j == 2 { break outer }
      c = c + 1
      j = j + 1
    }
    i = i + 1
  }
  u8(c)
}
`,
			2,
		},
	}
	for _, c := range cases {
		if got := buildAndRun(t, c.src); got != c.want {
			t.Errorf("%s: exited %d; want %d", c.name, got, c.want)
		}
	}
}

// TestExec_ForLoopThreeClause covers the full `for var i = init; cond; i op= step`
// form, now that the two frontend gaps are fixed (a MathAssignOpExpr inferExprType
// case for the `+=` post/body; entering the loop's scope so the init variable
// resolves in the condition/post/body). Each case's exit code pins a distinct
// behavior:
//   - upward `+= 1` with the counter read in the body: 0+1+2+3+4 = 10.
//   - `+=` used *inside* the body (not just the post) also lowers (load/op/store).
//   - downward `-= 1` from 5 while `i > 0`: 5+4+3+2+1 = 15 (proves the post step
//     and condition compose for a decreasing counter).
//   - a narrow `u8` counter: the `+=` RHS literal takes the counter's width
//     (checkMathAssignOp propagation), so it lowers at u8 instead of mismatching.
func TestExec_ForLoopThreeClause(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want int
	}{
		{
			"upward",
			`
let main() -> u8 => {
  var s = 0
  for var i = 0; i < 5; i += 1 {
    s = s + i
  }
  u8(s)
}
`,
			10,
		},
		{
			"compound-assign in body",
			`
let main() -> u8 => {
  var s = 0
  for var i = 0; i < 5; i += 1 {
    s += i
  }
  u8(s)
}
`,
			10,
		},
		{
			"downward",
			`
let main() -> u8 => {
  var s = 0
  for var i = 5; i > 0; i -= 1 {
    s += i
  }
  u8(s)
}
`,
			15,
		},
		{
			"narrow u8 counter",
			`
let main() -> u8 => {
  var s: u8 = 0
  for var i: u8 = 0; i < 5; i += 1 {
    s = s + i
  }
  u8(s)
}
`,
			10,
		},
	}
	for _, c := range cases {
		if got := buildAndRun(t, c.src); got != c.want {
			t.Errorf("%s: exited %d; want %d", c.name, got, c.want)
		}
	}
}

// TestExec_Functions exercises user-defined function definitions and calls end
// to end. Each case's exit code pins a distinct piece of the two-pass
// declare/define machinery:
//   - simple call: params bind (as allocas) and the return value flows back.
//   - narrow-arg literal: the arg `200`/`100` take the u8 param width (arg
//     propagation), so `a + b` wraps mod 256 to 44 — proof the args are i8, not
//     i64. i64 args would have been a hard IR error, not 44.
//   - early return: an explicit `return` inside a one-armed `if` seals the block
//     and returns 7; the tail `x` is the not-taken path.
//   - recursion: factorial(5) = 120 — the self-call resolves because all
//     functions are declared before any body is lowered.
//   - mutual recursion: isEven/isOdd call each other — forward reference across
//     two functions.
//   - call in a loop: inc(i) is called each iteration; 1+2+3+4+5 = 15.
func TestExec_Functions(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want int
	}{
		{
			"simple call",
			`
let add = (a: u8, b: u8) -> u8 => a + b
let main() -> u8 => add(40, 2)
`,
			42,
		},
		{
			// The literal args narrow to u8, so `a + b` happens at u8 width where
			// 200 + 100 = 300 overflows — checked arithmetic traps (exit 101)
			// rather than wrapping. (A wide-width add wouldn't overflow, so the
			// trap also proves the args narrowed.)
			"narrow-arg overflow traps at u8",
			`
let add = (a: u8, b: u8) -> u8 => a + b
let main() -> u8 => add(200, 100)
`,
			overflowTrapExitCode,
		},
		{
			"early return",
			`
let clamp = (x: u8) -> u8 => {
  if x > 7 { return 7 }
  x
}
let main() -> u8 => clamp(200)
`,
			7,
		},
		{
			"recursion",
			`
let fact = (n: u8) -> u8 => {
  if n <= 1 { return 1 }
  n * fact(n - 1)
}
let main() -> u8 => fact(5)
`,
			120,
		},
		{
			"mutual recursion",
			`
let isEven = (n: u8) -> u8 => {
  if n == 0 { return 1 }
  isOdd(n - 1)
}
let isOdd = (n: u8) -> u8 => {
  if n == 0 { return 0 }
  isEven(n - 1)
}
let main() -> u8 => isEven(10)
`,
			1,
		},
		{
			"call in a loop",
			`
let inc = (x: u8) -> u8 => x + 1
let main() -> u8 => {
  var s: u8 = 0
  for var i: u8 = 0; i < 5; i += 1 {
    s = s + inc(i)
  }
  u8(s)
}
`,
			15,
		},
	}
	for _, c := range cases {
		if got := buildAndRun(t, c.src); got != c.want {
			t.Errorf("%s: exited %d; want %d", c.name, got, c.want)
		}
	}
}

// TestExec_MixedWidthComparison: `i8(5) < 3` used to be deferred (the bare `3`
// lowered to i64, mismatching the i8 lhs). Context-directed literal-width
// inference now types `3` as i8 from its concrete sibling, so the comparison
// lowers to a single i8 `icmp` and runs: 5 < 3 is false, so the else branch
// (0) is taken.
func TestExec_MixedWidthComparison(t *testing.T) {
	if got := buildAndRun(t, "let main() -> u8 => if i8(5) < 3 { 1 } else { 0 }\n"); got != 0 {
		t.Errorf("i8(5) < 3 exited %d; want 0 (false → else)", got)
	}
	if got := buildAndRun(t, "let main() -> u8 => if i8(5) < 100 { 1 } else { 0 }\n"); got != 1 {
		t.Errorf("i8(5) < 100 exited %d; want 1 (true → then)", got)
	}
}

// TestExec_LiteralWidthArithmetic proves context-directed literal-width
// inference lowers arithmetic at the narrow width, not i64. Each case is chosen
// so the answer *differs* if the untyped literals were left i64:
//   - `x + 3` (x: i8): the `3` takes i8; without inference it'd be i64 and the
//     i8+i64 add would be a hard IR error, not 8.
//   - `(x * 20) / 7` (x: u8): u8-width `20*20` wraps mod 256 to 144, so 144/7=20.
//     Done at i64 it would be 400/7=57 — a different exit code, so this pins the
//     wrap to the operand width, not just the final truncation.
//   - `var y: u8`, `y = y + x`: reassignment RHS literals/width flow through the
//     alloca; 100+200 wraps to 44 at u8.
func TestExec_LiteralWidthArithmetic(t *testing.T) {
	cases := []struct {
		src  string
		want int
	}{
		{`
let main() -> u8 => {
  let x: i8 = 5
  u8(x + 3)
}
`, 8},
		// `x * 20` happens at u8 width (x is u8), where 20 * 20 = 400 overflows —
		// checked arithmetic traps rather than wrapping (400 mod 256 = 144, then
		// /7 = 20 under the old silent-wrap semantics). x is a parameter so the
		// overflow is opaque to the value-range analysis (this tests the runtime
		// trap, not the compile-time overflow error).
		{`
let f = (x: u8) -> u8 => (x * 20) / 7
let main() -> u8 => f(20)
`, overflowTrapExitCode},
		// `y + x` at u8 width overflows (100 + 200 = 300) — traps rather than
		// wrapping to 44. Operands via parameters (opaque to lyra-E020).
		{`
let f = (x: u8, y: u8) -> u8 => { var v = y
  v = v + x
  u8(v) }
let main() -> u8 => f(200, 100)
`, overflowTrapExitCode},
	}
	for _, c := range cases {
		if got := buildAndRun(t, c.src); got != c.want {
			t.Errorf("%q exited %d; want %d", c.src, got, c.want)
		}
	}
}

// TestEmit_LiteralExceedsContextWidth_Error: a literal too large for the width
// its context demands (`i8(5) < 300` — 300 doesn't fit i8) is deliberately left
// untyped by the typechecker rather than silently wrapped, so it lowers to the
// i64 default and mismatches the i8 lhs. The backend's defensive width-mismatch
// guard then fires — a loud error, not miscompiled code. (A nicer typechecker
// diagnostic for this is a possible follow-up; today it's caught at lowering.)
func TestEmit_LiteralExceedsContextWidth_Error(t *testing.T) {
	_, err := emitSource(t, "let main() -> u8 => if i8(5) < 300 { 1 } else { 0 }\n")
	if err == nil {
		t.Fatal("expected an error: 300 does not fit i8, leaving a mismatched-width comparison")
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

// TestExec_ConstValues: top-level `const` declarations lower — a reference inlines
// the const's compile-time value. Previously the backend errored "unbound
// identifier" (consts type-checked but never lowered).
func TestExec_ConstValues(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want int
	}{
		{"scalar const", "const MAX = 100\nlet main() -> u8 => u8(MAX)\n", 100},
		{"const arithmetic", "const AREA = 10 * 20\nlet main() -> u8 => u8(AREA - 44)\n", 156},
		{"annotated const used directly", "const N: u8 = 42\nlet main() -> u8 => N\n", 42},
		{"const in an expression", "const BASE = 10\nlet main() -> u8 => u8(BASE + 5)\n", 15},
		{"const referencing a const", "const A = 5\nconst B = A + 1\nlet main() -> u8 => u8(B)\n", 6},
	}
	for _, c := range cases {
		if got := buildAndRun(t, c.src); got != c.want {
			t.Errorf("%s: exited %d; want %d", c.name, got, c.want)
		}
	}
}
