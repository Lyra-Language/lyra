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
	{"u8 add wraps past 255", `let main = () -> u8 => {
	  let x: u8 = 200
	  let y: u8 = 100
	  x + y
	}`},
	{"u8 sub below 0", `let main = () -> u8 => {
	  let x: u8 = 10
	  let y: u8 = 20
	  x - y
	}`},
	{"i8 mul past 127", `let main = () -> u8 => {
	  let x: i8 = 100
	  let y: i8 = 2
	  u8(x * y)
	}`},
	{"i32 signed mul overflow", `let main = () -> u8 => {
	  let x: i32 = 100000
	  let y: i32 = 100000
	  u8(x * y)
	}`},
	{"i64 add overflow", `let main = () -> u8 => {
	  let x: i64 = 9223372036854775807
	  let y: i64 = 1
	  u8(x + y)
	}`},
	{"compound += overflow", `let main = () -> u8 => {
	  var x: u8 = 250
	  x += 10
	  x
	}`},
}

// TestExec_CheckedArithmetic_Traps runs each overflowing program and asserts it
// traps (exit 101) rather than wrapping to a value.
func TestExec_CheckedArithmetic_Traps(t *testing.T) {
	for _, c := range overflowingCases {
		if got := buildAndRun(t, c.src); got != overflowTrapExitCode {
			t.Errorf("%s: exited %d; want %d (overflow trap)", c.name, got, overflowTrapExitCode)
		}
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
	for _, c := range nonOverflowingCases {
		if got := buildAndRun(t, c.src); got != c.want {
			t.Errorf("%s: exited %d; want %d", c.name, got, c.want)
		}
	}
}

// TestEmit_CheckedArithmeticIR pins the lowering shape: `+`/`-`/`*` go through a
// with-overflow intrinsic and a trap, while `/` and `%` do not (division overflow
// / div-by-zero are a separate slice).
func TestEmit_CheckedArithmeticIR(t *testing.T) {
	emit := func(src string) string {
		out, err := emitSource(t, src)
		if err != nil {
			t.Fatalf("emit: %v", err)
		}
		return out
	}

	add := emit(`let main = () -> u8 => {
	  let x: u8 = 1
	  let y: u8 = 2
	  x + y
	}`)
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

	// Signedness picks the s/u intrinsic.
	sadd := emit(`let main = () -> u8 => {
	  let x: i16 = 1
	  let y: i16 = 2
	  u8(x + y)
	}`)
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
