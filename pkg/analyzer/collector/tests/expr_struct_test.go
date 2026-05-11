package collector_test

import (
	"path/filepath"
	"testing"
)

func TestCollector_AnonymousStructInstance(t *testing.T) {
	source := `let point = { x: 1, y: 2 }`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "anonymous_struct_instance.golden"))
}

func TestCollector_StructInstance(t *testing.T) {
	source := `let point = Point { x: 1, y: 2 }`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "struct_instance.golden"))
}

func TestCollector_StructInstanceShorthand(t *testing.T) {
	source := `let point = Point { 1, 2 }`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "struct_instance_shorthand.golden"))
}

func TestCollector_StructInstanceWithGenericArguments(t *testing.T) {
	source := `let point = Point::<i32> { x: 1, y: 2 }`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "struct_instance_with_generic_arguments.golden"))
}

func TestCollector_StructShorthandWithMultipleSpreadValues(t *testing.T) {
	source := `let merged = Point { ...base, ...extra }`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "struct_shorthand_with_multiple_spread_values.golden"))
}

func TestCollector_StructWithGenericsAndSpreadFieldValues(t *testing.T) {
	source := `let p = Point::<i32> { x: ...xs }`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "struct_with_generics_and_spread_field_values.golden"))
}

func TestCollector_RecordUpdateSingleField(t *testing.T) {
	source := `Player { existingPlayer | health: newHealth }`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "record_update_single_field.golden"))
}

func TestCollector_RecordUpdateMultipleFields(t *testing.T) {
	source := `Player { existingPlayer | health: newHealth, stamina: 100 }`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "record_update_multiple_fields.golden"))
}

func TestCollector_RecordUpdateWithExpressionFields(t *testing.T) {
	source := `let p = { player | health: player.health - 10, x: player.x + dx }`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "record_update_with_expression_fields.golden"))
}
