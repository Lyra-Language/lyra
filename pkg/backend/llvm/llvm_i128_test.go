package llvm

import (
	"os"
	"os/exec"
	"path/filepath"
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

// TestEmit_I128MulOverflowSurvivesO2 guards the helper against the optimiser rather
// than against the emitter. TestEmit_I128_IR checks the *emitted* IR for the intrinsic,
// but `lyrac build` hands that IR to clang at -O2, and the helper's first body — the
// `(a*b)/a != b` idiom — was one InstCombine recognises: it folded the body straight
// back into `llvm.smul.with.overflow.i128`, which on aarch64 Linux lowers to a call to
// `__muloti4`, and every std.temporal example failed to link in the arm64 clang-15
// container. The backend tests compile at clang's -O0 default, so nothing here saw it.
//
// So this optimises the module the way lyrac does and checks two things: the -O2 IR
// has no signed 128-bit multiply-with-overflow (host-independent, and what the fold
// produces on every target), and an aarch64-linux object built at -O2 names no
// `__muloti4` (the link failure itself; skipped when this clang has no AArch64 target).
func TestEmit_I128MulOverflowSurvivesO2(t *testing.T) {
	t.Parallel()
	clang := lookClang(t)
	src := `let mul = (a: i128, b: i128) -> i128 => a * b
let main = () -> void => {
  println(mul(3, 4))
}
`
	ll, err := emitSource(t, src)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	in := filepath.Join(dir, "in.ll")
	if err := os.WriteFile(in, []byte(ll), 0o644); err != nil {
		t.Fatal(err)
	}

	optimised, err := exec.Command(clang, "-O2", "-S", "-emit-llvm", "-Wno-override-module", in, "-o", "-").CombinedOutput()
	if err != nil {
		t.Fatalf("clang -O2 -emit-llvm: %v\n%s", err, optimised)
	}
	if !strings.Contains(string(optimised), "lyra_i128_mul_overflow") {
		t.Fatalf("the helper did not survive to the optimised IR, so this test checks nothing:\n%s", optimised)
	}
	if strings.Contains(string(optimised), "llvm.smul.with.overflow.i128") {
		t.Errorf("-O2 folded the i128 checked multiply back into llvm.smul.with.overflow.i128, "+
			"which lowers to compiler-rt's __muloti4 and does not link against libgcc:\n%s", optimised)
	}

	obj := filepath.Join(dir, "out.o")
	if out, err := exec.Command(clang, "--target=aarch64-linux-gnu", "-O2", "-c", "-Wno-override-module", in, "-o", obj).CombinedOutput(); err != nil {
		t.Logf("skipping the aarch64-linux object check (no AArch64 target in this clang?): %v\n%s", err, out)
		return
	}
	bytes, err := os.ReadFile(obj)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(bytes), "__muloti4") {
		t.Errorf("the aarch64-linux object built at -O2 references __muloti4, which libgcc does not provide")
	}
}
