package collector_test

import (
	"path/filepath"
	"testing"
)

func TestCollector_SimpleFunctionDefinition(t *testing.T) {
	source := `pub def sum<int>: (int, int) -> int = (a, b) => a + b`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "simple_function_definition.golden"))
}

func TestCollector_FunctionDefinitionWithGenericParams(t *testing.T) {
	source := `pub def sum<t>: (t, t) -> t = (a, b) => a + b`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "function_definition_with_generic_params.golden"))
}

func TestCollector_FunctionDefinitionWithMultipleClausesAndGuard(t *testing.T) {
	source := `
		def fib: (int) -> int = {
			(n) if n < 2 => n,
			(n) => fib(n-2) + fib(n-1),
		}
	`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "function_definition_with_multiple_clauses_and_guard.golden"))
}

func TestCollector_FunctionDefinitionWithModifiedParameters(t *testing.T) {
	source := `def add: (mut Point, ref Vec3) -> Point = (point, vec) => point + vec`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "function_definition_with_modified_parameters.golden"))
}

func TestCollector_FunctionDefinitionWithDefaultParameters(t *testing.T) {
	source := `def add: (int, int) -> int = (a, b = 0) => a + b`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "function_definition_with_default_parameters.golden"))
}
