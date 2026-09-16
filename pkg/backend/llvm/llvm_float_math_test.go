package llvm

import (
	"strings"
	"testing"
)

// The trigonometric builtins, `exp`/`exp2`, and the two that take an argument — `pow` and
// `atan2` (09/15). The logarithms and `sqrt` they join are in llvm_log_test.go.
//
// **Builtins rather than Lyra, on the same grounds as the logarithms**: a sine is not
// expressible in this language — no series, no lookup table, and no FFI to reach libm. The
// gap was visible in the examples, four of which say in a comment that Lyra "has `sqrt` and
// the logarithms and no `sin`" and work around it with a square wave or a polynomial.
//
// The assertions are rounded to six places rather than digit-exact, since the last places
// of a transcendental come from the platform's libm.
func TestExec_TrigonometricBuiltins(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, body, want string }{
		{"sin at a quarter turn", `let a: f64 = 1.5707963267948966; println(a.sin().to_fixed(6));`, "1.000000"},
		{"sin at zero", `let a: f64 = 0.0; println(a.sin().to_fixed(6));`, "0.000000"},
		{"cos at a half turn", `let a: f64 = 3.141592653589793; println(a.cos().to_fixed(6));`, "-1.000000"},
		{"tan at an eighth turn", `let a: f64 = 0.7853981633974483; println(a.tan().to_fixed(6));`, "1.000000"},
		// The inverses answer in radians, and each undoes its own function.
		{"asin of one", `let v: f64 = 1.0; println(v.asin().to_fixed(6));`, "1.570796"},
		{"acos of zero", `let v: f64 = 0.0; println(v.acos().to_fixed(6));`, "1.570796"},
		{"atan of one", `let v: f64 = 1.0; println(v.atan().to_fixed(6));`, "0.785398"},
		{"asin undoes sin", `let a: f64 = 0.3; println(a.sin().asin().to_fixed(6));`, "0.300000"},
		{"exp of one is e", `let v: f64 = 1.0; println(v.exp().to_fixed(6));`, "2.718282"},
		{"exp of zero", `let v: f64 = 0.0; println(v.exp().to_fixed(6));`, "1.000000"},
		{"exp undoes log", `let v: f64 = 7.5; println(v.log().exp().to_fixed(4));`, "7.5000"},
		{"exp2 of ten", `let v: f64 = 10.0; println(v.exp2().to_fixed(1));`, "1024.0"},
		{"exp2 undoes log2", `let v: f64 = 40.0; println(v.log2().exp2().to_fixed(4));`, "40.0000"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			src := "let main = () -> void => {\n" + c.body + "\n}\n"
			if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != c.want {
				t.Errorf("got %q; want %q", got, c.want)
			}
		})
	}
}

// The two builtins that take an argument. Both operands and the result are the receiver's
// width, which is the integer overflow builtins' shape — the *arity* is what separates
// these from the unary family, and the backend reads it off the name (floatMathOp.binary).
//
// **`atan2` takes the x coordinate**, so `y.atan2(x)` is the angle of `(x, y)`: the
// receiver is the first argument of C's `atan2(y, x)`. It is the one of the pair that
// cannot be written as `(y / x).atan()` — the quotient loses the quadrant, which is what
// the two negative cases below pin, and divides by zero on the vertical axis.
func TestExec_BinaryFloatMathBuiltins(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, body, want string }{
		{"pow", `let b: f64 = 2.0; println(b.pow(10.0).to_fixed(1));`, "1024.0"},
		{"a fractional exponent is a root", `let b: f64 = 9.0; println(b.pow(0.5).to_fixed(4));`, "3.0000"},
		{"a negative exponent is a reciprocal", `let b: f64 = 4.0; println(b.pow(-1.0).to_fixed(4));`, "0.2500"},
		{"anything to the zero is one", `let b: f64 = 7.3; println(b.pow(0.0).to_fixed(1));`, "1.0"},
		{"the argument may be a variable", `let b: f64 = 3.0; let e: f64 = 4.0; println(b.pow(e).to_fixed(1));`, "81.0"},
		{"atan2 in the first quadrant", `let y: f64 = 1.0; println(y.atan2(1.0).to_fixed(6));`, "0.785398"},
		// The quadrant `(y / x).atan()` cannot see: (-1, -1) and (1, 1) have the same
		// quotient and opposite angles.
		{"atan2 in the third quadrant", `let y: f64 = -1.0; println(y.atan2(-1.0).to_fixed(6));`, "-2.356194"},
		{"atan2 on the vertical axis", `let y: f64 = 1.0; println(y.atan2(0.0).to_fixed(6));`, "1.570796"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			src := "let main = () -> void => {\n" + c.body + "\n}\n"
			if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != c.want {
				t.Errorf("got %q; want %q", got, c.want)
			}
		})
	}
}

