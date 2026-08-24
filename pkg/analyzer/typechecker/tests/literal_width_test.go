package typechecker_test

import (
	"testing"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// Context-directed literal-width inference: an untyped integer literal is
// recorded in the TypeTable at the concrete width its context demands (an
// annotation, a concrete sibling operand, a declared return type), not the i64
// default. These tests assert on the recorded *leaf* type — the specific gap the
// feature closes — since bottom-up inference already got the enclosing
// expression's result type right.

// leafType returns the recorded TypeTable type of the literal leaf reached by
// descending `path` (each step "L"/"R" picks the Left/Right of a MathBinaryOp)
// from the first statement's initializer.
func leafType(t *testing.T, source, path string) types.Type {
	t.Helper()
	res := parseCollectAndCheck(t, source, false)
	assertNoErrors(t, res)
	decl, ok := res.program.Statements[0].(*ast.VarDeclStmt)
	if !ok {
		t.Fatalf("expected VarDeclStmt, got %T", res.program.Statements[0])
	}
	var cur ast.Expression = decl.Value
	for _, step := range path {
		bin, ok := cur.(*ast.MathBinaryOpExpr)
		if !ok {
			t.Fatalf("path step %q: expected MathBinaryOpExpr, got %T", string(step), cur)
		}
		if step == 'L' {
			cur = bin.Left
		} else {
			cur = bin.Right
		}
	}
	typ, ok := res.typeTable.Get(cur)
	if !ok {
		t.Fatal("no TypeTable entry for the literal leaf")
	}
	return typ
}

func assertPrimitive(t *testing.T, typ types.Type, want types.PrimitiveTypeName) {
	t.Helper()
	p, ok := typ.(types.PrimitiveType)
	if !ok {
		t.Fatalf("expected PrimitiveType %s, got %T (%s)", want, typ, typ)
	}
	if p.Name != want {
		t.Errorf("leaf type = %s; want %s", p.Name, want)
	}
}

// An annotation pushes its width onto every literal leaf, not just the top node.
func TestLiteralWidth_Annotation_ReachesNestedLeaves(t *testing.T) {
	src := `let x: u8 = 5 + 3`
	assertPrimitive(t, leafType(t, src, "L"), types.UInt8)
	assertPrimitive(t, leafType(t, src, "R"), types.UInt8)
}

// A concrete operand fixes the width of its untyped literal sibling.
func TestLiteralWidth_ConcreteSibling_TypesLiteral(t *testing.T) {
	// x is i8 (annotated); `x + 3` should record `3` as i8.
	src := "let x: i8 = 5\nlet y = x + 3"
	res := parseCollectAndCheck(t, src, false)
	assertNoErrors(t, res)
	decl := res.program.Statements[1].(*ast.VarDeclStmt)
	bin := decl.Value.(*ast.MathBinaryOpExpr)
	typ, ok := res.typeTable.Get(bin.Right)
	if !ok {
		t.Fatal("no TypeTable entry for the `3` literal")
	}
	assertPrimitive(t, typ, types.Int8)
}

// Without any context, a literal leaf stays untyped — propagation only fires
// from a concrete context. The backend maps an untyped leaf to the i64 default
// (literalIntType), so this is the "defaults to i64" path.
func TestLiteralWidth_Unannotated_StaysUntyped(t *testing.T) {
	assertPrimitive(t, leafType(t, `let x = 5 + 3`, "L"), types.UntypedInt)
}

// A literal too large for the context width is left untyped (not silently
// wrapped), so no concrete width is recorded on it. The annotated-decl site
// still reports the overflow via the fold-based range check.
func TestLiteralWidth_OverflowLeftUntyped(t *testing.T) {
	res := parseCollectAndCheck(t, `let x: u8 = 300`, false)
	assertErrorsAre(t, res, "x: literal value 300 overflows u8")
}

// **An argument is a value position, and the rule holds in every value position.**
//
// propagateLiteralType deliberately leaves an unfitting literal untyped, "expecting a
// *downstream* site to report it" — which is why the declaration, reassignment and return
// positions each pair it with checkIntegerLiteralRange. An argument has no downstream: the
// literal reached the backend, was narrowed to the parameter's width, and `direct(300)`
// against a `u8` parameter checked clean and printed **44**.
//
// The four call shapes are separate code paths that had all missed it, which is why each is
// asserted here rather than one standing in for the rest.
func TestLiteralWidth_ArgumentOverflow_DirectCall_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
let direct = pure (n: u8) -> u8 => n
let f = () -> u8 => direct(300)`, false)
	assertErrorsAre(t, res, "direct: argument 1 (n): literal value 300 overflows u8")
}

// Through a function-typed *value*, where there is no declaration to read a parameter name
// from — so the subject is positional.
func TestLiteralWidth_ArgumentOverflow_IndirectCall_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
let indirect = pure (f: (u8) -> u8) -> u8 => f(300)`, false)
	assertErrorsAre(t, res, "f: argument 1: literal value 300 overflows u8")
}

// A trait method's declared parameter width is the argument's context too.
func TestLiteralWidth_ArgumentOverflow_TraitMethod_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
struct Box { tag: i64 }
trait Taker { pure take: (Self, u8) -> u8 }
impl Taker for Box { take = pure (self, n) => n }
let f = (b: Box) -> u8 => b.take(300)`, false)
	assertErrorsAre(t, res, "take: argument 1: literal value 300 overflows u8")
}

// The boundary values must still be accepted — a check that rejects 255 for a u8 would be
// worse than the gap it closes.
func TestLiteralWidth_ArgumentAtTheBoundary_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
let take = pure (n: u8, m: i8) -> i64 => i64(n) + i64(m)
let f = () -> i64 => take(255, -128)
let g = () -> i64 => take(0, 127)`, false)
	assertNoErrors(t, res)
}
