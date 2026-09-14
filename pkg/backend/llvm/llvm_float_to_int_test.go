package llvm

import (
	"strings"
	"testing"
)

// `floor`/`ceil`/`round` trap when the result is not an integer they can produce
// (08/14). Before this, `fptosi` was emitted bare — and it is **poison** out of range in
// LLVM, not a saturating conversion — so `(1.0e20).floor()` answered 0, `-1.0e20`
// answered 0, and the value was whatever the optimizer left behind.
//
// It was the one gap in this language's numeric ladder: integer overflow traps, an index
// out of bounds traps, an out-of-range shift traps, a violated newtype constraint traps.
// And it was the quietest kind of gap, because a plausible wrong number reads as
// arithmetic rather than as a fault — `to_fixed`'s first draft rendered `1.0e20` as
// `9223372036854775807.9223372036854775807` and looked entirely credible doing it.
func TestExec_FloatToIntTrapsOutOfRange(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, body string }{
		{"far above the range", `let v: f64 = 1.0e20; println(v.floor());`},
		{"far below the range", `let v: f64 = -1.0e20; println(v.floor());`},
		// 2^63 is the first value past i64's maximum, and the reason the upper bound is
		// exclusive: i64's max (2^63-1) is not representable in binary64, so an
		// inclusive check against it would compare against 2^63 and admit this.
		{"exactly 2^63", `let v: f64 = 9223372036854775808.0; println(v.floor());`},
		{"a NaN", `let z: f64 = 0.0; let n = z / z; println(n.round());`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			out, code := runPreludeCombined(t, "let main = () -> void => {\n"+c.body+"\n}\n")
			if code != 101 {
				t.Errorf("want the trap (exit 101), got exit %d and %q", code, out)
			}
			if !strings.Contains(out, "out of range for an integer") {
				t.Errorf("want the float-to-int trap message, got %q", out)
			}
		})
	}
}

// The guard must not narrow what already worked, and the boundary is where an
// off-by-one would hide: i64's minimum is exactly -2^63 and *is* representable, so it
// converts rather than trapping.
func TestExec_FloatToIntAcceptsWhatFits(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, body, want string }{
		{"the minimum, which is exact", `let v: f64 = -9223372036854775808.0; println(v.floor());`, "-9223372036854775808"},
		{"a large exact value", `let v: f64 = 9007199254740992.0; println(v.floor());`, "9007199254740992"},
		{"ordinary rounding", `let v: f64 = 3.7; println(v.floor()); println(v.ceil()); println(v.round());`, "3\n4\n4"},
		{"negative rounding", `let v: f64 = -3.7; println(v.floor()); println(v.round());`, "-4\n-4"},
		{"an f32 receiver", `let v: f32 = 3.7; println(v.floor());`, "3"},
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

// **A conversion the value-range pass proves in range has no guard** (09/13), and computes
// the same answer the guarded one did — negative values, `round`'s half-away-from-zero, an
// f32, and a float binding included. The IR assertion is what shows the guard is gone; the
// output is what shows nothing else went with it.
func TestExec_ProvablyInRangeRoundingHasNoGuard(t *testing.T) {
	t.Parallel()
	src := `let scaled = (i: u8) -> i64 => (f64(i) / 10.0 - 12.0).floor()
let halves = (i: i16) -> i64 => (f64(i) * 0.5).round()
let narrow = (i: u16) -> i64 => { let u: f32 = f32(f64(i) * 0.25)
  u.ceil() }
let main = () -> void => {
  println("${scaled(0)} ${scaled(125)} ${scaled(255)}")
  println("${halves(-3)} ${halves(3)} ${halves(-32768)}")
  println("${narrow(1)} ${narrow(65535)}")
}
`
	ir := emitWithPrelude(t, src)
	for _, fn := range []string{"scaled", "halves", "narrow"} {
		start := strings.Index(ir, "@lyra."+fn+"(")
		if start < 0 {
			t.Fatalf("no definition of %s in the IR", fn)
		}
		body := ir[start:]
		body = body[:strings.Index(body, "\n}\n")]
		if strings.Contains(body, "lyra_panic_float_to_int") {
			t.Errorf("%s: a provably in-range conversion still carries its guard", fn)
		}
	}
	want := "-12 0 13\n-2 2 -16384\n1 16384"
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != want {
		t.Errorf("got %q; want %q", got, want)
	}
}

// And a value the pass cannot bound keeps it: a match arm's payload shadowing a bounded
// outer binding of the same name must trap, not convert poison.
func TestExec_ShadowedFloatStillTraps(t *testing.T) {
	t.Parallel()
	out, code := runPreludeCombined(t, `let main = () -> void => {
  let x = 2.0
  let m: Maybe<f64> = Some(1.0e300)
  match m { Some(x) => println(x.floor()), None => println(x.floor()) }
}
`)
	if code != 101 || !strings.Contains(out, "out of range for an integer") {
		t.Errorf("want the float-to-int trap, got exit %d and %q", code, out)
	}
}
