package collector_test

import "testing"

func TestCollector_SimpleAnonymousTupleLiteral(t *testing.T) {
	runGoldenTest(t, `let point: (int, int) = (1, 2)`, "simple_anonymous_tuple_literal")
}

func TestCollector_SimpleNamedTupleLiteral(t *testing.T) {
	runGoldenTest(t, `let point: Point = (1, 2)`, "simple_named_tuple_literal")
}

func TestCollector_SimpleNamedTupleLiteralWithGenericParameters(t *testing.T) {
	runGoldenTest(t, `let point = Point::<i32>(1, 2)`, "simple_named_tuple_literal_with_generic_parameters")
}
