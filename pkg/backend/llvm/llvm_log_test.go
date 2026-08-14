package llvm

import (
	"strings"
	"testing"
)

// The unary float-math builtins — `log` (natural), `log2`, `log10`, `sqrt` (08/14).
//
// **Builtins rather than prelude Lyra, and that is the `random_seed` rule rather than the
// `parse_i64` one.** A logarithm is not expressible in this language: no series, no lookup
// table, and no FFI to reach libm. Parsing and formatting are arithmetic and live in the
// prelude; this cannot.
//
// All three ship together because smooth mandelbrot coloring is `n + 1 - log2(log(|z|))` —
// `log2` written as `x.log() / 2.0.log()` costs an extra call and loses accuracy at exactly
// the magnitudes shading depends on. The trio also makes the bare name's base unambiguous
// by contrast: `log` is the one with no subscript.
func TestExec_Logarithms(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, body, want string }{
		{"natural log of e", `let e: f64 = 2.718281828459045; println(e.log().to_fixed(4));`, "1.0000"},
		{"log of 1 is zero", `let o: f64 = 1.0; println(o.log().to_fixed(4));`, "0.0000"},
		{"log2 of 8", `let v: f64 = 8.0; println(v.log2().to_fixed(4));`, "3.0000"},
		{"log10 of 1000", `let v: f64 = 1000.0; println(v.log10().to_fixed(4));`, "3.0000"},
		// The result is the receiver's own width, not a fixed one — a log is a float
		// operation whose answer is a float, unlike the rounding builtins.
		{"an f32 receiver stays f32", `let v: f32 = 8.0; let r: f32 = v.log2(); println(f64(r).to_fixed(2));`, "3.00"},
		{"a literal receiver", `println((8.0).log2().to_fixed(1));`, "3.0"},
		{"sqrt", `let v: f64 = 2.0; println(v.sqrt().to_fixed(6));`, "1.414214"},
		{"sqrt of a perfect square", `let v: f64 = 9.0; println(v.sqrt().to_fixed(1));`, "3.0"},
		// The magnitude of a complex number, which is what sqrt was added for: the
		// escape-time renderer avoids it via log(|z|^2)/2, and a distance estimator
		// cannot.
		{"a complex magnitude", `let m2: f64 = 3.0 * 3.0 + 4.0 * 4.0; println(m2.sqrt().to_fixed(1));`, "5.0"},
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

// **Outside the domain it answers IEEE's value rather than trapping**, which is the same
// choice float division already makes: `1.0 / 0.0` is an infinity, not a fault. The trap
// comes later and in one place — feeding either of these to an integer conversion is what
// fails, which is where the guard belongs (guardFloatToInt).
func TestExec_LogarithmOutsideItsDomain(t *testing.T) {
	t.Parallel()
	src := `
let main = () -> void => {
  let z: f64 = 0.0;
  println("${z.log()}");
  let neg: f64 = -1.0;
  println("${neg.log()}");
  println("${neg.sqrt()}");
}
`
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "-inf\nnan\nnan" {
		t.Errorf("got %q; want \"-inf\\nnan\\nnan\"", got)
	}
}

// Smooth coloring end to end — the reason these exist. `log|z|` is computed as
// `log(|z|^2)/2`, which avoids a square root the language does not have.
//
// The assertion is the *shape* of the answer rather than a digit-exact constant: a point
// outside the set gets a fractional escape value strictly between its integer iteration
// count and the next, which is precisely what smooth coloring buys over the bare count.
func TestExec_SmoothEscapeValue(t *testing.T) {
	t.Parallel()
	src := `
let escape = pure (cre: f64, cim: f64, max_iter: i64) -> f64 => {
  var zre = 0.0
  var zim = 0.0
  var n = 0
  var mag2 = 0.0
  for n < max_iter {
    let next_re = zre * zre - zim * zim + cre
    zim = 2.0 * zre * zim + cim
    zre = next_re
    mag2 = zre * zre + zim * zim
    if mag2 > 4.0 { break }
    n += 1
  }
  if n >= max_iter { f64(max_iter) } else { f64(n) + 1.0 - (mag2.log() / 2.0).log2() }
}
let main = () -> void => {
  println(escape(-0.5, 0.5, 60).to_fixed(1));
  println(escape(1.0, 1.0, 60).to_fixed(4));
}
`
	out := strings.Split(strings.TrimSpace(buildAndRunWithPrelude(t, src, "")), "\n")
	if len(out) != 2 {
		t.Fatalf("want two lines, got %q", out)
	}
	// Inside the set: the loop runs to max_iter and the count is returned whole.
	if out[0] != "60.0" {
		t.Errorf("a point inside the set should reach max_iter, got %q", out[0])
	}
	// Outside: `break` fires before the counter advances, so the bare count is 1 — and
	// the smooth value lands strictly inside [1, 2). That gap *is* the feature: the
	// integer count is what produces visible banding, and the fraction is what removes
	// it. Asserted as a range rather than digit-exact, since the last places come from
	// the platform's libm.
	if !strings.HasPrefix(out[1], "1.") || out[1] == "1.0000" {
		t.Errorf("want a fractional escape value strictly inside [1,2), got %q", out[1])
	}
}

// A logarithm is arithmetic over a scalar, so it is usable from the code that most wants
// it: an inner loop declared `pure noalloc`. Builtin methods are charged no effect by the
// three purity ladders, and this asserts the log family joined that set rather than
// falling to the unresolved-callee default.
func TestCheck_LogarithmIsPureAndNoalloc(t *testing.T) {
	t.Parallel()
	src := `
let shade = pure noalloc (mag2: f64) -> f64 => (mag2.log() / 2.0).log2()
let main = () -> void => { let v: f64 = 16.0; println(shade(v).to_fixed(2)); }
`
	// log(16)/2 is log|z| for |z|^2 = 16, i.e. log(4) = 1.3863; log2 of that is 0.4712.
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "0.47" {
		t.Errorf("got %q; want \"0.47\"", got)
	}
}
