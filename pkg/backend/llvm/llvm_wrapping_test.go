package llvm

import (
	"strings"
	"testing"
)

// The explicit escape hatches from checked-by-default arithmetic: the integer
// wrapping/saturating builtin methods. Where plain `+`/`-`/`*` trap on overflow
// (llvm_checked_arith_test.go), these give the well-defined alternatives —
// modular wraparound or clamp-to-bound — and never trap.

var wrappingCases = []struct {
	name string
	src  string
	want int
}{
	// wrapping_* is modular two's-complement: the same result the old silent-wrap
	// arithmetic gave, now only when explicitly asked for.
	{"wrapping_add wraps mod 256", `let main = () -> u8 => {
	  let x: u8 = 200
	  let y: u8 = 100
	  x.wrapping_add(y)
	}`, 44}, // 300 mod 256
	{"wrapping_sub underflows mod 256", `let main = () -> u8 => {
	  let x: u8 = 10
	  let y: u8 = 20
	  x.wrapping_sub(y)
	}`, 246}, // -10 mod 256
	{"wrapping_mul wraps mod 256", `let main = () -> u8 => {
	  let x: u8 = 100
	  let y: u8 = 3
	  x.wrapping_mul(y)
	}`, 44}, // 300 mod 256

	// saturating_* clamps to the type's range.
	{"saturating_add clamps to u8 max", `let main = () -> u8 => {
	  let x: u8 = 200
	  let y: u8 = 100
	  x.saturating_add(y)
	}`, 255},
	{"saturating_sub clamps to 0 (unsigned)", `let main = () -> u8 => {
	  let x: u8 = 10
	  let y: u8 = 20
	  x.saturating_sub(y)
	}`, 0},
	{"saturating_add signed clamps to i8 min", `let main = () -> u8 => {
	  let x: i8 = -100
	  let y: i8 = -100
	  u8(x.saturating_add(y))
	}`, 128}, // -200 clamps to -128 → u8 exit 128
	{"saturating_mul unsigned clamps to max", `let main = () -> u8 => {
	  let x: u8 = 100
	  let y: u8 = 3
	  x.saturating_mul(y)
	}`, 255}, // 300 clamps
	{"saturating_mul signed clamps to min (mixed signs)", `let main = () -> u8 => {
	  let x: i8 = 100
	  let y: i8 = -2
	  u8(x.saturating_mul(y))
	}`, 128}, // -200 clamps to i8 min -128 → u8 exit 128
	{"saturating_mul signed clamps to max (same signs)", `let main = () -> u8 => {
	  let x: i8 = -100
	  let y: i8 = -2
	  u8(x.saturating_mul(y))
	}`, 127}, // +200 clamps to i8 max 127

	// No overflow: both are transparent, returning the true result.
	{"wrapping_add no overflow", `let main = () -> u8 => {
	  let x: u8 = 20
	  let y: u8 = 22
	  x.wrapping_add(y)
	}`, 42},
	{"saturating_add no overflow", `let main = () -> u8 => {
	  let x: u8 = 20
	  let y: u8 = 22
	  x.saturating_add(y)
	}`, 42},
}

func TestExec_WrappingSaturating(t *testing.T) {
	for _, c := range wrappingCases {
		if got := buildAndRun(t, c.src); got != c.want {
			t.Errorf("%s: exited %d; want %d", c.name, got, c.want)
		}
	}
}

// TestExec_WrappingIsNotChecked confirms the escape hatch actually escapes: an
// operation that would trap under checked `+` returns a wrapped/clamped value
// instead of exiting 101.
func TestExec_WrappingIsNotChecked(t *testing.T) {
	// Operands via a parameter so the overflow is opaque to lyra-E020 (this checks
	// the runtime trap, not the compile-time overflow error).
	trapping := `let add = (x: u8, y: u8) -> u8 => x + y
	let main = () -> u8 => add(200, 100)`
	if got := buildAndRun(t, trapping); got != overflowTrapExitCode {
		t.Fatalf("sanity: plain + should trap, got %d", got)
	}
	for _, src := range []string{
		`let main = () -> u8 => { let x: u8 = 200
		  let y: u8 = 100
		  x.wrapping_add(y) }`,
		`let main = () -> u8 => { let x: u8 = 200
		  let y: u8 = 100
		  x.saturating_add(y) }`,
	} {
		if got := buildAndRun(t, src); got == overflowTrapExitCode {
			t.Errorf("wrapping/saturating must not trap, but exited %d", overflowTrapExitCode)
		}
	}
}

// TestEmit_WrappingSaturatingIR pins the lowering: wrapping is a raw op (no
// with.overflow, no trap), saturating add/sub uses the .sat intrinsic.
func TestEmit_WrappingSaturatingIR(t *testing.T) {
	emit := func(src string) string {
		out, err := emitSource(t, src)
		if err != nil {
			t.Fatalf("emit: %v", err)
		}
		return out
	}

	wadd := emit(`let main = () -> u8 => {
	  let x: u8 = 1
	  let y: u8 = 2
	  x.wrapping_add(y)
	}`)
	if strings.Contains(wadd, "with.overflow") || strings.Contains(wadd, "lyra_panic_overflow") {
		t.Error("wrapping_add must not be overflow-checked")
	}

	sadd := emit(`let main = () -> u8 => {
	  let x: u8 = 1
	  let y: u8 = 2
	  x.saturating_add(y)
	}`)
	if !strings.Contains(sadd, "llvm.uadd.sat.i8") {
		t.Error("unsigned saturating_add should use llvm.uadd.sat")
	}

	ssub := emit(`let main = () -> u8 => {
	  let x: i16 = 1
	  let y: i16 = 2
	  u8(x.saturating_sub(y))
	}`)
	if !strings.Contains(ssub, "llvm.ssub.sat.i16") {
		t.Error("signed saturating_sub should use llvm.ssub.sat")
	}
}
