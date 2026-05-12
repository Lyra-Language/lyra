package collector_test

import "testing"

func TestCollector_AnonymousStructInstance(t *testing.T) {
	runGoldenTest(t, `let point = { x: 1, y: 2 }`, "anonymous_struct_instance")
}

func TestCollector_StructInstance(t *testing.T) {
	runGoldenTest(t, `let point = Point { x: 1, y: 2 }`, "struct_instance")
}

func TestCollector_StructInstanceShorthand(t *testing.T) {
	runGoldenTest(t, `let point = Point { 1, 2 }`, "struct_instance_shorthand")
}

func TestCollector_StructInstanceWithGenericArguments(t *testing.T) {
	runGoldenTest(t, `let point = Point::<i32> { x: 1, y: 2 }`, "struct_instance_with_generic_arguments")
}

func TestCollector_StructShorthandWithMultipleSpreadValues(t *testing.T) {
	runGoldenTest(t, `let merged = Point { ...base, ...extra }`, "struct_shorthand_with_multiple_spread_values")
}

func TestCollector_StructWithGenericsAndSpreadFieldValues(t *testing.T) {
	runGoldenTest(t, `let p = Point::<i32> { x: ...xs }`, "struct_with_generics_and_spread_field_values")
}

func TestCollector_RecordUpdateSingleField(t *testing.T) {
	runGoldenTest(t, `Player { existingPlayer | health: newHealth }`, "record_update_single_field")
}

func TestCollector_RecordUpdateMultipleFields(t *testing.T) {
	runGoldenTest(t, `Player { existingPlayer | health: newHealth, stamina: 100 }`, "record_update_multiple_fields")
}

func TestCollector_RecordUpdateWithExpressionFields(t *testing.T) {
	runGoldenTest(t, `let p = { player | health: player.health - 10, x: player.x + dx }`, "record_update_with_expression_fields")
}
