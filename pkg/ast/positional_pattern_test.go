package ast

import "testing"

func TestMatchPositions(t *testing.T) {
	id := func(n string) Pattern { return &IdentifierPattern{Name: n} }
	rest := &RestPattern{Identifier: "r"}
	names := func(ps Positions) []string {
		out := make([]string, len(ps.Columns))
		for i, c := range ps.Columns {
			if c != nil {
				out[i] = c.GetName()
			}
		}
		return out
	}
	for _, c := range []struct {
		name     string
		elems    []Pattern
		n        int
		ok       bool
		want     []string
		from, to int
	}{
		{"no rest", []Pattern{id("a"), id("b")}, 2, true, []string{"a", "b"}, 0, 0},
		{"no rest, wrong arity", []Pattern{id("a"), id("b")}, 3, false, nil, 0, 0},
		{"middle", []Pattern{id("a"), rest, id("z")}, 4, true, []string{"a", "", "", "z"}, 1, 3},
		{"leading", []Pattern{rest, id("z")}, 3, true, []string{"", "", "z"}, 0, 2},
		{"trailing", []Pattern{id("a"), rest}, 3, true, []string{"a", "", ""}, 1, 3},
		{"empty rest", []Pattern{id("a"), rest, id("z")}, 2, true, []string{"a", "z"}, 1, 1},
		{"too many around the rest", []Pattern{id("a"), rest, id("y"), id("z")}, 2, false, nil, 0, 0},
		{"two rests", []Pattern{rest, id("a"), &RestPattern{Identifier: "s"}}, 4, false, nil, 0, 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			ps, ok := MatchPositions(c.elems, c.n)
			if ok != c.ok {
				t.Fatalf("ok = %v; want %v", ok, c.ok)
			}
			if !ok {
				return
			}
			got := names(ps)
			if len(got) != len(c.want) {
				t.Fatalf("columns = %q; want %q", got, c.want)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Fatalf("columns = %q; want %q", got, c.want)
				}
			}
			if ps.Rest != nil && (ps.RestFrom != c.from || ps.RestTo != c.to) {
				t.Errorf("rest covers [%d, %d); want [%d, %d)", ps.RestFrom, ps.RestTo, c.from, c.to)
			}
		})
	}
}
