package collector_test

import (
	"strings"
	"testing"

	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
)

// ── fixed<I, F>: refused as unimplemented (lyra-E055, 08/14) ────────────────
//
// The annotation parses and collects into a real `types.FixedPointType`, and no pass
// after the collector knows what one is — so the type is *uninhabitable*: every literal
// and every conversion into it is refused, and no value of it can be constructed by any
// spelling.
//
// Before this, an author met that as a series of ordinary type errors — "cannot assign
// integer literal to `fixed<16,16>`" — which reads as a fixable mistake and invites
// trying `1.5`, then `f64(1.5)`, then `i32(1)`, each answered by the same sentence with
// one noun changed. The lyra-E035/E052 rule applies: when a construct is unimplemented,
// say so, rather than leaving it to be inferred from what fails.
//
// The syntax is kept rather than deleted because the intent is to build it, and
// `fixed<I, F>` already commits to the design that serves determinism (binary scaling).

func fixedPointRefusal(t *testing.T, source string, wantCount int) {
	t.Helper()
	diags := diagnosticsOf(t, source)

	got := 0
	for _, d := range diags {
		if d.Code == diag.CodeFixedPointNotImplemented {
			got++
			if !strings.Contains(d.Message, "not implemented") {
				t.Errorf("E055 message should say it is not implemented: %q", d.Message)
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
  let x: fixed<16, 16> = 1
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
  let x: fixed<16, 16> = 1
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
		"struct field":  `struct Point { x: fixed<16, 16> }`,
		"newtype base":  `newtype Scaled = fixed<8, 8>`,
		"parameter":     `let take = (v: fixed<16, 16>) -> i64 => 1`,
		"return type":   `let give = () -> fixed<16, 16> => 1`,
		"array element": `let arr: []fixed<16, 16> = []`,
		"type alias":    `type Alias = fixed<4, 4>`,
	} {
		t.Run(name, func(t *testing.T) {
			fixedPointRefusal(t, source, 1)
		})
	}
}
