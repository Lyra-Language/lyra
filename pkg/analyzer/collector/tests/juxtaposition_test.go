package collector_test

import (
	"testing"

	"github.com/Lyra-Language/lyra/pkg/ast"
)

// Juxtaposition application (`Some 42`) and the parenthesized spelling
// (`Some(42)`) are one construct, so the collector erases the difference: both
// produce a named TupleLiteralExpr. That is what keeps the typechecker, purity,
// ownership, exhaustiveness and the backend from ever learning juxtaposition
// exists — so it is worth asserting directly rather than only through behaviour.

// firstBindingValue returns the value expression of the program's first binding.
func firstBindingValue(t *testing.T, source string) ast.Expression {
	t.Helper()
	program, _, _, _ := parseAndCollect(t, source)
	for _, node := range program.Statements {
		if decl, ok := node.(*ast.VarDeclStmt); ok {
			return decl.Value
		}
	}
	t.Fatalf("no binding found in %q", source)
	return nil
}

// assertAppliedConstructor checks the value is a named tuple literal with the
// given constructor name and element count.
func assertAppliedConstructor(t *testing.T, e ast.Expression, name string, elements int) *ast.TupleLiteralExpr {
	t.Helper()
	lit, ok := e.(*ast.TupleLiteralExpr)
	if !ok {
		t.Fatalf("expected *ast.TupleLiteralExpr, got %T", e)
	}
	if lit.Name != name {
		t.Errorf("constructor name = %q; want %q", lit.Name, name)
	}
	if len(lit.Elements) != elements {
		t.Errorf("element count = %d; want %d", len(lit.Elements), elements)
	}
	return lit
}

func TestJuxtaposition_MatchesParenSpelling(t *testing.T) {
	jux := assertAppliedConstructor(t, firstBindingValue(t, `let a = Some 42`), "Some", 1)
	paren := assertAppliedConstructor(t, firstBindingValue(t, `let a = Some(42)`), "Some", 1)

	juxLit, ok := jux.Elements[0].(*ast.IntegerLiteralExpr)
	if !ok {
		t.Fatalf("juxtaposed operand is %T, want *ast.IntegerLiteralExpr", jux.Elements[0])
	}
	parenLit, ok := paren.Elements[0].(*ast.IntegerLiteralExpr)
	if !ok {
		t.Fatalf("parenthesized operand is %T, want *ast.IntegerLiteralExpr", paren.Elements[0])
	}
	if juxLit.Value != parenLit.Value {
		t.Errorf("operand values differ: %d vs %d", juxLit.Value, parenLit.Value)
	}
}

// The decision: `Some -1` is `Some(-1)`, not `Some` minus one. A PascalCase name
// is never a variable and never a constant, so the subtraction reading has no
// operand to bind — see the entry in todo.md.
func TestJuxtaposition_SomeNegativeOneIsApplication(t *testing.T) {
	lit := assertAppliedConstructor(t, firstBindingValue(t, `let a = Some -1`), "Some", 1)
	neg, ok := lit.Elements[0].(*ast.NegationExpr)
	if !ok {
		t.Fatalf("operand is %T, want *ast.NegationExpr (i.e. Some(-1), not Some - 1)", lit.Elements[0])
	}
	if inner, ok := neg.Operand.(*ast.IntegerLiteralExpr); !ok || inner.Value != 1 {
		t.Errorf("expected the operand to be -1, got %v", neg.Operand)
	}
}

// A SCREAMING_CASE name is a constant, lexically distinct from a constructor, so
// arithmetic on it is untouched. This is the property that makes the decision
// above safe.
func TestJuxtaposition_ConstantArithmeticUnaffected(t *testing.T) {
	if _, ok := firstBindingValue(t, `let a = MAX - 1`).(*ast.MathBinaryOpExpr); !ok {
		t.Errorf("`MAX - 1` must stay a binary expression, got %T",
			firstBindingValue(t, `let a = MAX - 1`))
	}
}

// A positional payload keeps its own parens and its existing flat element list —
// `Rect(3, 4)` is not re-collected as one tuple-valued operand.
func TestJuxtaposition_PositionalPayloadUnchanged(t *testing.T) {
	assertAppliedConstructor(t, firstBindingValue(t, `let a = Rect(3, 4)`), "Rect", 2)
}

func TestJuxtaposition_OperandForms(t *testing.T) {
	cases := []struct {
		name string
		src  string
	}{
		{"string literal", `let a = Ok "yes"`},
		{"identifier", `let a = Some x`},
		{"nullary constructor", `let a = Some None`},
		{"array literal", `let a = Some [1, 2]`},
		{"nested application", `let a = Some Some 1`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, ok := firstBindingValue(t, c.src).(*ast.TupleLiteralExpr); !ok {
				t.Errorf("%s: got %T, want a named tuple literal", c.src, firstBindingValue(t, c.src))
			}
		})
	}
}
