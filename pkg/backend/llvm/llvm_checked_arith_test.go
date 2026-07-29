package llvm

import (
	"strings"
	"testing"
)

// Checked arithmetic (Pit-of-Success #2): plain `+`/`-`/`*` on integers trap on
// overflow rather than silently wrapping. A trap writes to stderr and exits with
// overflowTrapExitCode (101), so a program that overflows exits 101 while a
// non-overflowing one returns its real value.

// overflowingCases each overflow and must trap (exit 101). They span signed and
// unsigned, all three ops, and narrow (i8/u8) plus wide (i32/i64) widths so the
// per-width intrinsic selection is exercised.
var overflowingCases = []struct {
	name string
	src  string
}{
	// Operands are passed through function parameters so they're opaque to the
	// value-range analysis (lyra-E020) — these exercise the *runtime* trap on
	// values it can't prove overflowing statically, not the compile-time error.
	{"u8 add wraps past 255", `let add = (x: u8, y: u8) -> u8 => x + y
	let main = () -> u8 => add(200, 100)`},
	{"u8 sub below 0", `let sub = (x: u8, y: u8) -> u8 => x - y
	let main = () -> u8 => sub(10, 20)`},
	{"i8 mul past 127", `let mul = (x: i8, y: i8) -> u8 => u8(x * y)
	let main = () -> u8 => mul(100, 2)`},
	{"i32 signed mul overflow", `let mul = (x: i32, y: i32) -> u8 => u8(x * y)
	let main = () -> u8 => mul(100000, 100000)`},
	{"i64 add overflow", `let add = (x: i64, y: i64) -> u8 => u8(x + y)
	let main = () -> u8 => add(9223372036854775807, 1)`},
	{"compound += overflow", `let bump = (x: u8) -> u8 => {
	  var v = x
	  v += 10
	  v
	}
	let main = () -> u8 => bump(250)`},
}

// TestExec_CheckedArithmetic_Traps runs each overflowing program and asserts it
// traps (exit 101) rather than wrapping to a value.
func TestExec_CheckedArithmetic_Traps(t *testing.T) {
	t.Parallel()
	for _, c := range overflowingCases {
		t.Run("", func(t *testing.T) {
			t.Parallel()
			if got := buildAndRun(t, c.src); got != overflowTrapExitCode {
				t.Errorf("%s: exited %d; want %d (overflow trap)", c.name, got, overflowTrapExitCode)
			}
		})
	}
}

// nonOverflowingCases compute a real value with no overflow — checked arithmetic
// must be transparent when nothing overflows.
var nonOverflowingCases = []struct {
	name string
	src  string
	want int
}{
	{"u8 add in range", `let main = () -> u8 => {
	  let x: u8 = 20
	  let y: u8 = 22
	  x + y
	}`, 42},
	{"i32 mul then narrow", `let main = () -> u8 => {
	  let x: i32 = 6
	  let y: i32 = 7
	  u8(x * y)
	}`, 42},
	{"subtraction in range", `let main = () -> u8 => {
	  let x: u8 = 50
	  let y: u8 = 8
	  x - y
	}`, 42},
	{"compound += in range", `let main = () -> u8 => {
	  var x: u8 = 40
	  x += 2
	  x
	}`, 42},
	{"boundary: u8 max, no overflow", `let main = () -> u8 => {
	  let x: u8 = 255
	  let y: u8 = 0
	  x + y
	}`, 255},
}

func TestExec_CheckedArithmetic_NoOverflow(t *testing.T) {
	t.Parallel()
	for _, c := range nonOverflowingCases {
		t.Run("", func(t *testing.T) {
			t.Parallel()
			if got := buildAndRun(t, c.src); got != c.want {
				t.Errorf("%s: exited %d; want %d", c.name, got, c.want)
			}
		})
	}
}

// TestEmit_CheckedArithmeticIR pins the lowering shape: `+`/`-`/`*` go through a
// with-overflow intrinsic and a trap, while `/` and `%` do not (division overflow
// / div-by-zero are a separate slice).
func TestEmit_CheckedArithmeticIR(t *testing.T) {
	t.Parallel()
	emit := func(src string) string {
		out, err := emitSource(t, src)
		if err != nil {
			t.Fatalf("emit: %v", err)
		}
		return out
	}

	// Operands via parameters (full-range → the value-range analysis can't prove
	// no-overflow), so the checked-op IR shape is actually emitted rather than
	// elided as it would be for provable constants (see TestEmit_OverflowElision).
	add := emit(`let add = (x: u8, y: u8) -> u8 => x + y
	let main = () -> u8 => add(1, 2)`)
	for _, needle := range []string{
		"llvm.uadd.with.overflow.i8",
		"call void @lyra_panic_overflow()",
		"unreachable",
	} {
		if !strings.Contains(add, needle) {
			t.Errorf("checked add IR missing %q", needle)
		}
	}
	// The trap reports to stderr (fd 2) and exits, once, shared across sites.
	if n := strings.Count(add, "define void @lyra_panic_overflow"); n != 1 {
		t.Errorf("want exactly one trap function definition, got %d", n)
	}

	// Signedness picks the s/u intrinsic (operands via parameters, as above).
	sadd := emit(`let f = (x: i16, y: i16) -> u8 => u8(x + y)
	let main = () -> u8 => f(1, 2)`)
	if !strings.Contains(sadd, "llvm.sadd.with.overflow.i16") {
		t.Error("signed add should use llvm.sadd.with.overflow")
	}

	// Division and remainder are not overflow-checked here.
	div := emit(`let main = () -> u8 => {
	  let a: u8 = 84
	  let b: u8 = 2
	  a / b
	}`)
	if strings.Contains(div, "with.overflow") {
		t.Error("division must not be overflow-checked in this slice")
	}
	if strings.Contains(div, "lyra_panic_overflow") {
		t.Error("division must not emit the overflow trap")
	}

	// A program with no arithmetic carries none of the trap machinery (lazy).
	none := emit(`let main = () -> u8 => 42`)
	if strings.Contains(none, "lyra_panic_overflow") || strings.Contains(none, "with.overflow") {
		t.Error("a non-arithmetic program should carry no overflow-trap machinery")
	}
}
