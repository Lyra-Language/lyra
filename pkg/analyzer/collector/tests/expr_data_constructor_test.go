package collector_test

import "testing"

// A bare nullary constructor (`None`, `Red`) used as a value collects to a
// DataConstructorExpr with a nil Value. The applied form uses call syntax
// (`Some(42)`) and collects as a named TupleLiteralExpr — see
// literal_tuple_test.go — which the typechecker resolves to its data type.
func TestCollect_NullaryDataConstructorExpr(t *testing.T) {
	source := `let v = None`
	runGoldenTest(t, source, "expr_data_constructor_nullary")
}
