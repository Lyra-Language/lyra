package llvm

import (
	"os/exec"
	"strings"
	"testing"
)

// runPreludeCombined runs src against the real prelude and returns its combined output and
// exit status — what a *trap* test needs, since a panic writes to stderr and exits 101
// while the stdout-only runner sees an empty string either way.
func runPreludeCombined(t *testing.T, src string) (string, int) {
	t.Helper()
	out, err := exec.Command(preludeBinary(t, src)).CombinedOutput()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("running the binary failed: %v", err)
	}
	return string(out), code
}

// `to_fixed` — a float rendered with a chosen number of decimal places
// (`std/prelude/format.lyra`, 08/14).
//
// The built-in formatter writes the shortest rendering that reads back as the same value,
// which is right for inspecting a value and wrong for a column of them: `1.0 / 3.0` needs
// seventeen digits to round-trip, so it prints them. A status line wants `0.3333`.
//
// Written in Lyra rather than as a builtin, on the `parse_i64` rule, so these are the tests
// that say the *arithmetic* is right — carrying, padding, the sign, and the range guard.
func TestExec_ToFixed(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, expr, want string }{
		{"repeating", "(1.0 / 3.0).to_fixed(4)", "0.3333"},
		{"negative", "v.to_fixed(4)", "-0.7436"},
		{"pads trailing zeros", "h.to_fixed(3)", "0.500"},
		{"carries to a whole unit", "c.to_fixed(2)", "1.00"},
		{"carries across a digit", "n.to_fixed(2)", "10.00"},
		{"whole number", "w.to_fixed(2)", "42.00"},
		{"no decimal point at zero places", "w.to_fixed(0)", "42"},
		// The default rendering switches to scientific notation here (`1.234e-06`),
		// which is exactly what makes it unusable for a fixed column.
		{"tiny value is not scientific", "t.to_fixed(4)", "0.0000"},
		{"large value keeps its whole part", "b.to_fixed(2)", "1234567.89"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			src := `
let main = () -> void => {
  let v: f64 = -0.7436;
  let h: f64 = 0.5;
  let c: f64 = 0.999;
  let n: f64 = 9.9999;
  let w: f64 = 42.0;
  let t: f64 = 0.000001234;
  let b: f64 = 1234567.891;
  println(` + c.expr + `);
}
`
			if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != c.want {
				t.Errorf("%s = %q; want %q", c.expr, got, c.want)
			}
		})
	}
}

// **The range guard is the part that cannot be left to the conversion.** Converting an
// out-of-range float to an integer does not check — `(1.0e20).floor()` answers 0, and so
// does a NaN — so an unguarded formatter prints a confident wrong number rather than
// failing. Before the guard, `1.0e20.to_fixed(2)` rendered as
// `9223372036854775807.9223372036854775807`.
//
// This asserts the trap, and stays correct if the underlying conversion is ever made to
// trap on its own: either way the program does not print a wrong answer.
func TestExec_ToFixedRefusesAnUnrenderableValue(t *testing.T) {
	t.Parallel()
	src := `
let main = () -> void => {
  let huge: f64 = 1.0e20;
  println(huge.to_fixed(2));
}
`
	out, code := runPreludeCombined(t, src)
	if strings.Contains(out, "9223372036854775807") {
		t.Fatalf("printed a saturated integer instead of trapping: %q", out)
	}
	if code == 0 || !strings.Contains(out, "too large") {
		t.Errorf("want the to_fixed range trap, got exit %d and %q", code, out)
	}
}

// A negative `places` is a caller's mistake, not a value to render — the reasoning `slice`
// applies to an inverted range.
func TestExec_ToFixedRefusesNegativePlaces(t *testing.T) {
	t.Parallel()
	src := `
let main = () -> void => {
  let x: f64 = 1.5;
  println(x.to_fixed(-1));
}
`
	out, code := runPreludeCombined(t, src)
	if code == 0 || !strings.Contains(out, "must not be negative") {
		t.Errorf("want the negative-places trap, got exit %d and %q", code, out)
	}
}

// **A method resolves on a bare literal receiver**, through all three paths and not just
// the builtin one.
//
// `builtinMethodSignature` promotes an untyped literal internally, so `1.5.floor()` has
// worked since literals became postfix heads (08/06). Trait dispatch and UFCS run first
// and saw `untyped_float`, which matches no impl and no `self: f64` parameter — so every
// prelude function over a float was unreachable from the literal a reader would naturally
// try it on, reporting *"float literal has no method"* while the identical call on a
// `let`-bound `f64` worked. The receiver is now pinned to its default width before any of
// the three run.
func TestExec_MethodsResolveOnALiteralReceiver(t *testing.T) {
	t.Parallel()
	src := `
let main = () -> void => {
  println((1.0 / 3.0).to_fixed(4));   // UFCS free function
  println((2.5).abs());               // trait impl
  println((0.0 - 2.5).is_negative()); // trait impl, bool result
  println(1.5.floor());               // builtin, unchanged
  println(1.wrapping_add(2));         // builtin on an integer literal, unchanged
}
`
	want := "0.3333\n2.5\ntrue\n1\n3"
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != want {
		t.Errorf("got %q; want %q", got, want)
	}
}
