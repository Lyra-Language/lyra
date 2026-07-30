package llvm

import (
	"strings"
	"testing"
)

// 128-bit integers lower end to end: arithmetic runs at i128 width (so a product
// that overflows i64 is computed correctly, not truncated), the checked-overflow
// intrinsics extend to .i128, division goes through compiler-rt (linked by clang),
// and print uses the hand-written base-10 formatter (there is no printf modifier
// for 128 bits). See todo.md's i128/u128 change set.

// TestExec_I128_ExitCode: i128 arithmetic feeds the u8 exit code through an
// explicit narrowing conversion (i128 → u8), proving both the arithmetic and the
// down-conversion lower.
func TestExec_I128_ExitCode(t *testing.T) {
	t.Parallel()
	src := `let main = () -> u8 => {
  let a: i128 = 200
  let b: i128 = 55
  u8(a + b)
}
`
	if got := buildAndRun(t, src); got != 255 {
		t.Errorf("expected exit 255, got %d", got)
	}
}

// TestExec_I128_Print exercises the formatter across the cases that distinguish a
// real 128-bit path from an i64 one: a product that exceeds i64/u64, a large
// negative value, zero, and a value near u64 max.
func TestExec_I128_Print(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
		want string
	}{
		{
			// 1e12 squared = 1e24, far past i64 max (~9.2e18): correct only at i128.
			"i128 big multiply",
			`let main = () -> void => {
  let a: i128 = 1000000000000
  println(a * a)
}`,
			"1000000000000000000000000\n",
		},
		{
			"i128 large negative",
			`let main = () -> void => {
  let a: i128 = 1000000000000
  let sq: i128 = a * a
  println(-sq)
}`,
			"-1000000000000000000000000\n",
		},
		{
			// 1e10 squared = 1e20, past u64 max (~1.8e19).
			"u128 big multiply",
			`let main = () -> void => {
  let a: u128 = 10000000000
  println(a * a)
}`,
			"100000000000000000000\n",
		},
		{
			"i128 zero",
			`let main = () -> void => {
  let a: i128 = 0
  println(a)
}`,
			"0\n",
		},
		{
			// A large-unsigned literal (> i64 max) reaches u128 via conversion.
			"u128 near u64 max",
			`let main = () -> void => {
  let a: u128 = u128(18446744073709551615)
  println(a)
}`,
			"18446744073709551615\n",
		},
		{
			"i128 division via compiler-rt",
			`let main = () -> void => {
  let a: i128 = 1000000000000
  let sq: i128 = a * a
  println(sq / 1000000)
}`,
			"1000000000000000000\n",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			out, code := buildAndRunCapture(t, c.src)
			if code != 0 {
				t.Fatalf("expected exit 0, got %d (out=%q)", code, out)
			}
			if out != c.want {
				t.Errorf("expected %q, got %q", c.want, out)
			}
		})
	}
}

// TestEmit_I128_IR pins the lowering shape: an i128 type, the checked multiply, and
// the formatter (which divides by 10 at i128 width).
//
// The signed 128-bit multiply deliberately does **not** go through
// `llvm.smul.with.overflow.i128`: LLVM expands that into a call to compiler-rt's
// `__muloti4`, which Linux clang does not link (it defaults to libgcc, which has no
// such symbol), so an i128 multiply failed to link there while the same IR was fine on
// macOS. It calls the emitted `lyra_i128_mul_overflow` instead, keeping `clang out.ll`
// self-contained on every platform. The intrinsic's *absence* is asserted too, since
// its return would silently reintroduce the platform split.
func TestEmit_I128_IR(t *testing.T) {
	t.Parallel()
	src := `let main = () -> void => {
  let a: i128 = 5
  println(a * a)
}
`
	got, err := emitSource(t, src)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"i128",
		"lyra_i128_mul_overflow",
		"lyra_i128_to_str",
		"udiv i128",
		"urem i128",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("emitted IR missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "llvm.smul.with.overflow.i128") {
		t.Errorf("the signed 128-bit multiply is back on the intrinsic, which expands to "+
			"compiler-rt's __muloti4 and does not link on Linux:\n%s", got)
	}
}
