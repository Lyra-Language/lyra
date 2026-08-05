package ast

import "testing"

// Every GetName on an expression is a **source rendering**. Parents compose them — a
// match arm builds `match <pattern> { <body> }` out of its children's — and diagnostics
// interpolate the result, so what these return is read by users about their own code.
//
// The literals did not hold up their end until 08/03: they printed their Go type and
// fields, which is how `expected array pattern, got IntegerLiteralExpr(0, Base: 10)..<=
// IntegerLiteralExpr(10, Base: 10)` reached a diagnostic. Nothing caught it because
// GetName reaches no golden file — these tests are the substitute for that.

func TestGetName_Literals(t *testing.T) {
	cases := []struct {
		name string
		expr Named
		want string
	}{
		{"decimal", &IntegerLiteralExpr{Value: 42, Base: IntegerBase10}, "42"},
		{"hex keeps its base", &IntegerLiteralExpr{Value: 255, Base: IntegerBase16}, "0xFF"},
		{"binary keeps its base", &IntegerLiteralExpr{Value: 5, Base: IntegerBase2}, "0b101"},
		{"octal keeps its base", &IntegerLiteralExpr{Value: 8, Base: IntegerBase8}, "0o10"},
		{"float", &FloatLiteralExpr{Value: 1.5}, "1.5"},
		{"string is quoted", &StringLiteralExpr{Value: "hi"}, `"hi"`},
		{"empty string is visible", &StringLiteralExpr{Value: ""}, `""`},
		{"bool", &BooleanLiteralExpr{Value: true}, "true"},
		{"rune", &CharacterLiteralExpr{Value: 'x'}, "'x'"},
		{"regex uses the current spelling", &RegexLiteralExpr{Pattern: `\d+`}, `r"\d+"`},
	}
	for _, c := range cases {
		if got := c.expr.GetName(); got != c.want {
			t.Errorf("%s: GetName() = %q; want %q", c.name, got, c.want)
		}
	}
}

// An unsigned literal renders its magnitude, not the bit pattern it is stored as.
func TestGetName_UnsignedIntegerRendersItsMagnitude(t *testing.T) {
	lit := &IntegerLiteralExpr{Value: -1, Base: IntegerBase10, Unsigned: true}
	if got, want := lit.GetName(), "18446744073709551615"; got != want {
		t.Errorf("GetName() = %q; want %q", got, want)
	}
}

func TestGetName_InterpolatedString(t *testing.T) {
	s := &InterpolatedStringExpr{Segments: []Expression{
		&StringLiteralExpr{Value: "a"},
		&IdentifierExpr{Name: "b"},
		&StringLiteralExpr{Value: "c"},
	}}
	if got, want := s.GetName(), `"a${b}c"`; got != want {
		t.Errorf("GetName() = %q; want %q", got, want)
	}
}

// Postfix forms compose their operand's rendering rather than wrapping it in the node's
// Go type name.
func TestGetName_PostfixForms(t *testing.T) {
	xs := &IdentifierExpr{Name: "xs"}
	zero := &IntegerLiteralExpr{Value: 0, Base: IntegerBase10}
	cases := []struct {
		expr Named
		want string
	}{
		{&IndexExpr{Object: xs, Index: zero}, "xs[0]"},
		{&MemberExpr{Object: xs, Property: IdentifierExpr{Name: "len"}}, "xs.len"},
		{&TupleIndexExpr{Object: xs, Index: 1}, "xs.1"},
		{&TryExpr{Operand: xs}, "xs?"},
		{&TraitMethodPathExpr{TraitName: "Show", Method: IdentifierExpr{Name: "show"}}, "Show::show"},
		{&ArrayRepeatExpr{Value: zero, Count: &IntegerLiteralExpr{Value: 3, Base: IntegerBase10}}, "[0; 3]"},
	}
	for _, c := range cases {
		if got := c.expr.GetName(); got != c.want {
			t.Errorf("GetName() = %q; want %q", got, c.want)
		}
	}
}

// A pattern list renders as source too. Formatting the slice with %v printed Go's view of
// it — a list of pointers — which is what a tuple or array pattern used to show.
func TestGetName_PatternLists(t *testing.T) {
	a := &IdentifierPattern{Name: "a"}
	b := &IdentifierPattern{Name: "b"}
	if got, want := (&TuplePattern{Elements: []Pattern{a, b}}).GetName(), "(a, b)"; got != want {
		t.Errorf("tuple pattern: %q; want %q", got, want)
	}
	if got, want := (&ArrayPattern{Elements: []Pattern{a, b}}).GetName(), "[a, b]"; got != want {
		t.Errorf("array pattern: %q; want %q", got, want)
	}
}

// The literal a pattern holds is an expression, so its rendering is the expression's —
// not whatever %v makes of an `any` holding a pointer.
func TestGetName_LiteralPatternRendersItsExpression(t *testing.T) {
	p := &LiteralPattern{Value: &IntegerLiteralExpr{Value: 7, Base: IntegerBase10}}
	if got, want := p.GetName(), "7"; got != want {
		t.Errorf("GetName() = %q; want %q", got, want)
	}
}

// No expression rendering may name a Go type. This is the guard that generalizes the
// specific cases above: a node added later gets caught here rather than in a user's
// diagnostic.
func TestGetName_NoRenderingLeaksAGoTypeName(t *testing.T) {
	xs := &IdentifierExpr{Name: "xs"}
	zero := &IntegerLiteralExpr{Value: 0, Base: IntegerBase10}
	for _, e := range []Named{
		zero,
		&FloatLiteralExpr{Value: 1},
		&StringLiteralExpr{Value: "s"},
		&BooleanLiteralExpr{Value: false},
		&CharacterLiteralExpr{Value: 'c'},
		&RegexLiteralExpr{Pattern: "x"},
		&InterpolatedStringExpr{Segments: []Expression{xs}},
		&IndexExpr{Object: xs, Index: zero},
		&MemberExpr{Object: xs, Property: IdentifierExpr{Name: "f"}},
		&TupleIndexExpr{Object: xs, Index: 0},
		&TryExpr{Operand: xs},
		&TraitMethodPathExpr{TraitName: "T", Method: IdentifierExpr{Name: "m"}},
		&ArrayRepeatExpr{Value: zero, Count: zero},
		&TuplePattern{Elements: []Pattern{&IdentifierPattern{Name: "a"}}},
		&ArrayPattern{Elements: []Pattern{&IdentifierPattern{Name: "a"}}},
		&LiteralPattern{Value: zero},
		&RegexPattern{Pattern: "x"},
	} {
		if name := e.GetName(); containsGoTypeName(name) {
			t.Errorf("%T renders a Go type name: %q", e, name)
		}
	}
}

// containsGoTypeName looks for the tell — this codebase's node types all end in `Expr`
// or `Pattern`, and neither substring can appear in Lyra source that these render.
func containsGoTypeName(s string) bool {
	for _, marker := range []string{"Expr", "Pattern"} {
		for i := 0; i+len(marker) <= len(s); i++ {
			if s[i:i+len(marker)] == marker {
				return true
			}
		}
	}
	return false
}
