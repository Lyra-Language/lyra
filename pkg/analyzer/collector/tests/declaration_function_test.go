package collector_test

import (
	"path/filepath"
	"testing"
)

func TestCollector_BasicFunctionDeclarationWithoutParams(t *testing.T) {
	source := `
	let foo = () => {
		// do stuff
	}`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "basic_function_declaration_without_params.golden"))
}

func TestCollector_BasicFunctionDeclarationWithVisibility(t *testing.T) {
	source := `
	pub let foo = () => {
		// do stuff
	}`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "basic_function_declaration_with_visibility.golden"))
}

func TestCollector_BasicFunctionDeclarationWithParams(t *testing.T) {
	source := `
	let add = (a: Int, b: String) => a + b`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "basic_function_declaration_with_params.golden"))
}

func TestCollector_DefaultParameterValues(t *testing.T) {
	source := `
	let greet = (name: string, prefix: string = "Hello") -> string => "${prefix} ${name}"`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "function_declaration_with_default_parameter_values.golden"))
}

func TestCollector_DefaultValueAsCallExpression(t *testing.T) {
	source := `
	let process = (items: []string, config: Config = get_default_config()) -> void => {}`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "function_declaration_with_default_value_as_call_expression.golden"))
}

func TestCollector_DefaultValueWithMultipleClauses(t *testing.T) {
	source := `
	let factorial = pure rec (n: int, acc: int = 1) -> int {
		(0, acc) => acc,
		(n, acc) => factorial(n - 1, acc * n),
	}`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "function_declaration_with_multiple_default_values.golden"))
}

func TestCollector_PureFunctionDeclaration(t *testing.T) {
	source := `
	let add = pure (a: int, b: int) -> int => a + b`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "pure_function_declaration.golden"))
}

func TestCollector_AsyncFunctionDeclaration(t *testing.T) {
	source := `
	let fetchJSON = async (url: string, options: FetchOptions) -> Response => {
		let resp = await fetch(url, options)
		return JSON.parse(resp)
	}`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "async_function_declaration.golden"))
}

func TestCollector_PureAsyncFunctionDeclaration(t *testing.T) {
	source := `
	let compute = pure async (n: int) -> int => n * 2`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "pure_async_function_declaration.golden"))
}

func TestCollector_PureRecursiveFunctionWithPatternMatching(t *testing.T) {
	source := `
	let fib = pure rec (n: int) -> int {
		(0) => 0,
		(1) => 1,
		(n) => fib(n-1) + fib(n-2),
	}`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "pure_function_with_pattern_matching.golden"))
}

func TestCollector_PureRecursiveFunctionWithMultipleFunctionClausesAndGuard(t *testing.T) {
	source := `
	let fib = pure rec (n: int) -> int {
		(n) if n < 2 => n,
		(n) => fib(n-2) + fib(n-1),
	}`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "pure_recursive_function_with_multiple_function_clauses_and_guard.golden"))
}

func TestCollector_FunctionWithGenericParams(t *testing.T) {
	source := `
	let sum<n> = (a: n, b: n) -> n => a + b`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "function_with_generic_params.golden"))
}

func TestCollector_FunctionWithGenericParamsAndConstraints(t *testing.T) {
	source := `
	let compare<n> where n: Ord = (a: n, b: n) -> n => a <=> b`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "function_with_generic_params_and_constraints.golden"))
}

func TestCollector_FunctionWithPatternParams(t *testing.T) {
	source := `
	let foo = ({ x, y }: Point, [one, two, three, ...rest]: []string, (alpha, beta): SomeTuple) -> Void => {
		// do stuff
	}`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "function_with_pattern_params.golden"))
}

func TestCollector_HigherOrderFunction(t *testing.T) {
	source := `
	let map<t,u> = (array: []t, func: (t) -> u) -> []u => {
		//  new_array = []
		//  for item in array {
		//    new_array.push(func(item))
		//  }
		//  new_array
	}`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "higher_order_function.golden"))
}

func TestCollector_TypeParameterModifiers(t *testing.T) {
	source := `
	let render = (scene: ref Scene) -> Void => {
		// render the scene
	}`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "type_parameter_modifiers.golden"))
}

func TestCollector_FunctionTypeReturnModifier(t *testing.T) {
	source := `
	let open_file = (path: string) -> mut File => {
		// do some stuff
		return file
	}`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "function_type_return_modifier.golden"))
}
