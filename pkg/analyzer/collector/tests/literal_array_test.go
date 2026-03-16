package collector_test

import (
	"path/filepath"
	"testing"
)

func TestCollectSimpleStaticArrayLiteral(t *testing.T) {
	source := `let arr = [1, 2, 3]`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "simple_static_array_literal.golden"))
}

func TestCollectArrayLiteralWithExpression(t *testing.T) {
	source := `let arr = [1, 2 * PI, 3]`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "array_literal_with_expression.golden"))
}

func TestCollectArrayRepeatInitializationWithStructs(t *testing.T) {
	source := `let arr = [Vec3 { x: 0, y: 0, z: 0 }; 100]`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "array_repeat_initialization_with_structs.golden"))
}

func TestCollectSimpleDynamicArrayLiteral(t *testing.T) {
	source := `let arr: stack []int = [1, 2, 3]`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "simple_dynamic_array_literal.golden"))
}

func TestCollectArrayRepeatInitialization(t *testing.T) {
	source := `let arr = [0; 8]`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "array_repeat_initialization.golden"))
}

func TestCollectArrayRepeatInitializationWithCompileTimeConstantCount(t *testing.T) {
	source := `let arr = [0; SIZE]`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "array_repeat_initialization_with_compile_time_constant_count.golden"))
}
