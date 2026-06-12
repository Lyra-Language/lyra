package typechecker_test

import (
	"testing"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// ── condition must be bool ───────────────────────────────────────────────────

func TestTypeCheck_If_Condition_BoolLiteralTrue_Warning(t *testing.T) {
	res := parseCollectAndCheck(t, `if true { 1 }`, false)
	assertWarningsAre(t, res, "condition is always true")
}

func TestTypeCheck_If_Condition_BoolLiteralFalse_Warning(t *testing.T) {
	res := parseCollectAndCheck(t, `if false { 1 }`, false)
	assertWarningsAre(t, res, "condition is always false")
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
		let x: i64 = 5
		if x == 3 { 1 }
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_If_Condition_IntVar_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let n: i64 = 42
		if n { 1 }
	`, false)
	assertErrorsAre(t, res, "if condition must be boolean, got i64")
}

func TestTypeCheck_If_Condition_StringVar_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let s: string = "hello"
		if s { 1 }
	`, false)
	assertErrorsAre(t, res, "if condition must be boolean, got string")
}

// ── one-armed if: no branch-type requirement ─────────────────────────────────

func TestTypeCheck_If_OneArmed_NoError(t *testing.T) {
	// No else branch — the value is discarded; no compatibility required.
	res := parseCollectAndCheck(t, `
		let cond: bool = true
		if cond { 1 }
	`, false)
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
	res := parseCollectAndCheck(t, `
		let cond: bool = true
		if cond { 1 } else { 2 }
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_IfElse_SameStringType_NoError(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let cond: bool = true
		if cond { "a" } else { "b" }
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_IfElse_SameBoolType_NoError(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let cond: bool = true
		if cond { true } else { false }
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_IfElse_UntypedIntAndConcreteInt_NoError(t *testing.T) {
	// Untyped integer literal widens to i32 — branches are compatible.
	res := parseCollectAndCheck(t, `
		let x: i32 = 1
		let cond: bool = true
		if cond { x } else { 2 }
	`, false)
	assertNoErrors(t, res)
}

// Statement-position if/else: branch types never need to match.
func TestTypeCheck_IfElse_IntAndString_Statement_NoError(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let cond: bool = true
		if cond { 1 } else { "hello" }
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_IfElse_StringAndBool_Statement_NoError(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let cond: bool = true
		if cond { "a" } else { true }
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_IfElse_IntAndBool_Statement_NoError(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let cond: bool = true
		if cond { 1 } else { false }
	`, false)
	assertNoErrors(t, res)
}

// Value-position if/else: branch types must be compatible.
func TestTypeCheck_IfElse_IntAndString_Value_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let cond: bool = true
		let x = if cond { 1 } else { "hello" }
	`, false)
	assertErrorsAre(t, res, "if/else branches have incompatible types: then is integer literal, else is string")
}

func TestTypeCheck_IfElse_StringAndBool_Value_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let cond: bool = true
		let x = if cond { "a" } else { true }
	`, false)
	assertErrorsAre(t, res, "if/else branches have incompatible types: then is string, else is boolean")
}

// ── if/else as a value ───────────────────────────────────────────────────────

func TestTypeCheck_IfElse_AsValue_StringAnnotation_NoError(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let cond: bool = true
		let s: string = if cond { "yes" } else { "no" }
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_IfElse_AsValue_IntAnnotation_NoError(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let cond: bool = true
		let n: i64 = if cond { 1 } else { 2 }
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_IfElse_AsValue_WrongAnnotation_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let cond: bool = true
		let n: i64 = if cond { "yes" } else { "no" }
	`, false)
	assertErrorsAre(t, res, "n: cannot assign string to i64")
}

func TestTypeCheck_IfElse_AsValue_IncompatibleBranches_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let cond: bool = true
		let x = if cond { 1 } else { "oops" }
	`, false)
	assertErrorsAre(t, res, "if/else branches have incompatible types: then is integer literal, else is string")
}

// ── type-table entry ─────────────────────────────────────────────────────────

func TestTypeCheck_IfElse_TypeTable_RecordsCommonType(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let cond: bool = true
		let s = if cond { "yes" } else { "no" }
	`, false)
	assertNoErrors(t, res)
	decl := res.program.Statements[1].(*ast.VarDeclStmt)
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
		let x: i64 = 1
		if x == 1 { "one" } else if x == 2 { "two" } else { "other" }
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_ElseIf_AsValue_AllCompatible_NoError(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let x: i64 = 1
		let s: string = if x == 1 { "one" } else if x == 2 { "two" } else { "other" }
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_ElseIf_AsValue_WrongAnnotation_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let x: i64 = 1
		let n: i64 = if x == 1 { "one" } else if x == 2 { "two" } else { "other" }
	`, false)
	assertErrorsAre(t, res, "n: cannot assign string to i64")
}

// Statement-position else-if chains: no branch-type requirement at any level.
func TestTypeCheck_ElseIf_InnerBranchMismatch_Statement_NoError(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let cond: bool = true
		let cond2: bool = false
		if cond { 1 } else if cond2 { 2 } else { "three" }
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_ElseIf_MiddleBranchMismatch_Statement_NoError(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let cond: bool = true
		let cond2: bool = false
		if cond { 1 } else if cond2 { "two" } else { 3 }
	`, false)
	assertNoErrors(t, res)
}

// Value-position else-if chains: mismatch at any level still errors.
func TestTypeCheck_ElseIf_InnerBranchMismatch_Value_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let cond: bool = true
		let cond2: bool = false
		let x = if cond { 1 } else if cond2 { 2 } else { "three" }
	`, false)
	assertErrorsAre(t, res, "if/else branches have incompatible types: then is integer literal, else is string")
}

func TestTypeCheck_ElseIf_MiddleBranchMismatch_Value_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let cond: bool = true
		let cond2: bool = false
		let x = if cond { 1 } else if cond2 { "two" } else { 3 }
	`, false)
	assertErrorsAre(t, res, "if/else branches have incompatible types: then is string, else is integer literal")
}

func TestTypeCheck_ElseIf_BadCondition_Error(t *testing.T) {
	// Non-bool condition inside the else-if clause — always an error regardless of context.
	res := parseCollectAndCheck(t, `
		let n: i64 = 5
		let cond: bool = true
		if cond { 1 } else if n { 2 } else { 3 }
	`, false)
	assertErrorsAre(t, res, "if condition must be boolean, got i64")
}

// ── condition and branch errors ──────────────────────────────────────────────

func TestTypeCheck_IfElse_BadCondition_Statement_NoError(t *testing.T) {
	// Bad condition errors; branch mismatch in statement position does not.
	res := parseCollectAndCheck(t, `
		let n: i64 = 5
		if n { 1 } else { "oops" }
	`, false)
	assertErrorsAre(t, res, "if condition must be boolean, got i64")
}

func TestTypeCheck_IfElse_BadConditionAndBranches_Value_BothErrors(t *testing.T) {
	// In value context both errors fire.
	res := parseCollectAndCheck(t, `
		let n: i64 = 5
		let x = if n { 1 } else { "oops" }
	`, false)
	assertErrorsAre(t, res, "if condition must be boolean, got i64", "if/else branches have incompatible types: then is integer literal, else is string")
}
