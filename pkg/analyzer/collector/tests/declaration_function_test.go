package collector_test

import "testing"

func TestCollector_BasicFunctionDeclarationWithoutParams(t *testing.T) {
	source := `
	let foo = () => {
		// do stuff
	}`
	runGoldenTest(t, source, "basic_function_declaration_without_params")
}

func TestCollector_BasicFunctionDeclarationWithVisibility(t *testing.T) {
	source := `
	pub let foo = () => {
		// do stuff
	}`
	runGoldenTest(t, source, "basic_function_declaration_with_visibility")
}

func TestCollector_BasicFunctionDeclarationWithParams(t *testing.T) {
	runGoldenTest(t, `
	let add = (a: Int, b: String) => a + b`, "basic_function_declaration_with_params")
}

func TestCollector_DefaultParameterValues(t *testing.T) {
	runGoldenTest(t, `
	let greet = (name: string, prefix: string = "Hello") -> string => "${prefix} ${name}"`, "function_declaration_with_default_parameter_values")
}

func TestCollector_DefaultValueAsCallExpression(t *testing.T) {
	runGoldenTest(t, `
	let process = (items: []string, config: Config = get_default_config()) -> void => {}`, "function_declaration_with_default_value_as_call_expression")
}

func TestCollector_DefaultValueWithMultipleClauses(t *testing.T) {
	source := `
	let factorial = pure rec (n: int, acc: int = 1) -> int {
		(0, acc) => acc,
		(n, acc) => factorial(n - 1, acc * n),
	}`
	runGoldenTest(t, source, "function_declaration_with_multiple_default_values")
}

func TestCollector_PureFunctionDeclaration(t *testing.T) {
	runGoldenTest(t, `
	let add = pure (a: int, b: int) -> int => a + b`, "pure_function_declaration")
}

func TestCollector_AsyncFunctionDeclaration(t *testing.T) {
	source := `
	let fetchJSON = async (url: string, options: FetchOptions) -> Response => {
		let resp = await fetch(url, options)
		return JSON.parse(resp)
	}`
	runGoldenTest(t, source, "async_function_declaration")
}

func TestCollector_PureAsyncFunctionDeclaration(t *testing.T) {
	runGoldenTest(t, `
	let compute = pure async (n: int) -> int => n * 2`, "pure_async_function_declaration")
}

func TestCollector_PureRecursiveFunctionWithPatternMatching(t *testing.T) {
	source := `
	let fib = pure rec (n: int) -> int {
		(0) => 0,
		(1) => 1,
		(n) => fib(n-1) + fib(n-2),
	}`
	runGoldenTest(t, source, "pure_function_with_pattern_matching")
}

func TestCollector_PureRecursiveFunctionWithMultipleFunctionClausesAndGuard(t *testing.T) {
	source := `
	let fib = pure rec (n: int) -> int {
		(n) if n < 2 => n,
		(n) => fib(n-2) + fib(n-1),
	}`
	runGoldenTest(t, source, "pure_recursive_function_with_multiple_function_clauses_and_guard")
}

func TestCollector_FunctionWithGenericParams(t *testing.T) {
	runGoldenTest(t, `
	let sum<n> = (a: n, b: n) -> n => a + b`, "function_with_generic_params")
}

func TestCollector_FunctionWithGenericParamsAndConstraints(t *testing.T) {
	runGoldenTest(t, `
	let compare<n> where n: Ord = (a: n, b: n) -> n => a <=> b`, "function_with_generic_params_and_constraints")
}

func TestCollector_FunctionWithPatternParams(t *testing.T) {
	source := `
	let foo = ({ x, y }: Point, [one, two, three, ...rest]: []string, (alpha, beta): SomeTuple) -> Void => {
		// do stuff
	}`
	runGoldenTest(t, source, "function_with_pattern_params")
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
	runGoldenTest(t, source, "higher_order_function")
}

func TestCollector_TypeParameterModifiers(t *testing.T) {
	source := `
	let render = (scene: ref Scene) -> Void => {
		// render the scene
	}`
	runGoldenTest(t, source, "type_parameter_modifiers")
}

func TestCollector_FunctionTypeReturnModifier(t *testing.T) {
	source := `
	let open_file = (path: string) -> mut File => {
		// do some stuff
		return file
	}`
	runGoldenTest(t, source, "function_type_return_modifier")
}
