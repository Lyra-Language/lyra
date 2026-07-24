package typechecker_test

import (
	"testing"

	"github.com/Lyra-Language/lyra/pkg/ast"
)

// --- valid negation: signed integer targets ---

func TestTypeCheck_Negation_IntAnnotation(t *testing.T) {
	res := parseCollectAndCheck(t, `let x: i64 = -42`, false)
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

// --- no annotation: promotes to i64 ---

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
	if got := typ.String(); got != "i64" {
		t.Errorf("expected i64, got %s", got)
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
	// -3 + 5: UntypedSignedInt + UntypedInt → UntypedSignedInt (promotes to i64)
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
		var x: i64 = 0
		x = -5
	`, false)
	assertNoErrors(t, res)
}

// --- error cases ---

func TestTypeCheck_Negation_UnsignedAnnotation_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `let x: u8 = -1`, false)
	assertErrorsAre(t, res, "x: cannot assign integer literal to u8")
}

func TestTypeCheck_Negation_ConcreteUnsignedVar_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let a: u32 = 5
		let b = -a
	`, false)
	assertErrorsAre(t, res, "cannot negate unsigned type u32")
}

func TestTypeCheck_Negation_NonNumeric_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let flag: bool = true
		let x = -flag
	`, false)
	assertErrorsAre(t, res, "cannot negate non-numeric type boolean")
}

// --- i64 min literal ---

// -9223372036854775808 is the minimum i64. Its magnitude (2^63) overflows int64
// as a positive literal (so the collector marks it Unsigned/u64), but negated it
// is a valid i64 — special-cased in inferNegationExpr.
func TestTypeCheck_Negation_I64Min_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `let x: i64 = -9223372036854775808`, false)
	assertNoErrors(t, res)
}

// The same value doesn't fit a narrower signed type.
func TestTypeCheck_Negation_I64Min_TooNarrow_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `let x: i32 = -9223372036854775808`, false)
	assertErrorsAre(t, res, "x: literal value -9223372036854775808 overflows i32")
}

// A magnitude larger than 2^63 negated is below i64 min — out of range for any
// signed integer.
func TestTypeCheck_Negation_BelowI64Min_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `let x: i64 = -18446744073709551615`, false)
	assertErrorsAre(t, res,
		"negated literal -18446744073709551615 is out of range for a signed integer (below the minimum i64, -9223372036854775808)")
}

// -(int64max) is unchanged: its magnitude fits int64, so it's an ordinary
// negated literal.
func TestTypeCheck_Negation_MaxNegated_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `let x: i64 = -9223372036854775807`, false)
	assertNoErrors(t, res)
}
