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
	// An unannotated untyped-literal element stays untyped in the TypeTable — the
	// same "unannotated leaf stays untyped, the backend maps it to the i64
	// default" rule scalars follow (TestLiteralWidth_Unannotated_StaysUntyped).
	// This is what lets a *narrowing* context (a tuple annotation, a data-ctor or
	// struct tuple field) later fix the element width via propagateExpectedType.
	elem0, ok := res.typeTable.Get(tup.Elements[0])
	if !ok {
		t.Fatal("no TypeTable entry for first tuple element")
	}
	p0, ok := elem0.(types.PrimitiveType)
	if !ok || p0.Name != types.UntypedInt {
		t.Errorf("expected first element type untyped_int, got %s", elem0)
	}
	// A non-numeric element already has a concrete type, unaffected by widening.
	elem1, ok := res.typeTable.Get(tup.Elements[1])
	if !ok {
		t.Fatal("no TypeTable entry for second tuple element")
	}
	p1, ok := elem1.(types.PrimitiveType)
	if !ok || p1.Name != types.String {
		t.Errorf("expected second element type string, got %s", elem1)
	}
	// The tuple *type* the backend reads still settles to its i64 default (the
	// leaf's untyped-ness is an inference detail, not the tuple's element type).
	tupType, ok := res.typeTable.Get(tup)
	if !ok {
		t.Fatal("no TypeTable entry for the tuple literal")
	}
	tt, ok := tupType.(types.TupleType)
	if !ok || len(tt.Elements) != 2 {
		t.Fatalf("expected a 2-element tuple type, got %s", tupType)
	}
	if p, ok := tt.Elements[0].(types.PrimitiveType); !ok || p.Name != types.Int64 {
		t.Errorf("expected tuple type element 0 to be i64, got %s", tt.Elements[0])
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

// **A destructured binding settles to its default width, exactly as a scalar one does.**
//
// `let j = 1` gives `j` the i64 default, so `a + j` on a `u8` is refused as a mixed-width
// add. The destructured spelling left the element *untyped*, so the operator check had no
// concrete type to object to and let it through — and the backend then emitted `u8 + i64`,
// which clang rejects outright:
//
//	invalid LLVM IR input: Intrinsic called with incompatible signature
//	  %13 = call { i8, i1 } @llvm.uadd.with.overflow.i8(i8 %11, i64 %12)
//
// The front end accepting a form the backend cannot build is rule 5 inverted; the two
// spellings now give the same answer.
func TestTupleDestructuring_UntypedElementSettlesToDefault_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
let main = () -> void => {
  let (i, j) = (10, 1)
  let a: u8 = 5
  println(a + j)
}`, false)
	assertErrorsAre(t, res, "operator +: incompatible types: u8 and i64")
}

// An annotation still fixes the widths, and narrow values still fit them — promoting must
// not reach past a declared type.
func TestTupleDestructuring_AnnotatedWidthsSurvive_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
let main = () -> void => {
  let (a, b): (u8, i8) = (200, -100)
  println(i64(a) + i64(b))
}`, false)
	assertNoErrors(t, res)
}
