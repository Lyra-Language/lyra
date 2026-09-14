package typechecker_test

import (
	"strings"
	"testing"
)

// A tuple position written in place obeys the rules a field write does: the value must fit
// the element's type, and the root binding must allow interior mutation (09/13).
func TestTupleIndexAssignment_Rules(t *testing.T) {
	for _, c := range []struct{ name, src, want string }{
		{"wrong element type", `let f = () -> void => { var p = (1, "a"); p.1 = 5 }`, "cannot assign"},
		{"a deeply immutable let", `let f = () -> void => { let p = (1, 2); p.0 = 3 }`, "deeply immutable"},
		{"an immutable parameter", `let f = (p: (i64, i64)) -> void => { p.0 = 3 }`, "immutable borrow"},
		{"out of range", `let f = () -> void => { var p = (1, 2); p.2 = 3 }`, "out of range"},
	} {
		t.Run(c.name, func(t *testing.T) {
			res := parseCollectAndCheck(t, c.src, false)
			found := false
			for _, e := range res.errors {
				found = found || strings.Contains(e.Message, c.want)
			}
			if !found {
				t.Errorf("want an error containing %q, got %v", c.want, res.errors)
			}
		})
	}
	res := parseCollectAndCheck(t, `let f = (p: mut (i64, string)) -> void => { p.0 += 1; p.1 = "b" }`, false)
	for _, e := range res.errors {
		t.Errorf("a `mut` parameter's tuple positions are writable; got %q", e.Message)
	}
}
