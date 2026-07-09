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
	let factorial = pure (n: i64, acc: i64 = 1) -> i64 {
		(0, acc) => acc,
		(n, acc) => factorial(n - 1, acc * n),
	}`
	runGoldenTest(t, source, "function_declaration_with_multiple_default_values")
}

func TestCollector_PureFunctionDeclaration(t *testing.T) {
	runGoldenTest(t, `
	let add = pure (a: i64, b: i64) -> i64 => a + b`, "pure_function_declaration")
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
	let compute = pure async (n: i64) -> i64 => n * 2`, "pure_async_function_declaration")
}

func TestCollector_PureRecursiveFunctionWithPatternMatching(t *testing.T) {
	source := `
	let fib = pure (n: i64) -> i64 {
		(0) => 0,
		(1) => 1,
		(n) => fib(n-1) + fib(n-2),
	}`
	runGoldenTest(t, source, "pure_function_with_pattern_matching")
}

func TestCollector_PureRecursiveFunctionWithMultipleFunctionClausesAndGuard(t *testing.T) {
	source := `
	let fib = pure (n: i64) -> i64 {
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

// Function-definition sugar (`let name(params) => body`, no `=`) must collect
// to exactly the same AST as the equivalent `let name = (params) => body`
// form — the grammar stores the sugar's lambda in the same `value` field, so
// there is no dedicated AST node. Each test below reuses the `=`-form's golden
// file; a pass asserts the two forms produce byte-identical ASTs.
func TestCollector_SugarFunctionWithParams(t *testing.T) {
	runGoldenTest(t, `
	let add(a: Int, b: String) => a + b`, "basic_function_declaration_with_params")
}

func TestCollector_SugarFunctionWithGenericParams(t *testing.T) {
	runGoldenTest(t, `
	let sum<n>(a: n, b: n) -> n => a + b`, "function_with_generic_params")
}

func TestCollector_SugarFunctionWithGenericParamsAndConstraints(t *testing.T) {
	runGoldenTest(t, `
	let compare<n> where n: Ord (a: n, b: n) -> n => a <=> b`, "function_with_generic_params_and_constraints")
}

// Modifiers leading the name (`let pure add(…)`) must be lifted onto the lambda,
// producing the same AST as the `= pure (…)` form — reusing that golden proves it.
func TestCollector_SugarFunctionWithLeadingModifier(t *testing.T) {
	runGoldenTest(t, `
	let pure add(a: i64, b: i64) -> i64 => a + b`, "pure_function_declaration")
}

func TestCollector_SugarFunctionWithLeadingPureAsync(t *testing.T) {
	runGoldenTest(t, `
	let pure async compute(n: i64) -> i64 => n * 2`, "pure_async_function_declaration")
}

// The grammar's fn_modifiers rule accepts any order / duplicates; the collector
// enforces the canonical order and rejects duplicates (parity with the fixed
// order the `= <lambda>` grammar enforces).
func TestCollector_SugarModifierOutOfOrderIsError(t *testing.T) {
	if errs := parseAndCollectErrors(t, `
	let async pure compute(n: i64) -> i64 => n`); len(errs) == 0 {
		t.Fatalf("expected an out-of-order modifier error, got none")
	}
}

func TestCollector_SugarDuplicateModifierIsError(t *testing.T) {
	if errs := parseAndCollectErrors(t, `
	let pure pure compute(n: i64) -> i64 => n`); len(errs) == 0 {
		t.Fatalf("expected a duplicate modifier error, got none")
	}
}

func TestCollector_FunctionWithPatternParams(t *testing.T) {
	source := `
	let foo = ({ x, y }: Point, [one, two, three, ...rest]: []string, (alpha, beta): SomeTuple) -> void => {
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
	let render = (scene: ref Scene) -> void => {
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
