package typechecker_test

import (
	"testing"

	"github.com/Lyra-Language/lyra/pkg/ast"
)

// --- identifier resolution uses TypeTable (fix #2) ---

func TestTypeCheck_Ident_RefersToAnnotatedDecl(t *testing.T) {
	// y picks up i32 from x's annotation via the TypeTable.
	res := parseCollectAndCheck(t, `
		let x: i32 = 1
		let y: i32 = x
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_Ident_RefersToUnannotatedDecl(t *testing.T) {
	// x has no annotation; TypeTable records "i64". y: i64 = x should pass.
	res := parseCollectAndCheck(t, `
		let x = 42
		let y: i64 = x
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_Ident_MismatchViaReference(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let x: string = "hi"
		let y: i64 = x
	`, false)
	assertErrorsAre(t, res, "y: cannot assign string to i64")
}

// --- binary expression inference (#1) ---

// no-annotation: result type is inferred from operands

func TestTypeCheck_BinaryExpr_IntLiterals_NoAnnotation(t *testing.T) {
	res := parseCollectAndCheck(t, `let x = 1 + 2`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_BinaryExpr_FloatLiterals_NoAnnotation(t *testing.T) {
	res := parseCollectAndCheck(t, `let x = 1.0 + 2.0`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_BinaryExpr_IntAndFloatLiterals_NoAnnotation(t *testing.T) {
	res := parseCollectAndCheck(t, `let x = 1 + 2.0`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_BinaryExpr_FloatAndIntLiterals_NoAnnotation(t *testing.T) {
	res := parseCollectAndCheck(t, `let x = 1.0 + 2`, false)
	assertNoErrors(t, res)
}

// annotation compatible with binary result

func TestTypeCheck_BinaryExpr_I32Annotation_IntLiterals(t *testing.T) {
	res := parseCollectAndCheck(t, `let x: i32 = 1 + 2`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_BinaryExpr_F64Annotation_FloatLiterals(t *testing.T) {
	res := parseCollectAndCheck(t, `let x: f64 = 1.0 + 2.0`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_BinaryExpr_F64Annotation_IntAndFloatLiterals(t *testing.T) {
	res := parseCollectAndCheck(t, `let x: f64 = 1 + 2.0`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_BinaryExpr_F64Annotation_FloatAndIntLiterals(t *testing.T) {
	res := parseCollectAndCheck(t, `let x: f64 = 1.0 + 2`, false)
	assertNoErrors(t, res)
}

// annotation mismatch with binary result

func TestTypeCheck_BinaryExpr_StringAnnotation_IntAddition(t *testing.T) {
	res := parseCollectAndCheck(t, `let x: string = 1 + 2`, false)
	assertErrorsAre(t, res, "x: cannot assign integer literal to string")
}

func TestTypeCheck_BinaryExpr_IntAnnotation_FloatAddition(t *testing.T) {
	res := parseCollectAndCheck(t, `let x: i64 = 1.0 + 2.0`, false)
	assertErrorsAre(t, res, "x: cannot assign float literal to i64")
}

func TestTypeCheck_BinaryExpr_IntAnnotation_MixedAddition(t *testing.T) {
	res := parseCollectAndCheck(t, `let x: i64 = 1 + 2.0`, false)
	assertErrorsAre(t, res, "x: cannot assign float literal to i64")
}

// Incompatible concrete types
func TestTypeCheck_BinaryExpr_ConcreteIntAndConcreteInt_DifferentSizes_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let a: i32 = 1
		let b: i64 = 2
		let x = a + b
	`, false)
	assertErrorsAre(t, res, "operator +: incompatible types: i32 and i64")
}

func TestTypeCheck_BinaryExpr_ConcreteIntAndConcreteFloat_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let a: i32 = 1
		let b: f64 = 2.0
		let x = a + b
	`, false)
	assertErrorsAre(t, res, "operator +: incompatible types: i32 and f64")
}

// operand type errors

func TestTypeCheck_BinaryExpr_NonNumericOperand(t *testing.T) {
	// flag is bool (non-numeric); adding it to an i64 should error.
	res := parseCollectAndCheck(t, `
		let flag: bool = true
		let x = flag + 1
	`, false)
	assertErrorsAre(t, res, "operator +: operands must be numeric, got boolean and integer literal")
}

// TypeTable records the result type

func TestTypeCheck_BinaryExpr_TypeTable_UntypedInt(t *testing.T) {
	res := parseCollectAndCheck(t, `let x = 1 + 2`, false)
	decl := res.program.Statements[0].(*ast.VarDeclStmt)
	typ, ok := res.typeTable.Get(decl.Value)
	if !ok {
		t.Fatal("expected type table entry for binary expr")
	}
	if got := typ.String(); got != "i64" {
		t.Errorf("expected i64, got %s", got)
	}
}

func TestTypeCheck_BinaryExpr_TypeTable_IntPlusFloat_IsF64(t *testing.T) {
	res := parseCollectAndCheck(t, `let x = 1 + 2.0`, false)
	decl := res.program.Statements[0].(*ast.VarDeclStmt)
	typ, ok := res.typeTable.Get(decl.Value)
	if !ok {
		t.Fatal("expected type table entry")
	}
	if got := typ.String(); got != "f64" {
		t.Errorf("expected f64, got %s", got)
	}
}

func TestTypeCheck_BinaryExpr_TypeTable_FloatPlusInt_IsF64(t *testing.T) {
	res := parseCollectAndCheck(t, `let x = 1.0 + 2`, false)
	decl := res.program.Statements[0].(*ast.VarDeclStmt)
	typ, ok := res.typeTable.Get(decl.Value)
	if !ok {
		t.Fatal("expected type table entry")
	}
	if got := typ.String(); got != "f64" {
		t.Errorf("expected f64, got %s", got)
	}
}

func TestTypeCheck_BinaryExpr_TypeTable_FloatPlusFloat_IsF64(t *testing.T) {
	res := parseCollectAndCheck(t, `let x = 1.0 + 2.0`, false)
	decl := res.program.Statements[0].(*ast.VarDeclStmt)
	typ, ok := res.typeTable.Get(decl.Value)
	if !ok {
		t.Fatal("expected type table entry")
	}
	if got := typ.String(); got != "f64" {
		t.Errorf("expected f64, got %s", got)
	}
}

func TestTypeCheck_BinaryExpr_TypeTable_ConcreteInt_WinsOverUntyped(t *testing.T) {
	// untyped i64 + i64 → i64
	res := parseCollectAndCheck(t, `
		let a: i64 = 10
		let x = a + 5
	`, false)
	decl := res.program.Statements[1].(*ast.VarDeclStmt)
	typ, ok := res.typeTable.Get(decl.Value)
	if !ok {
		t.Fatal("expected type table entry for binary expr")
	}
	if got := typ.String(); got != "i64" {
		t.Errorf("expected i64, got %s", got)
	}
}

func TestTypeCheck_BinaryExpr_TypeTable_ConcreteFloat_WinsOverUntyped(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let a: f32 = 1.5
		let x = a + 2.0
	`, false)
	decl := res.program.Statements[1].(*ast.VarDeclStmt)
	typ, ok := res.typeTable.Get(decl.Value)
	if !ok {
		t.Fatal("expected type table entry")
	}
	if got := typ.String(); got != "f32" {
		t.Errorf("expected f32, got %s", got)
	}
}

