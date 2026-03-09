package collector

import (
	"path/filepath"
	"testing"
)

func TestCollector_TupleTypeDeclaration(t *testing.T) {
	source := `tuple Point2D(float, float)`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "tuple_type_declaration.golden"))
}

func TestCollector_TupleTypeDeclarationWithGenericParameters(t *testing.T) {
	source := `tuple Point2D<t>(t, t)`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "tuple_type_declaration_with_generic_parameters.golden"))
}

func TestCollector_TupleTypeDeclarationWithVisibility(t *testing.T) {
	source := `pub tuple Point2D(float, float)`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "tuple_type_declaration_with_visibility.golden"))
}

func TestCollector_TupleTypeDeclarationWithAllocationModifier(t *testing.T) {
	source := `pub stack tuple Point2D(float, float)`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "tuple_type_declaration_with_allocation_modifier.golden"))
}

func TestCollector_TupleTypeDeclarationWithNestedTuple(t *testing.T) {
	source := `tuple RGBA((int, int, int), float)`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "tuple_type_declaration_with_nested_tuple.golden"))
}
