package typechecker_test

import (
	"testing"

	"github.com/Lyra-Language/lyra/pkg/ast"
)

// An interpolated string is always a `string`, and each interpolated segment is
// type-checked as a printable scalar (the same set print/println accept).

func TestInterpolation_TypeIsString(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let name: string = "Ada"
		let s = "hi ${name}"`, false)
	assertNoErrors(t, res)
	decl := res.program.Statements[1].(*ast.VarDeclStmt)
	typ, ok := res.typeTable.Get(decl.Value)
	if !ok {
		t.Fatal("expected a recorded type for the interpolated string")
	}
	if got := typ.String(); got != "string" {
		t.Errorf("interpolated string type = %s, want string", got)
	}
}

func TestInterpolation_PrintableSegments_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let name: string = "Ada"
		let n: i32 = 42
		let pi: f64 = 3.14
		let flag: bool = true
		let ch: rune = 'x'
		let s = "${name} ${n} ${pi} ${flag} ${ch}"`, false)
	assertNoErrors(t, res)
}

// An interpolation expression is a real expression: an undefined name inside
// `${…}` is reported (before this landed, segments went entirely unchecked).
func TestInterpolation_UndefinedIdentifier_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `let s = "hi ${missing}"`, false)
	assertErrorsAre(t, res, `undefined identifier "missing"`)
}

// A non-printable value (an aggregate — no Show/Display trait yet) can't be
// interpolated.
func TestInterpolation_NonPrintableSegment_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let p: (i32, i32) = (1, 2)
		let s = "point ${p}"`, false)
	assertErrorsAre(t, res,
		"cannot interpolate a value of type AnonymousTuple(i32, i32) (expected a string, an integer, a float, bool, or rune)")
}

// An untyped numeric-literal segment is settled to its default width so the
// backend reads a concrete type off it (the analogue of print's behavior).
func TestInterpolation_UntypedLiteralSegment_SettlesToDefault(t *testing.T) {
	res := parseCollectAndCheck(t, `let s = "n=${7}"`, false)
	assertNoErrors(t, res)
	interp := res.program.Statements[0].(*ast.VarDeclStmt).Value.(*ast.InterpolatedStringExpr)
	// Segments: StringLiteralExpr("n="), IntegerLiteralExpr(7).
	var lit *ast.IntegerLiteralExpr
	for _, seg := range interp.Segments {
		if il, ok := seg.(*ast.IntegerLiteralExpr); ok {
			lit = il
		}
	}
	if lit == nil {
		t.Fatal("expected an integer-literal segment")
	}
	typ, ok := res.typeTable.Get(lit)
	if !ok {
		t.Fatal("expected a recorded type for the literal segment")
	}
	if got := typ.String(); got != "i64" {
		t.Errorf("untyped literal segment settled to %s, want i64", got)
	}
}
