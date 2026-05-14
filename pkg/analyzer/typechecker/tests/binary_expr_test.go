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
	`)
	assertNoErrors(t, res)
}

func TestTypeCheck_Ident_RefersToUnannotatedDecl(t *testing.T) {
	// x has no annotation; TypeTable records "int". y: int = x should pass.
	res := parseCollectAndCheck(t, `
		let x = 42
		let y: int = x
	`)
	assertNoErrors(t, res)
}

func TestTypeCheck_Ident_MismatchViaReference(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let x: string = "hi"
		let y: int = x
	`)
	assertErrorCount(t, res, 1)
	assertErrorContains(t, res, "cannot assign string to int")
}

// --- binary expression inference (#1) ---

// no-annotation: result type is inferred from operands

func TestTypeCheck_BinaryExpr_IntLiterals_NoAnnotation(t *testing.T) {
	res := parseCollectAndCheck(t, `let x = 1 + 2`)
	assertNoErrors(t, res)
}

func TestTypeCheck_BinaryExpr_FloatLiterals_NoAnnotation(t *testing.T) {
	res := parseCollectAndCheck(t, `let x = 1.0 + 2.0`)
	assertNoErrors(t, res)
}

// annotation compatible with binary result

func TestTypeCheck_BinaryExpr_I32Annotation_IntLiterals(t *testing.T) {
	res := parseCollectAndCheck(t, `let x: i32 = 1 + 2`)
	assertNoErrors(t, res)
}

func TestTypeCheck_BinaryExpr_F64Annotation_FloatLiterals(t *testing.T) {
	res := parseCollectAndCheck(t, `let x: f64 = 1.0 + 2.0`)
	assertNoErrors(t, res)
}

// annotation mismatch with binary result

func TestTypeCheck_BinaryExpr_StringAnnotation_IntAddition(t *testing.T) {
	res := parseCollectAndCheck(t, `let x: string = 1 + 2`)
	assertErrorCount(t, res, 1)
	assertErrorContains(t, res, "cannot assign")
}

func TestTypeCheck_BinaryExpr_IntAnnotation_FloatAddition(t *testing.T) {
	res := parseCollectAndCheck(t, `let x: int = 1.0 + 2.0`)
	assertErrorCount(t, res, 1)
	assertErrorContains(t, res, "cannot assign float literal to int")
}

// operand type errors

func TestTypeCheck_BinaryExpr_NonNumericOperand(t *testing.T) {
	// flag is bool (non-numeric); adding it to an int should error.
	res := parseCollectAndCheck(t, `
		let flag: bool = true
		let x = flag + 1
	`)
	assertErrorCount(t, res, 1)
	assertErrorContains(t, res, "operands must be numeric")
}

func TestTypeCheck_BinaryExpr_MixedIntFloat(t *testing.T) {
	res := parseCollectAndCheck(t, `let x = 1 + 1.0`)
	assertErrorCount(t, res, 1)
	assertErrorContains(t, res, "incompatible types")
}

// TypeTable records the result type

func TestTypeCheck_BinaryExpr_TypeTable_UntypedInt(t *testing.T) {
	res := parseCollectAndCheck(t, `let x = 1 + 2`)
	decl := res.program.Statements[0].(*ast.VarDeclStmt)
	typ, ok := res.typeTable.Get(decl.Value)
	if !ok {
		t.Fatal("expected type table entry for binary expr")
	}
	if got := typ.String(); got != "int" {
		t.Errorf("expected int, got %s", got)
	}
}

func TestTypeCheck_BinaryExpr_TypeTable_ConcreteInt_WinsOverUntyped(t *testing.T) {
	// untyped int + i64 → i64
	res := parseCollectAndCheck(t, `
		let a: i64 = 10
		let x = a + 5
	`)
	decl := res.program.Statements[1].(*ast.VarDeclStmt)
	typ, ok := res.typeTable.Get(decl.Value)
	if !ok {
		t.Fatal("expected type table entry for binary expr")
	}
	if got := typ.String(); got != "i64" {
		t.Errorf("expected i64, got %s", got)
	}
}

// all arithmetic operators work

func TestTypeCheck_BinaryExpr_AllOps_Valid(t *testing.T) {
	for _, op := range []string{"+", "-", "*", "/", "%"} {
		src := "let x = 1 " + op + " 2"
		res := parseCollectAndCheck(t, src)
		assertNoErrors(t, res)
	}
}
