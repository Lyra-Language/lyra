package typechecker_test

import (
	"strings"
	"testing"
)

// A descending range is refused where a range is a **set** rather than an iteration.
//
// `5..>1` as a match pattern or a `newtype` constraint describes exactly the members
// `1..<5` does, so the spelling implies an order the construct does not have. The grammar
// accepts all four operators at all three sites — one node kind, kept that way by the 08/01
// unification — and the collector draws the line, which is what lets the message name the
// ascending spelling of the same set instead of pointing at a token.
func TestDescendingRange_RefusedInAPattern(t *testing.T) {
	errs := collectOnly(t, `
let classify = (n: i64) -> i64 => match n {
  5..>1 => 1,
  _ => 0,
}
`)
	if errs == "" {
		t.Fatal("expected a descending range pattern to be refused")
	}
	for _, want := range []string{"set has no direction", "1..<5"} {
		if !strings.Contains(errs, want) {
			t.Errorf("expected %q in the message, got %q", want, errs)
		}
	}
}

func TestDescendingRange_RefusedInAConstraint(t *testing.T) {
	errs := collectOnly(t, "newtype Pct = u8 where range(100..>=0)\n")
	if errs == "" {
		t.Fatal("expected a descending range constraint to be refused")
	}
	// The suggestion is built from the bounds, not by splicing the node's source — the
	// constraint's text is the whole `range(…)` wrapper, and splicing it produced an
	// unparseable suggestion, which is worse than none.
	if !strings.Contains(errs, "0..<=100") {
		t.Errorf("expected a parseable ascending suggestion, got %q", errs)
	}
}

// An *expression* range is where direction means something, so it is accepted.
func TestDescendingRange_AllowedInAnExpression(t *testing.T) {
	if errs := collectOnly(t, "let xs = [x in 5..>=1 | x]\n"); errs != "" {
		t.Errorf("a descending expression range should be accepted, got %q", errs)
	}
}
