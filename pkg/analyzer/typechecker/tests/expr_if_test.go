package typechecker_test

import (
	"testing"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// ── condition must be bool ───────────────────────────────────────────────────

func TestTypeCheck_If_Condition_BoolLiteral_NoError(t *testing.T) {
	res := parseCollectAndCheck(t, `if true { 1 }`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_If_Condition_BoolVar_NoError(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let flag: bool = true
		if flag { 1 }
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_If_Condition_BooleanExpr_NoError(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let x: int = 5
		if x == 3 { 1 }
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_If_Condition_IntVar_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let n: int = 42
		if n { 1 }
	`, false)
	assertErrorCount(t, res, 1)
	assertErrorIs(t, res, "if condition must be bool, got int")
}

func TestTypeCheck_If_Condition_StringVar_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let s: string = "hello"
		if s { 1 }
	`, false)
	assertErrorCount(t, res, 1)
	assertErrorIs(t, res, "if condition must be bool, got string")
}

// ── one-armed if: no branch-type requirement ─────────────────────────────────

func TestTypeCheck_If_OneArmed_NoError(t *testing.T) {
	// No else branch — the value is discarded; no compatibility required.
	res := parseCollectAndCheck(t, `if true { 1 }`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_If_OneArmed_MixedBodyTypes_NoError(t *testing.T) {
	// Even a block whose last expr is a string is fine with no else.
	res := parseCollectAndCheck(t, `
		let flag: bool = true
		if flag { "hello" }
	`, false)
	assertNoErrors(t, res)
}

// ── if/else branch compatibility ─────────────────────────────────────────────

func TestTypeCheck_IfElse_SameIntType_NoError(t *testing.T) {
	res := parseCollectAndCheck(t, `if true { 1 } else { 2 }`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_IfElse_SameStringType_NoError(t *testing.T) {
	res := parseCollectAndCheck(t, `if true { "a" } else { "b" }`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_IfElse_SameBoolType_NoError(t *testing.T) {
	res := parseCollectAndCheck(t, `if true { true } else { false }`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_IfElse_UntypedIntAndConcreteInt_NoError(t *testing.T) {
	// Untyped int literal widens to i32 — branches are compatible.
	res := parseCollectAndCheck(t, `
		let x: i32 = 1
		if true { x } else { 2 }
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_IfElse_IntAndString_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `if true { 1 } else { "hello" }`, false)
	assertErrorCount(t, res, 1)
	assertErrorIs(t, res, "if/else branches have incompatible types: then is integer literal, else is string")
}

func TestTypeCheck_IfElse_StringAndBool_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `if true { "a" } else { true }`, false)
	assertErrorCount(t, res, 1)
	assertErrorIs(t, res, "if/else branches have incompatible types: then is string, else is boolean")
}

func TestTypeCheck_IfElse_IntAndBool_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `if true { 1 } else { false }`, false)
	assertErrorCount(t, res, 1)
	assertErrorIs(t, res, "if/else branches have incompatible types: then is integer literal, else is boolean")
}

// ── if/else as a value ───────────────────────────────────────────────────────

func TestTypeCheck_IfElse_AsValue_StringAnnotation_NoError(t *testing.T) {
	res := parseCollectAndCheck(t, `let s: string = if true { "yes" } else { "no" }`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_IfElse_AsValue_IntAnnotation_NoError(t *testing.T) {
	res := parseCollectAndCheck(t, `let n: int = if true { 1 } else { 2 }`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_IfElse_AsValue_WrongAnnotation_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `let n: int = if true { "yes" } else { "no" }`, false)
	assertErrorCount(t, res, 1)
	assertErrorIs(t, res, "n: cannot assign string to int")
}

func TestTypeCheck_IfElse_AsValue_IncompatibleBranches_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `let x = if true { 1 } else { "oops" }`, false)
	assertErrorCount(t, res, 1)
	assertErrorIs(t, res, "if/else branches have incompatible types: then is integer literal, else is string")
}

// ── type-table entry ─────────────────────────────────────────────────────────

func TestTypeCheck_IfElse_TypeTable_RecordsCommonType(t *testing.T) {
	res := parseCollectAndCheck(t, `let s = if true { "yes" } else { "no" }`, false)
	assertNoErrors(t, res)
	decl := res.program.Statements[0].(*ast.VarDeclStmt)
	typ, ok := res.typeTable.Get(decl.Value)
	if !ok {
		t.Fatal("expected type table entry for if/else expression")
	}
	want := types.PrimitiveType{Name: types.String}
	if !types.TypesEqual(typ, want) {
		t.Errorf("expected %s, got %s", want, typ)
	}
}

// ── else-if chains (more than 2 branches) ──────────────────────────────────

func TestTypeCheck_ElseIf_AllCompatible_NoError(t *testing.T) {
	// if { string } else if { string } else { string } → ok
	res := parseCollectAndCheck(t, `
		let x: int = 1
		if x == 1 { "one" } else if x == 2 { "two" } else { "other" }
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_ElseIf_AsValue_AllCompatible_NoError(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let x: int = 1
		let s: string = if x == 1 { "one" } else if x == 2 { "two" } else { "other" }
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_ElseIf_AsValue_WrongAnnotation_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let x: int = 1
		let n: int = if x == 1 { "one" } else if x == 2 { "two" } else { "other" }
	`, false)
	assertErrorCount(t, res, 1)
	assertErrorIs(t, res, "n: cannot assign string to int")
}

func TestTypeCheck_ElseIf_InnerBranchMismatch_Error(t *testing.T) {
	// The else-if and final-else branches disagree; the outer then-branch is fine.
	// Only one error is reported (from the inner pair); the outer check is skipped
	// because the inner returns nil on error.
	res := parseCollectAndCheck(t, `
		if true { 1 } else if false { 2 } else { "three" }
	`, false)
	assertErrorCount(t, res, 1)
	assertErrorIs(t, res, "if/else branches have incompatible types: then is integer literal, else is string")
}

func TestTypeCheck_ElseIf_MiddleBranchMismatch_Error(t *testing.T) {
	// The else-if then-branch is string while both outer branches are int.
	res := parseCollectAndCheck(t, `
		if true { 1 } else if false { "two" } else { 3 }
	`, false)
	assertErrorCount(t, res, 1)
	assertErrorIs(t, res, "if/else branches have incompatible types: then is string, else is integer literal")
}

func TestTypeCheck_ElseIf_BadCondition_Error(t *testing.T) {
	// Non-bool condition inside the else-if clause.
	res := parseCollectAndCheck(t, `
		let n: int = 5
		if true { 1 } else if n { 2 } else { 3 }
	`, false)
	assertErrorCount(t, res, 1)
	assertErrorIs(t, res, "if condition must be bool, got int")
}

// ── condition and branch errors can co-occur ─────────────────────────────────

func TestTypeCheck_IfElse_BadConditionAndBranches_BothErrors(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let n: int = 5
		if n { 1 } else { "oops" }
	`, false)
	assertErrorCount(t, res, 2)
	assertErrorIs(t, res, "if condition must be bool, got int")
	assertErrorIs(t, res, "if/else branches have incompatible types: then is integer literal, else is string")
}
