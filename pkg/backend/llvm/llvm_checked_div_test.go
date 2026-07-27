package llvm

import (
	"strings"
	"testing"
)

// Checked division and negation (Pit-of-Success #2, the second slice after
// +/-/*): the runtime-error cases LLVM's div/rem and negate leave as undefined
// behavior are trapped instead — divide-by-zero, signed division overflow
// (INT_MIN / -1), and negation overflow (-INT_MIN) — all exiting with the trap
// code (101). INT_MIN cases use i64-min (-9223372036854775808); the narrow
// signed-min literals (i8 -128, etc.) now lower at their own width too — see
// llvm_narrow_min_test.go for that coverage.
//
// The zero divisor comes through a function parameter, not a `let b = 0` binding:
// a constant-zero divisor is now a compile-time error (lyra-E021, the range
// analysis's definite-divide-by-zero diagnostic), so to still exercise the
// *runtime* trap the divisor must be runtime-opaque (the analysis widens a param
// to its full range, keeping the check).

var checkedDivCases = []struct {
	name string
	src  string
}{
	{"divide by zero (/)", `
		let d = (a: u8, b: u8) -> u8 => a / b
		let main = () -> u8 => d(84, 0)
	`},
	{"divide by zero (%)", `
		let d = (a: u8, b: u8) -> u8 => a % b
		let main = () -> u8 => d(84, 0)
	`},
	{"divide by zero (%%)", `
		let d = (a: u8, b: u8) -> u8 => a %% b
		let main = () -> u8 => d(84, 0)
	`},
	{"signed divide by zero", `
		let d = (a: i32, b: i32) -> i32 => a / b
		let main = () -> u8 => u8(d(84, 0))
	`},
	{"INT_MIN / -1 overflow", `let main = () -> u8 => {
	  let a: i64 = -9223372036854775808
	  let b: i64 = -1
	  u8(a / b)
	}`},
	{"INT_MIN % -1 overflow", `let main = () -> u8 => {
	  let a: i64 = -9223372036854775808
	  let b: i64 = -1
	  u8(a % b)
	}`},
	{"negate INT_MIN overflow", `let main = () -> u8 => {
	  let x: i64 = -9223372036854775808
	  u8(-x)
	}`},
}

func TestExec_CheckedDivision_Traps(t *testing.T) {
	for _, c := range checkedDivCases {
		if got := buildAndRun(t, c.src); got != trapExitCode {
			t.Errorf("%s: exited %d; want %d (trap)", c.name, got, trapExitCode)
		}
	}
}

// Non-trapping division and negation stay transparent.
var checkedDivOkCases = []struct {
	name string
	src  string
	want int
}{
	{"unsigned division", `let main = () -> u8 => {
	  let a: u8 = 84
	  let b: u8 = 2
	  a / b
	}`, 42},
	{"signed division (both negative)", `let main = () -> u8 => {
	  let a: i8 = -84
	  let b: i8 = -2
	  u8(a / b)
	}`, 42},
	{"modulo", `let main = () -> u8 => {
	  let a: u8 = 44
	  let b: u8 = 100
	  a % b
	}`, 44},
	{"negation of a non-min value", `let main = () -> u8 => {
	  let x: i8 = -42
	  u8(-x)
	}`, 42},
	{"INT_MIN / 1 does not overflow", `let main = () -> u8 => {
	  let a: i64 = -9223372036854775808
	  let b: i64 = 1
	  u8(a / b)
	}`, 0}, // = INT_MIN, whose low 8 bits are 0
}

func TestExec_CheckedDivision_NoTrap(t *testing.T) {
	for _, c := range checkedDivOkCases {
		if got := buildAndRun(t, c.src); got != c.want {
			t.Errorf("%s: exited %d; want %d", c.name, got, c.want)
		}
	}
}

// TestEmit_CheckedDivisionIR pins the checks: `/` guards the divisor against zero
// (both signednesses) and, when signed, against the INT_MIN/-1 overflow; unsigned
// `/` gets only the zero check; negation guards against INT_MIN. Operands come
// through parameters so the value-range analysis can't prove the divisor nonzero
// (a constant divisor is elided — see TestEmit_DivisionElision); the params keep
// the divisor at its full type range, so the checks stay.
func TestEmit_CheckedDivisionIR(t *testing.T) {
	emit := func(src string) string {
		out, err := emitSource(t, src)
		if err != nil {
			t.Fatalf("emit: %v", err)
		}
		return out
	}

	udiv := emit(`
		let d = (a: u8, b: u8) -> u8 => a / b
		let main = () -> u8 => d(1, 2)
	`)
	if !strings.Contains(udiv, "@lyra_panic_divide_by_zero") {
		t.Error("unsigned division should guard against divide-by-zero")
	}
	if strings.Contains(udiv, "@lyra_panic_overflow") {
		t.Error("unsigned division cannot overflow — no overflow trap expected")
	}

	sdiv := emit(`
		let d = (a: i32, b: i32) -> i32 => a / b
		let main = () -> u8 => u8(d(1, 2))
	`)
	if !strings.Contains(sdiv, "@lyra_panic_divide_by_zero") || !strings.Contains(sdiv, "@lyra_panic_overflow") {
		t.Error("signed division should guard both divide-by-zero and INT_MIN/-1 overflow")
	}

	neg := emit(`let main = () -> u8 => {
	  let x: i8 = -42
	  u8(-x)
	}`)
	if !strings.Contains(neg, "@lyra_panic_overflow") {
		t.Error("negation should guard against -INT_MIN overflow")
	}

	// The divide-by-zero trap function is defined once, shared across sites.
	twoDivs := emit(`
		let d = (a: u8, b: u8, c: u8) -> u8 => (a / b) + (c / b)
		let main = () -> u8 => d(8, 2, 4)
	`)
	if n := strings.Count(twoDivs, "define void @lyra_panic_divide_by_zero"); n != 1 {
		t.Errorf("want one divide-by-zero trap definition, got %d", n)
	}
}
