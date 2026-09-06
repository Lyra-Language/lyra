package collector_test

import (
	"strings"
	"testing"
)

// The left side of a compound assignment is an assignable **place** — a binding, a field or
// an element — matching what `=` accepts.
//
// It admitted only a bare identifier until the interior forms were allowed, which made
// `counts[i].n += 1` a collection-time refusal while `counts[i].n = counts[i].n + 1` on the
// identical place compiled. The wording names the three kinds rather than saying "an
// identifier", since that message described the implementation rather than the rule.
func TestCollect_CompoundAssignment_AcceptsPlaces(t *testing.T) {
	for name, src := range map[string]string{
		"a binding":     `let main = () -> void => { var x = 1; x += 1 }`,
		"a field":       `struct Pt { x: i64 }` + "\n" + `let main = () -> void => { var p = Pt { x: 1 }; p.x += 1 }`,
		"an element":    `let main = () -> void => { var xs: []i64 = [1]; xs[0] += 1 }`,
		"a nested path": `struct Pt { x: i64 }` + "\n" + `struct B { p: Pt }` + "\n" + `let main = () -> void => { var bs: []B = [B { p: Pt { x: 1 } }]; bs[0].p.x += 1 }`,
	} {
		t.Run(name, func(t *testing.T) {
			if errs := parseAndCollectErrors(t, src); len(errs) > 0 {
				t.Errorf("expected no collector errors, got %v", errs)
			}
		})
	}
}

func TestCollect_CompoundAssignment_RefusesANonPlace(t *testing.T) {
	errs := parseAndCollectErrors(t, `
let f = () -> i64 => 1
let main = () -> void => { f() += 1 }
`)
	var found bool
	for _, e := range errs {
		if strings.Contains(e.Error(), "must be a binding, field or element") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected the non-place refusal, got %v", errs)
	}
}
