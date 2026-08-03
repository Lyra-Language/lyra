package llvm

import (
	"github.com/Lyra-Language/lyra/pkg/driver"
	"strings"
	"testing"
)

// Bitwise and shift operators (`& | ~ << >>`, prefix `~`), lowered in
// arithmetic.go. Xor is `~` rather than `^`, which is taken by raw-pointer types
// and postfix deref — see tree-sitter-lyra's CLAUDE.md.

func TestExec_BitwiseOps(t *testing.T) {
	t.Parallel()
	cases := []struct {
		src  string
		want int
	}{
		{"let main = () -> u8 => 12 & 10\n", 8},  // 1100 & 1010 == 1000
		{"let main = () -> u8 => 12 | 10\n", 14}, // 1100 | 1010 == 1110
		{"let main = () -> u8 => 12 ~ 10\n", 6},  // 1100 ^ 1010 == 0110
		{"let main = () -> u8 => 1 << 4\n", 16},  //
		{"let main = () -> u8 => 64 >> 3\n", 8},  //
		{"let main = () -> u8 => 0b1010 & 0b0110\n", 2},
		{"let main = () -> u8 => 0xF0 >> 4\n", 15},
		// Complement of an unsigned zero is all ones at that width.
		{"let main = () -> u8 => ~u8(0)\n", 255},
		// ...and complementing twice is the identity.
		{"let main = () -> u8 => ~~u8(42)\n", 42},
	}
	for _, c := range cases {
		t.Run("", func(t *testing.T) {
			t.Parallel()
			if got := buildAndRun(t, c.src); got != c.want {
				t.Errorf("%q exited %d; want %d", c.src, got, c.want)
			}
		})
	}
}

// `>>` is arithmetic on a signed operand (sign-filling) and logical on an
// unsigned one (zero-filling). This is the one place the two spellings of `>>`
// differ, and getting it backwards is a wrong answer rather than a crash — so it
// is checked by running both and comparing against the known results.
func TestExec_ShiftRightSignedness(t *testing.T) {
	t.Parallel()
	const src = `let main = () -> u8 => {
  let signed: i8 = -8
  let unsigned: u8 = 248
  var score: u8 = 0
  if signed >> 1 == -4 { score += 1 }
  if unsigned >> 1 == 124 { score += 2 }
  score
}
`
	// -8 >> 1 is -4 only under an arithmetic shift; 248 >> 1 is 124 only under a
	// logical one. A backend using ashr for both would give 248 >> 1 == 252.
	if got := buildAndRun(t, src); got != 3 {
		t.Errorf("exited %d; want 3 (1 = signed ashr, 2 = unsigned lshr)\n%s", got, src)
	}
}

// A shift amount at or beyond the operand's width is undefined behavior in raw
// LLVM, so it traps instead — the same treatment divide-by-zero gets, and for the
// same reason: the alternative is an answer that depends on the target's shift
// hardware.
func TestExec_ShiftOverflowTraps(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
	}{
		{
			"shift left at the width",
			`let shift = (n: i64, by: i64) -> i64 => n << by
			 let main = () -> u8 => u8(shift(1, 64))` + "\n",
		},
		{
			"shift right beyond the width",
			`let shift = (n: i64, by: i64) -> i64 => n >> by
			 let main = () -> u8 => u8(shift(1, 100))` + "\n",
		},
		{
			// Negative counts are caught by the same unsigned comparison: as a
			// two's-complement bit pattern, -1 is enormous.
			"negative shift amount",
			`let shift = (n: i64, by: i64) -> i64 => n << by
			 let main = () -> u8 => u8(shift(1, 0 - 1))` + "\n",
		},
		{
			// The count is narrower than the value, so it is widened before the
			// comparison — a check done after truncation would miss this.
			"narrow operand, in-range-looking count",
			`let shift = (n: u8, by: u8) -> u8 => n << by
			 let main = () -> u8 => shift(1, 8)` + "\n",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			out, code := buildAndRunPanic(t, c.src)
			if code != overflowTrapExitCode {
				t.Errorf("exited %d; want %d (the trap)\n%s", code, overflowTrapExitCode, c.src)
			}
			if !strings.Contains(out, "shift amount out of range") {
				t.Errorf("stderr = %q; want the shift-amount trap message", out)
			}
		})
	}
}

