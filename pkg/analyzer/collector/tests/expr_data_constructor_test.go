package collector_test

import "testing"

func TestCollect_DataConstructorExpr(t *testing.T) {
	source := `let wrapped = Some 42`
	runGoldenTest(t, source, "expr_data_constructor")
}

func TestCollect_DataConstructorExprWithCall(t *testing.T) {
	source := `let result = Ok compute()`
	runGoldenTest(t, source, "expr_data_constructor_with_call")
}

func TestCollect_DataConstructorExprWithStruct(t *testing.T) {
	source := `let v = Some Point { x: 1, y: 2 }`
	runGoldenTest(t, source, "expr_data_constructor_with_struct")
}

func TestCollect_DataConstructorExprWithIdentifier(t *testing.T) {
	source := `let v = Some user`
	runGoldenTest(t, source, "expr_data_constructor_with_identifier")
}

func TestCollect_DataConstructorExprWithNegation(t *testing.T) {
	source := `let v = Err -1`
	runGoldenTest(t, source, "expr_data_constructor_with_negation")
}

func TestCollect_DataConstructorExprWithTuple(t *testing.T) {
	source := `let v = Some Pair(1, 2)`
	runGoldenTest(t, source, "expr_data_constructor_with_tuple")
}
