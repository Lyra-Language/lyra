package typechecker_test

import (
	"testing"

	"github.com/Lyra-Language/lyra/pkg/ast"
)

// --- no annotation: no errors ---

func TestTypeCheck_NoAnnotation_IntLiteral(t *testing.T) {
	res := parseCollectAndCheck(t, `let x = 42`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_NoAnnotation_FloatLiteral(t *testing.T) {
	res := parseCollectAndCheck(t, `let x = 3.14`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_NoAnnotation_StringLiteral(t *testing.T) {
	res := parseCollectAndCheck(t, `let x = "hello"`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_NoAnnotation_BoolLiteral(t *testing.T) {
	res := parseCollectAndCheck(t, `let x = true`, false)
	assertNoErrors(t, res)
}

// --- annotation matches literal: no errors ---

func TestTypeCheck_IntAnnotation_IntLiteral(t *testing.T) {
	res := parseCollectAndCheck(t, `let x: int = 42`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_I32Annotation_IntLiteral(t *testing.T) {
	// Untyped integer literal is assignable to any concrete integer type.
	res := parseCollectAndCheck(t, `let x: i32 = 42`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_U64Annotation_IntLiteral(t *testing.T) {
	res := parseCollectAndCheck(t, `let x: u64 = 100`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_F32Annotation_FloatLiteral(t *testing.T) {
	res := parseCollectAndCheck(t, `let x: f32 = 3.14`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_F64Annotation_FloatLiteral(t *testing.T) {
	res := parseCollectAndCheck(t, `let x: f64 = 2.718`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_StringAnnotation_StringLiteral(t *testing.T) {
	res := parseCollectAndCheck(t, `let x: string = "hello"`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_BoolAnnotation_BoolLiteral(t *testing.T) {
	res := parseCollectAndCheck(t, `let x: bool = true`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_F64Annotation_IntLiteral(t *testing.T) {
	// Integer literal is assignable to a float type without an explicit cast.
	res := parseCollectAndCheck(t, `let x: f64 = 42`, false)
	assertNoErrors(t, res)
}

// --- annotation mismatch: one error ---

func TestTypeCheck_StringAnnotation_IntLiteral(t *testing.T) {
	res := parseCollectAndCheck(t, `let x: string = 42`, false)
	assertErrorCount(t, res, 1)
	assertErrorContains(t, res, "cannot assign integer literal to string")
}

func TestTypeCheck_IntAnnotation_StringLiteral(t *testing.T) {
	res := parseCollectAndCheck(t, `let x: int = "hello"`, false)
	assertErrorCount(t, res, 1)
	assertErrorContains(t, res, "cannot assign string to int")
}

func TestTypeCheck_BoolAnnotation_IntLiteral(t *testing.T) {
	res := parseCollectAndCheck(t, `let x: bool = 1`, false)
	assertErrorCount(t, res, 1)
	assertErrorContains(t, res, "cannot assign integer literal to bool")
}

func TestTypeCheck_IntAnnotation_FloatLiteral(t *testing.T) {
	// float literal is not assignable to an integer type.
	res := parseCollectAndCheck(t, `let x: int = 3.14`, false)
	assertErrorCount(t, res, 1)
	assertErrorContains(t, res, "cannot assign float literal to int")
}

func TestTypeCheck_UnsignedIntAnnotation_NegatedLiteral(t *testing.T) {
	res := parseCollectAndCheck(t, `let x: uint = -3`, false)
	assertErrorCount(t, res, 1)
	assertErrorContains(t, res, "cannot assign integer literal to uint")
}

// --- signed to unsigned mismatch: one error ---

func TestTypeCheck_UnsignedIntAnnotation_SignedIntDeclarationUnannotated(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let a = 3
		let b: uint = a
	`, false)
	assertErrorCount(t, res, 1)
	assertErrorContains(t, res, "cannot assign int to uint")
}

func TestTypeCheck_UnsignedIntAnnotation_SignedIntDeclarationAnnotated(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let a: int = 3
		let b: uint = a
	`, false)
	assertErrorCount(t, res, 1)
	assertErrorContains(t, res, "cannot assign int to uint")
}

func TestTypeCheck_UnsignedIntAnnotation_ConcreteSignedIntDeclaration(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let a: i32 = 3
		let b: uint = a
	`, false)
	assertErrorCount(t, res, 1)
	assertErrorContains(t, res, "cannot assign i32 to uint")
}

func TestTypeCheck_StringAnnotation_BoolLiteral(t *testing.T) {
	res := parseCollectAndCheck(t, `let x: string = false`, false)
	assertErrorCount(t, res, 1)
	assertErrorContains(t, res, "cannot assign boolean to string")
}

// --- multiple declarations: each checked independently ---

func TestTypeCheck_MultipleDecls_AllValid(t *testing.T) {
	res := parseCollectAndCheck(t, `
	let a: int = 1
	let b: string = "hi"
	let c: bool = true
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_MultipleDecls_OneError(t *testing.T) {
	res := parseCollectAndCheck(t, `
	let a: int = 1
	let b: string = 99
	let c: bool = true
	`, false)
	assertErrorCount(t, res, 1)
	assertErrorContains(t, res, "b")
}

func TestTypeCheck_MultipleDecls_TwoErrors(t *testing.T) {
	res := parseCollectAndCheck(t, `
	let a: string = 1
	let b: int = "oops"
	`, false)
	assertErrorCount(t, res, 2)
}

// --- type recorded in TypeTable ---

// typeTableEntryForFirstDecl parses source, type-checks it, and returns the
// TypeTable entry for the initializer of the first statement (which must be a
// VarDeclStmt). Fails the test if anything is missing.
func typeTableEntryForFirstDecl(t *testing.T, source string) string {
	t.Helper()
	res := parseCollectAndCheck(t, source, false)
	if len(res.program.Statements) == 0 {
		t.Fatal("expected at least one statement")
	}
	decl, ok := res.program.Statements[0].(*ast.VarDeclStmt)
	if !ok {
		t.Fatal("expected VarDeclStmt as first statement")
	}
	typ, ok := res.typeTable.Get(decl.Value)
	if !ok {
		t.Fatal("expected type table entry for initializer")
	}
	return typ.String()
}

// Without an annotation the literal is promoted to its default concrete type.

func TestTypeCheck_TypeTable_NoAnnotation_IntLiteral(t *testing.T) {
	if got := typeTableEntryForFirstDecl(t, `let x = 42`); got != "int" {
		t.Errorf("expected int, got %s", got)
	}
}

func TestTypeCheck_TypeTable_NoAnnotation_FloatLiteral(t *testing.T) {
	if got := typeTableEntryForFirstDecl(t, `let x = 3.14`); got != "f64" {
		t.Errorf("expected f64, got %s", got)
	}
}

func TestTypeCheck_TypeTable_NoAnnotation_StringLiteral(t *testing.T) {
	if got := typeTableEntryForFirstDecl(t, `let x = "hello"`); got != "string" {
		t.Errorf("expected string, got %s", got)
	}
}

func TestTypeCheck_TypeTable_NoAnnotation_BoolLiteral(t *testing.T) {
	if got := typeTableEntryForFirstDecl(t, `let x = true`); got != "boolean" {
		t.Errorf("expected boolean, got %s", got)
	}
}

// With a compatible annotation the annotation type (not the raw literal type) is stored,
// so downstream passes see the concrete type the value is used as.

func TestTypeCheck_TypeTable_I32Annotation_IntLiteral(t *testing.T) {
	if got := typeTableEntryForFirstDecl(t, `let x: i32 = 42`); got != "i32" {
		t.Errorf("expected i32, got %s", got)
	}
}

func TestTypeCheck_TypeTable_I64Annotation_IntLiteral(t *testing.T) {
	if got := typeTableEntryForFirstDecl(t, `let x: i64 = 42`); got != "i64" {
		t.Errorf("expected i64, got %s", got)
	}
}

func TestTypeCheck_TypeTable_U8Annotation_IntLiteral(t *testing.T) {
	if got := typeTableEntryForFirstDecl(t, `let x: u8 = 200`); got != "u8" {
		t.Errorf("expected u8, got %s", got)
	}
}

func TestTypeCheck_TypeTable_U64Annotation_IntLiteral(t *testing.T) {
	if got := typeTableEntryForFirstDecl(t, `let x: u64 = 0`); got != "u64" {
		t.Errorf("expected u64, got %s", got)
	}
}

func TestTypeCheck_TypeTable_F32Annotation_FloatLiteral(t *testing.T) {
	if got := typeTableEntryForFirstDecl(t, `let x: f32 = 1.5`); got != "f32" {
		t.Errorf("expected f32, got %s", got)
	}
}

func TestTypeCheck_TypeTable_F64Annotation_FloatLiteral(t *testing.T) {
	if got := typeTableEntryForFirstDecl(t, `let x: f64 = 2.718`); got != "f64" {
		t.Errorf("expected f64, got %s", got)
	}
}

func TestTypeCheck_TypeTable_StringAnnotation_StringLiteral(t *testing.T) {
	if got := typeTableEntryForFirstDecl(t, `let x: string = "hi"`); got != "string" {
		t.Errorf("expected string, got %s", got)
	}
}

func TestTypeCheck_TypeTable_BoolAnnotation_BoolLiteral(t *testing.T) {
	if got := typeTableEntryForFirstDecl(t, `let x: boolean = false`); got != "boolean" {
		t.Errorf("expected boolean, got %s", got)
	}
}

// On a type mismatch the raw untyped literal type is stored (not the annotation),
// since the annotation cannot be applied to an incompatible value.

func TestTypeCheck_TypeTable_Mismatch_StoresInferredType(t *testing.T) {
	if got := typeTableEntryForFirstDecl(t, `let x: string = 42`); got != "integer literal" {
		t.Errorf("expected inferred integer literal on mismatch, got %s", got)
	}
}

func TestTypeCheck_TypeTable_Mismatch_FloatToInt_StoresInferredType(t *testing.T) {
	if got := typeTableEntryForFirstDecl(t, `let x: int = 3.14`); got != "float literal" {
		t.Errorf("expected inferred float literal on mismatch, got %s", got)
	}
}