// A shift by a compile-time constant already in range emits no check at all —
// the overwhelmingly common `x << 3`. Asserted on the IR because the whole point
// is the absence of a branch, which running the program cannot observe.
func TestEmit_ConstantShiftEmitsNoTrap(t *testing.T) {
	t.Parallel()
	got, err := emitSource(t, "let main = () -> u8 => 1 << 3\n")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "lyra_panic_shift_overflow") {
		t.Errorf("a constant in-range shift should emit no trap:\n%s", got)
	}
	// ...while a variable amount keeps it.
	got, err = emitSource(t, `let shift = (n: i64, by: i64) -> i64 => n << by
let main = () -> u8 => u8(shift(1, 2))
`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "lyra_panic_shift_overflow") {
		t.Errorf("a variable shift amount must keep its check:\n%s", got)
	}
}

// The point of teaching the value-range pass about bitwise results: a mask bounds
// its output, so the *arithmetic that follows* can be proved in range and drop its
// overflow trap. `x & 0x0F` is [0,15], so `+ 1` is [1,16] — comfortably inside u8.
//
// The unmasked control is the anti-vacuity half. Without it this test would pass
// just as well if the backend had stopped emitting checked adds altogether, which
// is the failure mode that makes an elision test worth writing carefully.
func TestEmit_MaskBoundsTheFollowingArithmetic(t *testing.T) {
	t.Parallel()
	masked, err := emitSource(t, `let f = (x: u8) -> u8 => (x & 0x0F) + 1
let main = () -> u8 => f(200)
`)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(masked, "add.with.overflow") {
		t.Errorf("a masked value should prove the addition safe and drop the trap:\n%s", masked)
	}

	unmasked, err := emitSource(t, `let f = (x: u8) -> u8 => x + 1
let main = () -> u8 => f(200)
`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(unmasked, "add.with.overflow") {
		t.Errorf("an unbounded u8 + 1 can overflow and must keep its check:\n%s", unmasked)
	}
}

// A *variable* shift count drops its check when the value-range pass can bound it —
// here by branch refinement, which pins `n` to [0,7] inside the `if`. The constant
// case was already folded away at lowering; this is the half that needed the
// analysis (NoShiftOverflow), and it is the shape real code takes.
func TestEmit_BoundedShiftCountElidesTheCheck(t *testing.T) {
	t.Parallel()
	bounded, err := emitSource(t, `let f = (x: u8, n: u8) -> u8 => if n < 8 { x << n } else { 0 }
let main = () -> u8 => f(1, 3)
`)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(bounded, "lyra_panic_shift_overflow") {
		t.Errorf("a count refined to [0,7] cannot trap; the check should be gone:\n%s", bounded)
	}

	// The control: the same shift with nothing bounding the count keeps its check.
	// Without this, the test would pass just as well if shifts had stopped emitting
	// checks altogether.
	unbounded, err := emitSource(t, `let f = (x: u8, n: u8) -> u8 => x << n
let main = () -> u8 => f(1, 3)
`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(unbounded, "lyra_panic_shift_overflow") {
		t.Errorf("an unbounded count can trap and must keep its check:\n%s", unbounded)
	}
}

// ...and the elided form still computes the right answer, which the IR assertion
// above cannot show.
func TestExec_BoundedShiftStillComputes(t *testing.T) {
	t.Parallel()
	const src = `let f = (x: u8, n: u8) -> u8 => if n < 8 { x << n } else { 0 }
let main = () -> u8 => f(3, 4) + f(1, 9)
`
	// 3 << 4 == 48, and the guarded else gives 0 for the out-of-range count.
	if got := buildAndRun(t, src); got != 48 {
		t.Errorf("exited %d; want 48\n%s", got, src)
	}
}

func TestExec_BitwiseCompoundAssignment(t *testing.T) {
	t.Parallel()
	const src = `let main = () -> u8 => {
  var x: u8 = 12
  x &= 10
  var y: u8 = 12
  y |= 10
  var z: u8 = 12
  z ~= 10
  var s: u8 = 1
  s <<= 3
  var r: u8 = 64
  r >>= 3
  if x == 8 && y == 14 && z == 6 && s == 8 && r == 8 { 42 } else { 0 }
}
`
	if got := buildAndRun(t, src); got != 42 {
		t.Errorf("exited %d; want 42\n%s", got, src)
	}
}

// The precedence chosen in the grammar, verified by what the program computes
// rather than by the parse tree: bitwise binds tighter than comparison (C's
// classic footgun), looser than arithmetic, and `&` > `~` > `|`.
// `x <<= n` types its count independently of the target, exactly as `x = x << n`
// does. Before, the compound form went through the assignability check and demanded
// `u8(n)` for a `u32` count — a conversion the binary spelling never asked for, and
// an asymmetry between two spellings of the same operation.
func TestExec_CompoundShiftTakesAnyIntegerCount(t *testing.T) {
	t.Parallel()
	const src = `let f = (x: u8, n: u32) -> u8 => {
  var v: u8 = x
  v <<= n
  v
}
let g = (x: u8, n: u32) -> u8 => x << n
let main = () -> u8 => f(3, 4) - g(3, 3)
`
	// 3 << 4 == 48, 3 << 3 == 24; the two spellings now accept the same count type.
	if got := buildAndRun(t, src); got != 24 {
		t.Errorf("exited %d; want 24\n%s", got, src)
	}
}

// ...and the count is still checked against the *shifted* width, not its own: a u32
// count of 8 is a perfectly ordinary u32 but out of range for a u8 shift.
func TestExec_CompoundShiftStillTrapsOnAWideCount(t *testing.T) {
	t.Parallel()
	const src = `let f = (x: u8, n: u32) -> u8 => {
  var v: u8 = x
  v <<= n
  v
}
let main = () -> u8 => f(1, 8)
`
	out, code := buildAndRunPanic(t, src)
	if code != overflowTrapExitCode {
		t.Errorf("exited %d; want %d (the trap)", code, overflowTrapExitCode)
	}
	if !strings.Contains(out, "shift amount out of range") {
		t.Errorf("stderr = %q; want the shift-amount trap message", out)
	}
}

func TestExec_BitwisePrecedence(t *testing.T) {
	t.Parallel()
	// Each `if` is one precedence rule, and the operands are chosen so the *other*
	// grouping gives a different answer — otherwise the test would pass either way:
	//
	//   12 & 8 == 8   →  (12 & 8) == 8 is true; 12 & (8 == 8) is not even well-typed
	//   3 | 1 + 1     →  3 | (1 + 1) == 3, but (3 | 1) + 1 == 4
	//   1 << 2 + 1    →  (1 << 2) + 1 == 5, but 1 << (2 + 1) == 8
	//   12 & 10 | 1   →  (12 & 10) | 1 == 9, but 12 & (10 | 1) == 8
	const src = `let main = () -> u8 => {
  var score: u8 = 0
  if 12 & 8 == 8 { score += 1 }
  if 3 | 1 + 1 == 3 { score += 2 }
  if 1 << 2 + 1 == 5 { score += 4 }
  if 12 & 10 | 1 == 9 { score += 8 }
  score
}
`
	if got := buildAndRun(t, src); got != 15 {
		t.Errorf("exited %d; want 15 (each bit is one precedence rule)\n%s", got, src)
	}
}

// Bitwise operators are integers-only: there is no meaningful bit operation on a
// float short of reinterpreting its IEEE representation, which is the kind of
// platform-shaped behaviour the fixed-width primitives exist to keep out.
func TestBitwiseRejectsFloats(t *testing.T) {
	t.Parallel()
	cases := []string{
		"let main = () -> u8 => { let a: f64 = 1.5\n let b: f64 = 2.5\n u8(a & b) }\n",
		"let main = () -> u8 => { let a: f64 = 1.5\n u8(~a) }\n",
		"let main = () -> u8 => { let a: f64 = 1.5\n u8(a << 1) }\n",
	}
	for _, src := range cases {
		t.Run("", func(t *testing.T) {
			t.Parallel()
			// driver.Analyze directly rather than emitSource, which fatals on
			// analysis errors — here the error *is* the expected result.
			res := driver.Analyze([]byte(src))
			if !res.HasErrors() {
				t.Errorf("expected a type error for a float operand:\n%s", src)
				return
			}
			var joined string
			for _, d := range res.Errors() {
				joined += d.Message + "\n"
			}
			if !strings.Contains(joined, "must be an integer") &&
				!strings.Contains(joined, "must be integers") {
				t.Errorf("diagnostics = %q; want one naming the integer requirement", joined)
			}
		})
	}
}
