package collector_test

import "testing"

func TestCollector_TupleTypeDeclaration(t *testing.T) {
	runGoldenTest(t, `tuple Point2D(float, float)`, "tuple_type_declaration")
}

func TestCollector_TupleTypeDeclarationWithGenericParameters(t *testing.T) {
	runGoldenTest(t, `tuple Point2D<t>(t, t)`, "tuple_type_declaration_with_generic_parameters")
}

func TestCollector_TupleTypeDeclarationWithGenericParamConstraints(t *testing.T) {
	runGoldenTest(t, `tuple Pair<a: Numeric, b: Ordered>(a, b)`, "tuple_type_declaration_with_generic_param_constraints")
}

func TestCollector_TupleTypeDeclarationWithVisibility(t *testing.T) {
	runGoldenTest(t, `pub tuple Point2D(float, float)`, "tuple_type_declaration_with_visibility")
}

func TestCollector_TupleTypeDeclarationWithAllocationModifier(t *testing.T) {
	runGoldenTest(t, `pub stack tuple Point2D(float, float)`, "tuple_type_declaration_with_allocation_modifier")
}

func TestCollector_TupleTypeDeclarationWithNestedTuple(t *testing.T) {
	runGoldenTest(t, `tuple RGBA((int, int, int), float)`, "tuple_type_declaration_with_nested_tuple")
}