// The result is the receiver's own width, as it is for the logarithms — and this is the
// test that pins the **f16 path in particular**, which is not the f64 one at a different
// width: `tan`, `asin`, `acos`, `atan` and `atan2` are emitted as direct libm calls rather
// than LLVM intrinsics (their intrinsics postdate the clang-15 the ASan container pins),
// and libm has no half-precision entry points at all. So an f16 receiver is widened to f32,
// computed, and rounded back (emitLibmMathCall) — a path with no counterpart in the
// intrinsic family, where LLVM does the widening itself.
func TestExec_FloatMathAnswersTheReceiversWidth(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, body, want string }{
		{"an f32 intrinsic receiver", `let v: f32 = 0.0; let r: f32 = v.sin(); println(f64(r).to_fixed(3));`, "0.000"},
		{"an f32 libm receiver", `let v: f32 = 1.0; let r: f32 = v.atan(); println(f64(r).to_fixed(3));`, "0.785"},
		{"an f32 binary receiver", `let v: f32 = 2.0; let r: f32 = v.pow(8.0); println(f64(r).to_fixed(1));`, "256.0"},
		{"an f16 intrinsic receiver", `let v: f16 = 0.0; let r: f16 = v.cos(); println(f64(r).to_fixed(1));`, "1.0"},
		{"an f16 libm receiver", `let v: f16 = 1.0; let r: f16 = v.atan(); println(f64(r).to_fixed(3));`, "0.785"},
		{"an f16 binary libm receiver", `let v: f16 = 1.0; let r: f16 = v.atan2(1.0); println(f64(r).to_fixed(3));`, "0.785"},
		{"a literal receiver", `println((0.0).cos().to_fixed(1));`, "1.0"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			src := "let main = () -> void => {\n" + c.body + "\n}\n"
			if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != c.want {
				t.Errorf("got %q; want %q", got, c.want)
			}
		})
	}
}

// **Outside its domain each answers IEEE's value rather than trapping**, which is the choice
// the logarithms and float division already make. `asin` and `acos` are defined only on
// [-1, 1] and give a NaN past it; `exp` overflows to infinity rather than saturating; and
// `pow` of a negative base to a fractional exponent is a NaN, since the real answer does
// not exist — which is exactly why `std.math`'s `cbrt` takes the sign out first.
//
// The trap comes later and in one place: feeding any of these to an integer conversion is
// what fails, which is where the guard belongs (guardFloatToInt).
func TestExec_FloatMathOutsideItsDomain(t *testing.T) {
	t.Parallel()
	src := `
let main = () -> void => {
  let past_one: f64 = 2.0;
  println("${past_one.asin()}");
  println("${past_one.acos()}");
  let big: f64 = 1000.0;
  println("${big.exp()}");
  let neg: f64 = -8.0;
  println("${neg.pow(0.3333333333333333)}");
}
`
	// `nan` unsigned on every platform: a printed NaN's sign is a property of the libm
	// that produced it, and the formatter normalizes it (see llvm_log_test.go).
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "nan\nnan\ninf\nnan" {
		t.Errorf("got %q; want \"nan\\nnan\\ninf\\nnan\"", got)
	}
}

// Every one of these is arithmetic over a scalar, so they are usable from the code that
// most wants them: an inner loop declared `pure noalloc`. Builtin methods are charged no
// effect by the three purity ladders, and this asserts the new names joined that set rather
// than falling to the unresolved-callee default — which the `std.math` helpers below depend
// on, since they are `pure noalloc` themselves and call these.
func TestCheck_FloatMathIsPureAndNoalloc(t *testing.T) {
	t.Parallel()
	src := `
let wave = pure noalloc (t: f64, freq: f64) -> f64 => (t * freq).sin() * 2.0.pow(-t) + t.atan2(1.0)
let main = () -> void => { let v: f64 = 0.0; println(wave(v, 1.0).to_fixed(2)); }
`
	// sin(0)*1 + atan2(0, 1) = 0.
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "0.00" {
		t.Errorf("got %q; want \"0.00\"", got)
	}
}

