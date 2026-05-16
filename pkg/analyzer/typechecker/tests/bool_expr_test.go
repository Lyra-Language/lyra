package typechecker_test

import (
	"testing"
)

// No errors
func TestTypeCheck_BoolNotExpr_BoolIdentifier(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let isTrue = true
		if !isTrue {
			return false
		}
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_BoolOrBinaryExpr_BoolIdentifiers(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let isTrue = true
		let isFalse = false
		isFalse || isTrue
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_BoolAndBinaryExpr_BoolIdentifiers(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let isTrue = true
		let isFalse = false
		isFalse && isTrue
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_BoolOrBinaryExpr_BoolIdentifierAndLiteral(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let isTrue = true
		false || isTrue
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_BoolAndBinaryExpr_BoolIdentifierAndLiteral(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let isTrue = true
		false && isTrue
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_BoolOrBinaryExpr_TwoBoolLiterals(t *testing.T) {
	res := parseCollectAndCheck(t, `
		false || true
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_BoolAndBinaryExpr_TwoBoolLiterals(t *testing.T) {
	res := parseCollectAndCheck(t, `
		false && true
	`, false)
	assertNoErrors(t, res)
}

// Errors
func TestTypeCheck_NotBooleanExpr_NonBool(t *testing.T) {
	res := parseCollectAndCheck(t, `let x = !3.1415`, false)
	assertErrorCount(t, res, 1)
	assertErrorContains(t, res, "not_boolean_expr: expected boolean, got float literal instead")
}

func TestTypeCheck_BoolAndBinaryExpr_OneNonBoolLiteralOnLeft(t *testing.T) {
	res := parseCollectAndCheck(t, `let x = "true" && true`, false)
	assertErrorCount(t, res, 1)
	assertErrorContains(t, res, "operator &&: operands must both be boolean, got string and boolean instead")
}

func TestTypeCheck_BoolAndBinaryExpr_OneNonBoolLiteralOnRight(t *testing.T) {
	res := parseCollectAndCheck(t, `let x = true && 4`, false)
	assertErrorCount(t, res, 1)
	assertErrorContains(t, res, "operator &&: operands must both be boolean, got boolean and integer literal instead")
}
