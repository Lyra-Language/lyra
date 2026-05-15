package typechecker_test

import (
	"testing"
)

// No errors
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

func TestTypeCheck_BoolAndBinaryExpr_OneNonBoolLiteralOnLeft(t *testing.T) {
	res := parseCollectAndCheck(t, `let x = "true" && true`, false)
	assertErrorCount(t, res, 1)
	assertErrorContains(t, res, "expected left expression to be bool, got string instead")
}

func TestTypeCheck_BoolAndBinaryExpr_OneNonBoolLiteralOnRight(t *testing.T) {
	res := parseCollectAndCheck(t, `let x = true && 4`, false)
	assertErrorCount(t, res, 1)
	assertErrorContains(t, res, "expected right expression to be bool, got integer literal instead")
}
