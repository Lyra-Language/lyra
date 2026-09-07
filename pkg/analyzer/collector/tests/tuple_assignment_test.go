package collector_test

import (
	"strings"
	"testing"
)

// A tuple assignment is desugared by the collector into a block: one destructuring
// `let` of synthesized names, then one ordinary assignment per place. The golden
// output *is* the desugaring, so a change to the shape shows up here.
func TestTupleAssignment_DesugarsToBlock(t *testing.T) {
	source := `
let f = () -> void => {
	var a = 1
	var b = 2
	(a, b) = (b, a)
}`
	runGoldenTest(t, source, "stmt_tuple_assignment")
}

func TestTupleAssignment_EveryPlaceKind(t *testing.T) {
	source := `
let f = (p: mut Pt, xs: mut []i64, q: ^mut i64) -> void => {
	var a = 1
	unsafe { (a, p.x, xs[0], q^) = (p.x, xs[0], q^, a) }
}`
	runGoldenTest(t, source, "stmt_tuple_assignment_places")
}

// A non-place element is refused by the collector, naming what a place is.
func TestTupleAssignment_NonPlaceIsRefused(t *testing.T) {
	cases := []struct{ name, source, want string }{
		{"literal", `let f = () -> void => { var a = 1
			(a, 5) = (1, 2) }`, "must be a place"},
		{"call", `let f = () -> void => { var a = 1
			(a, g()) = (1, 2) }`, "must be a place"},
		{"constructor target", `let f = () -> void => { var a = 1
			Some(a) = None }`, "cannot assign to a constructor"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			errs := parseAndCollectErrors(t, c.source)
			for _, e := range errs {
				if strings.Contains(e.Error(), c.want) {
					return
				}
			}
			t.Errorf("expected an error containing %q, got %v", c.want, errs)
		})
	}
}
