package typechecker_test

import (
	"testing"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// A data constructor's argument takes its declared payload-field width
// (context-directed literal-width propagation), the same treatment struct fields,
// tuple elements, and call arguments get — instead of promoting to the i64
// default. Without this a narrow payload's literal would lower at i64 and mismatch
// the variant's field type in the backend's tagged-union construction.
func TestDataConstructor_ArgTakesDeclaredFieldWidth(t *testing.T) {
	res := parseCollectAndCheck(t, `
data Byte = Zero | Wrap(u8)
let b = Wrap(200)
`, false)
	assertNoErrors(t, res)

	// Find the `200` constructor argument and check it recorded as u8.
	var arg ast.Expression
	for _, s := range res.program.Statements {
		vd, ok := s.(*ast.VarDeclStmt)
		if !ok || vd.Name != "b" {
			continue
		}
		if lit, ok := vd.Value.(*ast.TupleLiteralExpr); ok && len(lit.Elements) == 1 {
			arg = lit.Elements[0]
		}
	}
	if arg == nil {
		t.Fatal("could not find the constructor argument expression")
	}
	got, ok := res.typeTable.Get(arg)
	if !ok {
		t.Fatal("constructor argument has no recorded type")
	}
	p, ok := got.(types.PrimitiveType)
	if !ok || p.Name != types.UInt8 {
		t.Errorf("constructor argument recorded as %v; want u8", got)
	}
}

// A data constructor whose declared payload field is a *tuple* of narrow ints
// narrows the tuple literal's element leaves too: propagateExpectedType now
// recurses into a TupleLiteralExpr against its tuple context type, so
// `Wrapped((20, 22))` with `Wrapped((u8, u8))` records `20`/`22` as u8. Before,
// the tuple literal's elements defaulted to i64 and the backend panicked
// building the `{ i8, i8 }` payload (`insertvalue elem type mismatch`).
func TestDataConstructor_TuplePayloadElementsTakeDeclaredWidth(t *testing.T) {
	res := parseCollectAndCheck(t, `
data Wrap = Wrapped((u8, u8))
let w = Wrapped((20, 22))
`, false)
	assertNoErrors(t, res)

	// Find the inner `(20, 22)` tuple literal (the sole ctor argument).
	var inner *ast.TupleLiteralExpr
	for _, s := range res.program.Statements {
		vd, ok := s.(*ast.VarDeclStmt)
		if !ok || vd.Name != "w" {
			continue
		}
		outer, ok := vd.Value.(*ast.TupleLiteralExpr)
		if ok && len(outer.Elements) == 1 {
			inner, _ = outer.Elements[0].(*ast.TupleLiteralExpr)
		}
	}
	if inner == nil || len(inner.Elements) != 2 {
		t.Fatal("could not find the (20, 22) tuple payload")
	}
	for i, elem := range inner.Elements {
		got, ok := res.typeTable.Get(elem)
		if !ok {
			t.Fatalf("tuple payload element %d has no recorded type", i)
		}
		if p, ok := got.(types.PrimitiveType); !ok || p.Name != types.UInt8 {
			t.Errorf("tuple payload element %d recorded as %v; want u8", i, got)
		}
	}
}

// An anonymous tuple literal narrows element-wise against a tuple annotation:
// `let a: (i32, i32) = (1, 2)` type-checks (the untyped elements widen to i32)
// and records each element leaf at the annotated width. Before, the literal
// inferred to (i64, i64) and failed assignability against (i32, i32).
func TestTupleLiteral_AnnotationNarrowsElements(t *testing.T) {
	res := parseCollectAndCheck(t, `let a: (i32, i32) = (1, 2)`, false)
	assertNoErrors(t, res)

	decl := res.program.Statements[0].(*ast.VarDeclStmt)
	tup := decl.Value.(*ast.TupleLiteralExpr)
	for i, elem := range tup.Elements {
		got, ok := res.typeTable.Get(elem)
		if !ok {
			t.Fatalf("element %d has no recorded type", i)
		}
		if p, ok := got.(types.PrimitiveType); !ok || p.Name != types.Int32 {
			t.Errorf("element %d recorded as %v; want i32", i, got)
		}
	}
}
