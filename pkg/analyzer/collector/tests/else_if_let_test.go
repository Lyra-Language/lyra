package collector_test

import (
	"testing"

	"github.com/Lyra-Language/lyra/pkg/printer"
)

// An `if` or `if let` chained after an `else` — of an `if let`, or an `if let` after an
// `if`'s — collects exactly as the braced `else { if … }` does (`CollectElseIf`). As
// with a match arm's bare jump, the erasure is what keeps the feature cheap: the
// `match` reading of `if let`, the value-tail rule and the pattern's scope see only
// the block they already handled.
func TestElseIf_CollectsAsTheBracedForm(t *testing.T) {
	cases := []struct{ name, chained, braced string }{
		{
			"if let, else if",
			"let f = (m: Maybe<i64>, b: bool) -> i64 => {\n  if let Some(x) = m { x } else if b { 1 } else { 2 }\n}",
			"let f = (m: Maybe<i64>, b: bool) -> i64 => {\n  if let Some(x) = m { x } else { if b { 1 } else { 2 } }\n}",
		},
		{
			"if let, else if let",
			"let f = (m: Maybe<i64>, n: Maybe<i64>) -> i64 => {\n  if let Some(x) = m { x } else if let Some(y) = n { y } else { 2 }\n}",
			"let f = (m: Maybe<i64>, n: Maybe<i64>) -> i64 => {\n  if let Some(x) = m { x } else { if let Some(y) = n { y } else { 2 } }\n}",
		},
		{
			"if, else if let",
			"let f = (m: Maybe<i64>, b: bool) -> i64 => {\n  if b { 1 } else if let Some(x) = m { x } else { 2 }\n}",
			"let f = (m: Maybe<i64>, b: bool) -> i64 => {\n  if b { 1 } else { if let Some(x) = m { x } else { 2 } }\n}",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			chainedProg, _, _, chainedErrs := parseAndCollect(t, c.chained)
			if len(chainedErrs) != 0 {
				t.Fatalf("chained form failed to collect: %v", chainedErrs)
			}
			bracedProg, _, _, bracedErrs := parseAndCollect(t, c.braced)
			if len(bracedErrs) != 0 {
				t.Fatalf("braced form failed to collect: %v", bracedErrs)
			}
			chained := stripLocations(printer.PrintAST(chainedProg))
			braced := stripLocations(printer.PrintAST(bracedProg))
			if chained != braced {
				t.Errorf("chained and braced forms collect differently:\n--- chained ---\n%s\n--- braced ---\n%s",
					chained, braced)
			}
		})
	}
}
