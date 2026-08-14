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