// `std.math`: the constants, and the functions the builtins above made writable.
//
// **Nothing here is a builtin or wraps one.** A sine is a compiler builtin because it is
// not expressible in this language; a hyperbolic sine is two `exp` calls and a division, so
// it is ordinary Lyra and lives in the standard library. What this pins is that side of the
// line — that `x.sinh()` is a `std.math` function reached by UFCS, not a name the compiler
// knows.
//
// The constants are `f64`: an unannotated binding takes the default width, so a
// single-precision caller writes `f32(PI)` (checked below, since that is the line a reader
// will most want to have been tested).
func TestExec_StdMathConstantsAndDerivedFunctions(t *testing.T) {
	t.Parallel()
	out := buildAndRunWithPrelude(t, `module main
import std.math.{ PI, TAU, HALF_PI, E, SQRT_2, PHI, to_radians, to_degrees, sinh, cosh, tanh, hypot, cbrt }
let main = () -> void => {
  println(HALF_PI.sin().to_fixed(6))
  println(TAU.to_fixed(6))
  println((E.log()).to_fixed(1))
  println((SQRT_2 * SQRT_2).to_fixed(6))
  println((PHI * PHI - PHI).to_fixed(6))
  println(180.0.to_radians().to_fixed(6))
  println(PI.to_degrees().to_fixed(1))
  println(0.0.sinh().to_fixed(6))
  println(0.0.cosh().to_fixed(6))
  println(1.0.sinh().to_fixed(6))
  println(3.0.hypot(4.0).to_fixed(1))
  println((-8.0).cbrt().to_fixed(6))
  println(27.0.cbrt().to_fixed(6))
  let single: f32 = f32(PI)
  println(f64(single).to_fixed(4))
}
`, "")
	want := strings.Join([]string{
		"1.000000",  // sin(pi/2)
		"6.283185",  // tau
		"1.0",       // ln(e)
		"2.000000",  // sqrt(2) squared
		"1.000000",  // phi is the root of x^2 = x + 1
		"3.141593",  // 180 degrees in radians
		"180.0",     // pi in degrees
		"0.000000",  // sinh(0)
		"1.000000",  // cosh(0)
		"1.175201",  // sinh(1) = (e - 1/e)/2
		"5.0",       // the 3-4-5 triangle
		"-2.000000", // the real cube root of a negative
		"3.000000",  // and of a positive
		"3.1416",    // the constant narrowed to f32
	}, "\n")
	if got := strings.TrimSpace(out); got != want {
		t.Errorf("std.math = %q; want %q", got, want)
	}
}

// `tanh` saturates past ±20 and `hypot` scales by the larger leg. Both are guards against
// an intermediate overflowing where the *answer* does not, which is the one class of bug a
// spot check at ordinary magnitudes cannot see: the naive `tanh` returns `inf/inf` — a NaN
// — exactly where the answer is least in doubt, and the naive `hypot` answers infinity for
// a triangle whose hypotenuse an f64 holds perfectly well.
func TestExec_StdMathAvoidsIntermediateOverflow(t *testing.T) {
	t.Parallel()
	out := buildAndRunWithPrelude(t, `module main
import std.math.{ tanh, hypot }
let main = () -> void => {
  println(1000.0.tanh().to_fixed(6))
  println((-1000.0).tanh().to_fixed(6))
  println(0.5.tanh().to_fixed(6))
  let huge: f64 = 3.0e200
  println((huge.hypot(4.0e200) / 1.0e200).to_fixed(1))
  println(0.0.hypot(0.0).to_fixed(1))
}
`, "")
	want := strings.Join([]string{
		"1.000000",  // saturated, not NaN
		"-1.000000", // and on the other side
		"0.462117",  // the formula still runs in between
		"5.0",       // a 3-4-5 triangle scaled past where x*x overflows
		"0.0",       // the degenerate case the scaling would have divided by
	}, "\n")
	if got := strings.TrimSpace(out); got != want {
		t.Errorf("std.math at the extremes = %q; want %q", got, want)
	}
}

// A `const` may call the float builtins (09/15), which is what lets a derived constant be
// written as its definition rather than as a digit string.
//
// **This is the end-to-end check that the value is right**, which the typechecker's
// constancy tests cannot give: a `const` is inlined as its value *expression* at every use
// site, so what a program actually holds is whatever that expression lowers to. The first
// two assertions are the ones `std/math/constants.lyra` now depends on — the derived
// `SQRT_2` and `PHI` must equal the literals they replaced, and they do, bit for bit.
func TestExec_ConstFloatBuiltinsComputeTheRightValue(t *testing.T) {
	t.Parallel()
	out := buildAndRunWithPrelude(t, `module main
const SQRT_2_LIT = 1.4142135623730951
const SQRT_2_DER = 2.0.sqrt()
const PHI_LIT = 1.618033988749895
const PHI_DER = (1 + 5.0.sqrt()) / 2
const KILO = 2.0.pow(10.0)
const QUARTER_TURN = 1.0.atan2(0.0)
const TRUNCATED = 2.7.floor()
const DERIVED_TWICE = PHI_DER * 2.0
let main = () -> void => {
  println("${SQRT_2_LIT == SQRT_2_DER}")
  println("${PHI_LIT == PHI_DER}")
  println(KILO.to_fixed(1))
  println(QUARTER_TURN.to_fixed(6))
  println("${TRUNCATED}")
  println(DERIVED_TWICE.to_fixed(6))
}
`, "")
	want := strings.Join([]string{
		"true",     // a derived sqrt is bit-identical to the literal it replaced
		"true",     // and so is the golden ratio built from one
		"1024.0",   // a binary builtin folds too
		"1.570796", // atan2 on the vertical axis
		"2",        // a rounding builtin answers i64
		"3.236068", // a const derived from a derived const
	}, "\n")
	if got := strings.TrimSpace(out); got != want {
		t.Errorf("const float builtins = %q; want %q", got, want)
	}
}
