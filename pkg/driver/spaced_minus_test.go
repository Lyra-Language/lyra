package driver_test

import (
	"strings"
	"testing"

	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
)

const spacedLevel = `module main
data Level = Empty | Full(i64)
type Cents = i64
tuple Pair(i64)
let amount = pure (l: Level) -> i64 => match l {
  Full(n) => n,
  Empty => 0,
}
impl Sub for Level {
  (_-_) = pure (a, b) => Full(amount(a) - amount(b))
}
`

// `Empty - 1` is `Empty(-1)`: a constructor takes the operand written after it. With a space on
// both sides of the `-` it reads as the subtraction it is not, and the typechecker says so once
// (lyra-W033's two cases). On a constructor with no payload only the subtraction can have been
// meant, so it is one error — where it was a payload error and a negation of the wrong type. On
// one with a payload — a data constructor's, a `type`'s, a named tuple's — the application
// compiles, so it is a warning.
func TestSpacedMinus_ANoPayloadConstructorIsOneError(t *testing.T) {
	res := analyzeModules(t, map[string]string{"main.lyra": spacedLevel + `
let main = () -> void => println("${amount(Empty - Full(1))}")
`})
	errs := res.Errors()
	if len(errs) != 1 {
		t.Fatalf("want one error, got %d: %v", len(errs), errs)
	}
	if !strings.Contains(errs[0].Message, "`Empty - Full(1)` applies Empty to `-Full(1)`, and Empty takes no payload") ||
		!strings.Contains(errs[0].Message, "`(Empty) - Full(1)` to subtract") {
		t.Errorf("message: %q", errs[0].Message)
	}
	for _, d := range res.Diagnostics {
		if d.Code == diag.CodeSpacedNegatedOperand {
			t.Errorf("want the error alone, also got %s: %s", d.Code, d.Message)
		}
	}
}

func TestSpacedMinus_APayloadIsAWarning(t *testing.T) {
	for _, c := range []struct{ expr, want string }{
		{"amount(Full - 1)", "`Full - 1` applies Full to `-1`; it is not a subtraction. Write `Full(-1)` for the payload, or `(Full) - 1` to subtract"},
		{"i64(Cents - 1)", "`Cents - 1` applies Cents to `-1`"},
		{"(Pair - 2).0", "`Pair - 2` applies Pair to `-2`"},
	} {
		t.Run(c.expr, func(t *testing.T) {
			res := analyzeModules(t, map[string]string{"main.lyra": spacedLevel + `
let main = () -> void => println("${` + c.expr + `}")
`})
			for _, d := range res.Errors() {
				t.Errorf("unexpected error: %s", d.Message)
			}
			var got []diag.Diagnostic
			for _, d := range res.Diagnostics {
				if d.Code == diag.CodeSpacedNegatedOperand {
					got = append(got, d)
				}
			}
			if len(got) != 1 || !strings.Contains(got[0].Message, c.want) {
				t.Errorf("want one lyra-W033 containing %q, got %v", c.want, got)
			}
		})
	}
}

// The application's own spelling, the parenthesized payload, and the subtraction written with
// the constructor parenthesized say nothing.
func TestSpacedMinus_OtherSpellingsAreQuiet(t *testing.T) {
	for _, expr := range []string{"amount(Full -1)", "amount(Full(-1))", "amount((Empty) - Full(1))", "amount(Full(3) - Empty)"} {
		t.Run(expr, func(t *testing.T) {
			res := analyzeModules(t, map[string]string{"main.lyra": spacedLevel + `
let main = () -> void => println("${` + expr + `}")
`})
			for _, d := range res.Errors() {
				t.Errorf("unexpected error: %s", d.Message)
			}
			for _, d := range res.Diagnostics {
				if d.Code == diag.CodeSpacedNegatedOperand {
					t.Errorf("unexpected %s: %s", d.Code, d.Message)
				}
			}
		})
	}
}
