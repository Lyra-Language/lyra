package collector

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
	program, _ := parseAndCollectFull(t, source, true)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "struct_instance_with_generic_arguments.golden"))
}
