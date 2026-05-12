package collector_test

import "testing"

func TestCollectSimpleStaticArrayLiteral(t *testing.T) {
	runGoldenTest(t, `let arr = [1, 2, 3]`, "simple_static_array_literal")
}

func TestCollectArrayLiteralWithExpression(t *testing.T) {
	runGoldenTest(t, `let arr = [1, 2 * PI, 3]`, "array_literal_with_expression")
}

func TestCollectArrayRepeatInitializationWithStructs(t *testing.T) {
	runGoldenTest(t, `let arr = [Vec3 { x: 0, y: 0, z: 0 }; 100]`, "array_repeat_initialization_with_structs")
}

func TestCollectSimpleDynamicArrayLiteral(t *testing.T) {
	runGoldenTest(t, `let arr: stack []int = [1, 2, 3]`, "simple_dynamic_array_literal")
}

func TestCollectArrayRepeatInitialization(t *testing.T) {
	runGoldenTest(t, `let arr = [0; 8]`, "array_repeat_initialization")
}

func TestCollectArrayRepeatInitializationWithCompileTimeConstantCount(t *testing.T) {
	runGoldenTest(t, `let arr = [0; SIZE]`, "array_repeat_initialization_with_compile_time_constant_count")
}
