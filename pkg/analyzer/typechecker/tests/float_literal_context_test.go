package typechecker_test

import (
	"testing"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// An untyped int literal in a float context is recorded at the float type, the
// same context-directed adaptation int contexts get. Assignability admitted
// these all along; without the re-record the leaf fell back to i64 in the
// backend — an integer value in a float slot (`let x: f64 = -5` printed
// 18446744073709551611, and `x + 0.5` emitted invalid IR).

// The annotation reaches nested literal leaves through arithmetic.
func TestFloatLiteral_Annotation_ReachesNestedLeaves(t *testing.T) {
	src := `let x: f64 = 5 + 3`
	assertPrimitive(t, leafType(t, src, "L"), types.Float64)
	assertPrimitive(t, leafType(t, src, "R"), types.Float64)
}

func TestFloatLiteral_F32Annotation(t *testing.T) {
	assertPrimitive(t, leafType(t, `let x: f32 = 5 + 3`, "L"), types.Float32)
}

// A negated literal adapts too: the NegationExpr recursion stamps the operand.
func TestFloatLiteral_NegatedLiteral(t *testing.T) {
	res := parseCollectAndCheck(t, `let x: f64 = -5`, false)
	assertNoErrors(t, res)
	decl := res.program.Statements[0].(*ast.VarDeclStmt)
	neg := decl.Value.(*ast.NegationExpr)
	typ, ok := res.typeTable.Get(neg.Operand)
	if !ok {
		t.Fatal("no TypeTable entry for the negated literal's operand")
	}
	assertPrimitive(t, typ, types.Float64)
}

// A call argument adapts to a float parameter.
func TestFloatLiteral_CallArgument(t *testing.T) {
	src := "let half = (x: f64) -> f64 => x / 2\nlet y = half(9)"
	res := parseCollectAndCheck(t, src, false)
	assertNoErrors(t, res)
	decl := res.program.Statements[1].(*ast.VarDeclStmt)
	call := decl.Value.(*ast.FunctionCallExpr)
	typ, ok := res.typeTable.Get(call.Arguments[0])
	if !ok {
		t.Fatal("no TypeTable entry for the call argument literal")
	}
	assertPrimitive(t, typ, types.Float64)
}

// A return body adapts to the declared float return type.
func TestFloatLiteral_ReturnBody(t *testing.T) {
	res := parseCollectAndCheck(t, `let f = () -> f64 => 5`, false)
	assertNoErrors(t, res)
	decl := res.program.Statements[0].(*ast.VarDeclStmt)
	lambda := decl.Value.(*ast.LambdaExpr)
	typ, ok := res.typeTable.Get(lambda.Body)
	if !ok {
		t.Fatal("no TypeTable entry for the return body literal")
	}
	assertPrimitive(t, typ, types.Float64)
}

// A struct field value adapts to the field's declared float type.
func TestFloatLiteral_StructField(t *testing.T) {
	src := "struct Pt { v: f64 }\nlet p = Pt { v: 2 }"
	res := parseCollectAndCheck(t, src, false)
	assertNoErrors(t, res)
	decl := res.program.Statements[1].(*ast.VarDeclStmt)
	inst := decl.Value.(*ast.StructInstanceExpr)
	typ, ok := res.typeTable.Get(inst.Fields[0].Value)
	if !ok {
		t.Fatal("no TypeTable entry for the struct field literal")
	}
	assertPrimitive(t, typ, types.Float64)
}

// A data-constructor payload adapts to the declared float payload type.
func TestFloatLiteral_DataPayload(t *testing.T) {
	src := "data Boxed = Wrap(f64)\nlet w = Wrap(7)"
	res := parseCollectAndCheck(t, src, false)
	assertNoErrors(t, res)
	decl := res.program.Statements[1].(*ast.VarDeclStmt)
	lit := decl.Value.(*ast.TupleLiteralExpr)
	typ, ok := res.typeTable.Get(lit.Elements[0])
	if !ok {
		t.Fatal("no TypeTable entry for the payload literal")
	}
	assertPrimitive(t, typ, types.Float64)
}

// A comparison against a float operand adapts the literal sibling, so the
// backend emits an fcmp with two doubles rather than mismatched operand types.
func TestFloatLiteral_ComparisonSibling(t *testing.T) {
	src := "let a: f64 = 1.5\nlet b = a > 4"
	res := parseCollectAndCheck(t, src, false)
	assertNoErrors(t, res)
	decl := res.program.Statements[1].(*ast.VarDeclStmt)
	cmp := decl.Value.(*ast.BooleanBinaryOpExpr)
	typ, ok := res.typeTable.Get(cmp.Right)
	if !ok {
		t.Fatal("no TypeTable entry for the comparison literal")
	}
	assertPrimitive(t, typ, types.Float64)
}

// The adaptation is context-gated: with no float context a literal stays
// untyped (the i64-default path), exactly as before.
func TestFloatLiteral_NoContext_StaysUntyped(t *testing.T) {
	assertPrimitive(t, leafType(t, `let x = 5 + 3`, "L"), types.UntypedInt)
}
