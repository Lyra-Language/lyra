package typechecker_test

import (
	"testing"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// --- valid concatenation ---

func TestStringConcat(t *testing.T) {
	res := parseCollectAndCheck(t, `let str = "hello" ++ "world"`, false)
	assertNoErrors(t, res)
}

func TestStringConcat_StringAnnotation(t *testing.T) {
	res := parseCollectAndCheck(t, `let s: string = "hello" ++ " world"`, false)
	assertNoErrors(t, res)
}

func TestStringConcat_InterpolatedLeft(t *testing.T) {
	// Interpolation segments are now type-checked, so the interpolated name must
	// be a declared, printable binding.
	res := parseCollectAndCheck(t, `
		let name: string = "Ada"
		let s = "hello ${name}" ++ " world"`, false)
	assertNoErrors(t, res)
}

func TestStringConcat_InterpolatedRight(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let name: string = "Ada"
		let s = "hello " ++ " ${name}"`, false)
	assertNoErrors(t, res)
}

func TestStringConcat_BothInterpolated(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let first: string = "Ada"
		let last: string = "Lovelace"
		let s = "hello ${first}" ++ " ${last}"`, false)
	assertNoErrors(t, res)
}

// --- type-table entry ---

func TestStringConcat_TypeTable_RecordsString(t *testing.T) {
	res := parseCollectAndCheck(t, `let s = "hello" ++ " world"`, false)
	assertNoErrors(t, res)
	decl := res.program.Statements[0].(*ast.VarDeclStmt)
	typ, ok := res.typeTable.Get(decl.Value)
	if !ok {
		t.Fatal("expected type table entry for string concat expr")
	}
	want := types.PrimitiveType{Name: types.String}
	if !types.TypesEqual(typ, want) {
		t.Errorf("expected %s, got %s", want, typ)
	}
}

// --- operand type errors ---

func TestStringConcat_NonStringLeft_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let n: i64 = 42
		let s = n ++ " world"
	`, false)
	assertErrorsAre(t, res, "operator ++: operands must be strings, got i64 and string")
}

func TestStringConcat_NonStringRight_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let n: i64 = 42
		let s = "hello" ++ n
	`, false)
	assertErrorsAre(t, res, "operator ++: operands must be strings, got string and i64")
}

func TestStringConcat_BothNonString_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let n: i64 = 42
		let b: bool = true
		let s = n ++ b
	`, false)
	assertErrorsAre(t, res, "operator ++: operands must be strings, got i64 and boolean")
}

func TestStringConcat_BoolVar_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let flag: bool = true
		let s = flag ++ "hello"
	`, false)
	assertErrorsAre(t, res, "operator ++: operands must be strings, got boolean and string")
}

// --- annotation mismatch ---

func TestStringConcat_WrongAnnotation_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `let s: i64 = "hello" ++ " world"`, false)
	assertErrorsAre(t, res, "s: cannot assign string to i64")
}
