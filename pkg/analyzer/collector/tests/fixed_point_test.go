package collector_test

import (
	"strings"
	"testing"

	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
)

// ── fixed<I, F> formats (lyra-E055) ─────────────────────────────────────────
//
// The syntax takes any two integers and a format exists only where they add up to a width
// with an integer to hold it, so a format that can't exist is the collector's to refuse: it
// is the one place the syntax becomes a type.

func fixedPointRefusal(t *testing.T, source string, wantCount int) {
	t.Helper()
	diags := diagnosticsOf(t, source)
	got := 0
	for _, d := range diags {
		if d.Code == diag.CodeFixedPointFormat {
			got++
			if !strings.Contains(d.Message, "is not a format") {
				t.Errorf("E055 message should say what is wrong: %q", d.Message)
			}
		}
	}
	if got != wantCount {
		t.Errorf("got %d lyra-E055 diagnostics, want %d; all: %v", got, wantCount, diags)
	}
}

func TestFixedPoint_AnnotationIsRefused(t *testing.T) {
	fixedPointRefusal(t, `
let main = () => {
  let x: fixed<16, 17> = 1
}
`, 1)
}

// **Exactly one diagnostic per mention.** The collector returns nil rather than the type,
// so the annotation reads as *absent* downstream and the binding infers `i64` — without
// that, this answers one mistake with two errors, the second being the assignability
// failure that is only a consequence of the first.
func TestFixedPoint_ReportsOncePerMention(t *testing.T) {
	diags := diagnosticsOf(t, `
let main = () => {
  let x: fixed<16, 17> = 1
}
`)
	if len(diags) != 1 {
		t.Errorf("want exactly one diagnostic, got %d: %v", len(diags), diags)
	}
}

// Every position that can hold a type, because the refusal returns nil and a nil type is
// the shape that crashes a collector which was not expecting one (hazard 3's neighbour).
// The array case is the one that had a second, compiler-internal error underneath it
// ("parseArrayType: element type is nil") until parseArrayType was made to trust that
// parseType has already reported.
func TestFixedPoint_RefusedInEveryTypePosition(t *testing.T) {
	for name, source := range map[string]string{
		"struct field":  `struct Point { x: fixed<16, 17> }`,
		"newtype base":  `type Scaled = fixed<8, 9>`,
		"parameter":     `let take = (v: fixed<16, 17>) -> i64 => 1`,
		"return type":   `let give = () -> fixed<16, 17> => 1`,
		"array element": `let arr: []fixed<16, 17> = []`,
		"type alias":    `alias Alias = fixed<4, 3>`,
	} {
		t.Run(name, func(t *testing.T) {
			fixedPointRefusal(t, source, 1)
		})
	}
}

// Every width and split that can exist is collected, at both ends of what a split allows.
func TestFixedPoint_AFormatOfAWidthLyraHasIsCollected(t *testing.T) {
	for _, source := range []string{
		`let a: fixed<4, 4> = 1`, `let a: fixed<1, 7> = 0`, `let a: fixed<7, 1> = 1`,
		`let a: fixed<8, 8> = 1`, `let a: fixed<24, 8> = 1`, `let a: fixed<16, 16> = 1`,
		`let a: fixed<32, 32> = 1`, `let a: fixed<1, 63> = 0`, `let a: fixed<63, 1> = 1`,
	} {
		fixedPointRefusal(t, source, 0)
	}
}

func TestFixedPoint_AFormatThatCantExistIsRefused(t *testing.T) {
	for _, source := range []string{
		`let a: fixed<0, 8> = 1`, `let a: fixed<8, 0> = 1`, `let a: fixed<3, 8> = 1`,
		`let a: fixed<64, 64> = 1`, `let a: fixed<8, 4> = 1`,
	} {
		fixedPointRefusal(t, source, 1)
	}
}
