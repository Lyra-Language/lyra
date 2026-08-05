package ast

import (
	"fmt"
	"testing"
)

// RangePattern.GetName renders a pattern back toward its source form. It is what
// diagnostics interpolate, so a wrong rendering is user-visible even though it
// reaches no golden file — which is how `0..9=` for `0..<=9` survived: the
// operator was printed *after* the bound it qualifies.
//
// Expectations are built from the operands' own GetName rather than hard-coded,
// because a literal does not currently render as source (an IntegerLiteralExpr
// prints as `IntegerLiteralExpr(0, Base: 10)`). That is a separate defect in the
// same messages; what is asserted here is the part this test is about — where the
// operator sits and how an open bound renders.
func TestRangePatternGetName(t *testing.T) {
	zero := &IntegerLiteralExpr{Value: 0}
	nine := &IntegerLiteralExpr{Value: 9}
	ten := &IntegerLiteralExpr{Value: 10}

	cases := []struct {
		name    string
		pattern RangePattern
		want    string
	}{
		{
			"inclusive puts the operator before the end bound",
			RangePattern{Start: zero, End: nine, EndOperator: "<="},
			fmt.Sprintf("%s..<=%s", zero.GetName(), nine.GetName()),
		},
		{
			"exclusive puts the operator before the end bound",
			RangePattern{Start: zero, End: nine, EndOperator: "<"},
			fmt.Sprintf("%s..<%s", zero.GetName(), nine.GetName()),
		},
		// Open bounds: a nil side is an open range, not a missing bound, and
		// renders as nothing at all rather than as a stray separator.
		{
			"open end has no operator and no end bound",
			RangePattern{Start: ten},
			fmt.Sprintf("%s..", ten.GetName()),
		},
		{
			"open start inclusive",
			RangePattern{End: zero, EndOperator: "<="},
			fmt.Sprintf("..<=%s", zero.GetName()),
		},
		{
			"open start exclusive",
			RangePattern{End: zero, EndOperator: "<"},
			fmt.Sprintf("..<%s", zero.GetName()),
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.pattern.GetName(); got != c.want {
				t.Errorf("GetName() = %q; want %q", got, c.want)
			}
		})
	}
}
