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
	res := parseCollectAndCheck(t, `let s = "hello ${name}" ++ " world"`, false)
	assertNoErrors(t, res)
}

func TestStringConcat_InterpolatedRight(t *testing.T) {
	res := parseCollectAndCheck(t, `let s = "hello " ++ " ${name}"`, false)
	assertNoErrors(t, res)
}

func TestStringConcat_BothInterpolated(t *testing.T) {
	res := parseCollectAndCheck(t, `let s = "hello ${first}" ++ " ${last}"`, false)
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
		let n: int = 42
		let s = n ++ " world"
	`, false)
	assertErrorCount(t, res, 1)
	assertErrorContains(t, res, "operator ++: operands must be strings")
}

func TestStringConcat_NonStringRight_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let n: int = 42
		let s = "hello" ++ n
	`, false)
	assertErrorCount(t, res, 1)
	assertErrorContains(t, res, "operator ++: operands must be strings")
}

func TestStringConcat_BothNonString_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let n: int = 42
		let b: bool = true
		let s = n ++ b
	`, false)
	assertErrorCount(t, res, 1)
	assertErrorContains(t, res, "operator ++: operands must be strings")
}

func TestStringConcat_BoolVar_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let flag: bool = true
		let s = flag ++ "hello"
	`, false)
	assertErrorCount(t, res, 1)
	assertErrorContains(t, res, "operator ++: operands must be strings")
}

// --- annotation mismatch ---

func TestStringConcat_WrongAnnotation_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `let s: int = "hello" ++ " world"`, false)
	assertErrorCount(t, res, 1)
	assertErrorContains(t, res, "cannot assign")
}
