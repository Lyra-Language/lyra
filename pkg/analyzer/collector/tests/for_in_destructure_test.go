package collector_test

import (
	"strings"
	"testing"
)

// A destructuring loop header collects to an ordinary loop plus a destructuring `let` at
// the top of the body — the form is erased, so no later pass knows it existed.

func TestCollect_ForInDestructuring_IsAccepted(t *testing.T) {
	for name, src := range map[string]string{
		"element only":      `let main = () -> void => { for (a, b) in xs { } }`,
		"index and element": `let main = () -> void => { for i, (a, b) in xs { } }`,
		"wildcard inside":   `let main = () -> void => { for (_, b) in xs { } }`,
		"wildcard index":    `let main = () -> void => { for _, (a, b) in xs { } }`,
	} {
		t.Run(name, func(t *testing.T) {
			if errs := parseAndCollectErrors(t, src); len(errs) > 0 {
				t.Errorf("expected no collector errors, got %v", errs)
			}
		})
	}
}

// The first binding of the two-name form is the **index**, so destructuring it is refused
// where it is written rather than left to fail later as a type error about a name the
// author never wrote ("cannot destructure i64 with a tuple pattern").
func TestCollect_ForInDestructuring_RefusesAPatternInTheIndexSlot(t *testing.T) {
	errs := parseAndCollectErrors(t, `let main = () -> void => { for (a, b), i in xs { } }`)
	var found bool
	for _, e := range errs {
		if strings.Contains(e.Error(), "the first binding of a two-name loop is the index") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected the index-slot refusal, got %v", errs)
	}
}