// all arithmetic operators work

func TestTypeCheck_BinaryExpr_AllOps_Valid(t *testing.T) {
	for _, op := range []string{"+", "-", "*", "/", "%"} {
		src := "let x = 1 " + op + " 2"
		res := parseCollectAndCheck(t, src, false)
		assertNoErrors(t, res)
	}
}

// division/modulo by literal zero

func TestTypeCheck_BinaryExpr_DivByLiteralZero_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `let x = 5 / 0`, false)
	assertErrorsAre(t, res, "operator /: division by zero")
}

func TestTypeCheck_BinaryExpr_ModByLiteralZero_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `let x = 5 % 0`, false)
	assertErrorsAre(t, res, "operator %: division by zero")
}

func TestTypeCheck_BinaryExpr_RemainderByLiteralZero_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `let x = 5 %% 0`, false)
	assertErrorsAre(t, res, "operator %%: division by zero")
}

func TestTypeCheck_BinaryExpr_DivByLiteralZeroFloat_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `let x = 5.0 / 0.0`, false)
	assertErrorsAre(t, res, "operator /: division by zero")
}

func TestTypeCheck_BinaryExpr_DivByFoldedZero_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `let x = 10 / (5 - 5)`, false)
	assertErrorsAre(t, res, "operator /: division by zero")
}

func TestTypeCheck_BinaryExpr_ModByFoldedZero_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `let x = 10 % (2 * 0)`, false)
	assertErrorsAre(t, res, "operator %: division by zero")
}

func TestTypeCheck_BinaryExpr_DivByFoldedNonZero_NoError(t *testing.T) {
	res := parseCollectAndCheck(t, `let x = 10 / (5 - 3)`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_BinaryExpr_DivByNonZeroLiteral_NoError(t *testing.T) {
	res := parseCollectAndCheck(t, `let x = 10 / 2`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_BinaryExpr_DivByVariable_NoError(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let d: i64 = 0
		let x = 10 / d
	`, false)
	assertNoErrors(t, res)
}
