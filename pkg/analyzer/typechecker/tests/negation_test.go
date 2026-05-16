package typechecker_test

import (
	"testing"

	"github.com/Lyra-Language/lyra/pkg/ast"
)

// --- valid negation: signed integer targets ---

func TestTypeCheck_Negation_IntAnnotation(t *testing.T) {
	res := parseCollectAndCheck(t, `let x: int = -42`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_Negation_I32Annotation(t *testing.T) {
	res := parseCollectAndCheck(t, `let x: i32 = -5`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_Negation_I64Annotation(t *testing.T) {
	res := parseCollectAndCheck(t, `let x: i64 = -1000000000000`, false)
	assertNoErrors(t, res)
}

// --- valid negation: float targets ---

func TestTypeCheck_Negation_F64Annotation(t *testing.T) {
	res := parseCollectAndCheck(t, `let x: f64 = -3`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_Negation_F32Annotation(t *testing.T) {
	res := parseCollectAndCheck(t, `let x: f32 = -3`, false)
	assertNoErrors(t, res)
}

// --- no annotation: promotes to int ---

func TestTypeCheck_Negation_NoAnnotation(t *testing.T) {
	res := parseCollectAndCheck(t, `let x = -42`, false)
	assertNoErrors(t, res)
}

// --- TypeTable entries ---

func TestTypeCheck_Negation_TypeTable_NoAnnotation(t *testing.T) {
	res := parseCollectAndCheck(t, `let x = -42`, false)
	decl := res.program.Statements[0].(*ast.VarDeclStmt)
	typ, ok := res.typeTable.Get(decl.Value)
	if !ok {
		t.Fatal("expected type table entry for negation expr")
	}
	if got := typ.String(); got != "int" {
		t.Errorf("expected int, got %s", got)
	}
}

func TestTypeCheck_Negation_TypeTable_I32Annotation(t *testing.T) {
	res := parseCollectAndCheck(t, `let x: i32 = -5`, false)
	decl := res.program.Statements[0].(*ast.VarDeclStmt)
	typ, ok := res.typeTable.Get(decl.Value)
	if !ok {
		t.Fatal("expected type table entry for negation expr")
	}
	if got := typ.String(); got != "i32" {
		t.Errorf("expected i32, got %s", got)
	}
}

// --- binary expressions with negated literals ---

func TestTypeCheck_Negation_BinaryExpr_NegatedPlusPositive(t *testing.T) {
	// -3 + 5: UntypedSignedInt + UntypedInt → UntypedSignedInt (promotes to int)
	res := parseCollectAndCheck(t, `let x = -3 + 5`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_Negation_BinaryExpr_WithSignedVar(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let a: i64 = 10
		let x = a + -2
	`, false)
	assertNoErrors(t, res)
}

// --- reassignment with negated literal ---

func TestTypeCheck_Negation_Reassignment_Valid(t *testing.T) {
	res := parseCollectAndCheck(t, `
		var x: int = 0
		x = -5
	`, false)
	assertNoErrors(t, res)
}

// --- error cases ---

func TestTypeCheck_Negation_UnsignedAnnotation_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `let x: u8 = -1`, false)
	assertErrorCount(t, res, 1)
	assertErrorContains(t, res, "x: cannot assign integer literal to u8")
}

func TestTypeCheck_Negation_ConcreteUnsignedVar_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let a: u32 = 5
		let b = -a
	`, false)
	assertErrorCount(t, res, 1)
	assertErrorContains(t, res, "cannot negate unsigned type u32")
}

func TestTypeCheck_Negation_NonNumeric_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let flag: bool = true
		let x = -flag
	`, false)
	assertErrorCount(t, res, 1)
	assertErrorContains(t, res, "cannot negate non-numeric type boolean")
}
