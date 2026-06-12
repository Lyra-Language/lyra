package typechecker_test

import (
	"testing"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// ── element types recorded in TypeTable ─────────────────────────────────────

func TestTupleLiteral_ElementTypesInTypeTable(t *testing.T) {
	res := parseCollectAndCheck(t, `let p = (1, "hello")`, false)
	assertNoErrors(t, res)
	decl := res.program.Statements[0].(*ast.VarDeclStmt)
	tup := decl.Value.(*ast.TupleLiteralExpr)
	if len(tup.Elements) != 2 {
		t.Fatalf("expected 2 elements, got %d", len(tup.Elements))
	}
	elem0, ok := res.typeTable.Get(tup.Elements[0])
	if !ok {
		t.Fatal("no TypeTable entry for first tuple element")
	}
	p0, ok := elem0.(types.PrimitiveType)
	if !ok || p0.Name != types.Int64 {
		t.Errorf("expected first element type i64, got %s", elem0)
	}
	elem1, ok := res.typeTable.Get(tup.Elements[1])
	if !ok {
		t.Fatal("no TypeTable entry for second tuple element")
	}
	p1, ok := elem1.(types.PrimitiveType)
	if !ok || p1.Name != types.String {
		t.Errorf("expected second element type string, got %s", elem1)
	}
}

// ── destructuring: happy path ─────────────────────────────────────────────

func TestTupleDestructuring_MatchingArity_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `let (x, y) = (1, "hello")`, false)
	assertNoErrors(t, res)
}

func TestTupleDestructuring_ThreeElements_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `let (x, y, z) = (1, 2, 3)`, false)
	assertNoErrors(t, res)
}

// ── destructuring: arity mismatch ────────────────────────────────────────

func TestTupleDestructuring_TooFewPatternElements_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `let (x, y) = (1, 2, 3)`, false)
	assertErrorsAre(t, res, "tuple pattern has 2 element(s) but tuple has 3")
}

func TestTupleDestructuring_TooManyPatternElements_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `let (x, y, z) = (1, 2)`, false)
	assertErrorsAre(t, res, "tuple pattern has 3 element(s) but tuple has 2")
}

// ── destructuring: non-tuple RHS ─────────────────────────────────────────

func TestTupleDestructuring_NonTupleRhs_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `let (x, y) = 42`, false)
	assertErrorsAre(t, res, "cannot destructure integer literal with a tuple pattern")
}

func TestTupleDestructuring_StringRhs_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `let (x, y) = "hello"`, false)
	assertErrorsAre(t, res, "cannot destructure string with a tuple pattern")
}

// ── destructuring: rest pattern skips arity check ────────────────────────

func TestTupleDestructuring_RestAtEnd_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `let (x, ...rest) = (1, 2, 3)`, false)
	assertNoErrors(t, res)
}

func TestTupleDestructuring_RestAtBeginning_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `let (...rest, x) = (1, 2, 3)`, false)
	assertNoErrors(t, res)
}
