package checker_test

import (
	"strings"
	"testing"

	"github.com/Lyra-Language/lyra/pkg/analyzer/checker"
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/parser"
)

// lyra-W023 needs no types, so this runs the collector and the pass and nothing else.
func leadingMinusWarnings(t *testing.T, source string) []diag.Diagnostic {
	t.Helper()
	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	c := collector.NewCollector([]byte(source))
	program, _, _, _ := c.Collect(tree.RootNode())
	out := checker.CheckLeadingMinusContinuation(program)
	for _, d := range out {
		if d.Code != diag.CodeLeadingMinusContinuation {
			t.Fatalf("unexpected code %q", d.Code)
		}
		if d.Severity != diag.SeverityWarning {
			t.Fatalf("lyra-W023 must be a warning, got %v", d.Severity)
		}
	}
	return out
}

// **The bug this exists for, verbatim from the code it was found in.**
//
// `examples/raylib/shapes.lyra`'s `sin32` was written this way and had been shipping since
// the file existed: three statements, the first two discarded, the function returning its
// last term. It compiled and ran and was wrong, and nothing said so.
func TestLeadingMinus_TheOriginalBug(t *testing.T) {
	got := leadingMinusWarnings(t, `
let sin32 = pure (radians: f32) -> f32 => {
  let x2 = radians * radians
  radians - radians * x2 / 6.0 + radians * x2 * x2 / 120.0
    - radians * x2 * x2 * x2 / 5040.0
}
`)
	if len(got) != 1 {
		t.Fatalf("want one warning, got %d: %v", len(got), got)
	}
	if !strings.Contains(got[0].Message, "end of the previous line") {
		t.Errorf("message should name the fix; got: %s", got[0].Message)
	}
	if got[0].Location.StartLine != 5 {
		t.Errorf("should point at the `-` line (5), got line %d", got[0].Location.StartLine)
	}
}

// The negation is a *leaf*, not the root: `- x * x / 6.0` parses as `(((-x) * x) / 6.0)`.
// Testing the root would find nothing, which is why the pass walks the left spine.
func TestLeadingMinus_FindsANegationDownTheLeftSpine(t *testing.T) {
	if got := leadingMinusWarnings(t, `
let f = pure (x: f64) -> f64 => {
  x + x
    - x * x * x / 6.0
}
`); len(got) != 1 {
		t.Fatalf("want one warning, got %d: %v", len(got), got)
	}
}

// A bare negation, where the negation *is* the root.
func TestLeadingMinus_FindsABareNegation(t *testing.T) {
	if got := leadingMinusWarnings(t, `
let f = pure (x: f64) -> f64 => {
  x + x
    - x
}
`); len(got) != 1 {
		t.Fatalf("want one warning, got %d: %v", len(got), got)
	}
}

func TestLeadingMinus_Accepted(t *testing.T) {
	for _, c := range []struct{ name, src string }{
		// The correct spelling: the operator ends the line, so it is one expression.
		{"trailing operator", `
let f = pure (x: f64) -> f64 => {
  x + x -
    x * x * x / 6.0
}`},
		// A body that does something and then answers a negation. Ordinary, and the
		// reason the previous statement has to corroborate.
		{"an effectful statement then a negation", `
let f = (x: f64) -> f64 => {
  println("negating")
  0.0 - x
}`},
		// Spelled out on one line with an explicit `;`, so the split is deliberate.
		{"explicit semicolon", `let f = pure (x: f64) -> f64 => { x + x; 0.0 - x }`},
		// One statement, nothing to be a continuation of.
		{"a single statement", `let f = pure (x: f64) -> f64 => 0.0 - x`},
		// A call is not a discarded pure value, so it does not corroborate.
		{"a call then a negation", `
let g = () -> void => {}
let f = pure (x: f64) -> f64 => {
  g()
  0.0 - x
}`},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := leadingMinusWarnings(t, c.src); len(got) != 0 {
				t.Errorf("expected no warning, got %v", got)
			}
		})
	}
}

// Taking the advice must produce a program that means what the author wanted — the only
// way to know a suggested fix is real rather than plausible.
func TestLeadingMinus_TheSuggestedFixIsAccepted(t *testing.T) {
	if got := leadingMinusWarnings(t, `
let sin32 = pure (radians: f32) -> f32 => {
  let x2 = radians * radians
  radians - radians * x2 / 6.0 + radians * x2 * x2 / 120.0 -
    radians * x2 * x2 * x2 / 5040.0
}
`); len(got) != 0 {
		t.Errorf("the fix the message names should be clean, got %v", got)
	}
}
