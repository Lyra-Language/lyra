package collector_test

import "testing"

func TestCollect_StringConcatExpr(t *testing.T) {
	source := `let greeting = "Hello, " ++ name`
	runGoldenTest(t, source, "expr_string_concat")
}

func TestCollect_StringConcatExprChained(t *testing.T) {
	source := `let s = "a" ++ "b" ++ "c"`
	runGoldenTest(t, source, "expr_string_concat_chained")
}

// The following tests mirror the tree-sitter corpus tests for string_concat.

func TestCollect_StringConcatLiterals(t *testing.T) {
	source := `"hello" ++ " world"`
	runGoldenTest(t, source, "expr_string_concat_literals")
}

func TestCollect_StringConcatVariables(t *testing.T) {
	source := `greeting ++ name`
	runGoldenTest(t, source, "expr_string_concat_variables")
}

func TestCollect_StringConcatFunctionCallOperands(t *testing.T) {
	source := `foo() ++ bar()`
	runGoldenTest(t, source, "expr_string_concat_function_call_operands")
}

func TestCollect_StringConcatInLetDeclaration(t *testing.T) {
	source := `let s = "Hello, " ++ name`
	runGoldenTest(t, source, "expr_string_concat_in_let_declaration")
}
