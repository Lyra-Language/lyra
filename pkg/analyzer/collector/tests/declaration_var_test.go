package collector_test

import "testing"

func TestCollector_VariableDeclarationWithoutValue(t *testing.T) {
	source := `
	var the_answer
	the_answer = 42`
	runGoldenTest(t, source, "variable_declaration_without_value")
}

func TestCollector_VariableDeclarationWithLet(t *testing.T) {
	runGoldenTest(t, `let pi: float = 3.14159`, "variable_declaration_with_let")
}

func TestCollector_VariableDeclarationWithVar(t *testing.T) {
	runGoldenTest(t, `var the_answer: int = 42`, "variable_declaration_with_var")
}

func TestCollector_VariableDeclarationWithConst(t *testing.T) {
	runGoldenTest(t, `const PI: float = 3.14159`, "variable_declaration_with_const")
}

func TestCollector_VariableDeclarationWithoutTypeAnnotation(t *testing.T) {
	runGoldenTest(t, `let pi = 3.14159`, "variable_declaration_without_type_annotation")
}

func TestCollector_VariableDeclarationWithTupleType(t *testing.T) {
	runGoldenTest(t, `let the_answer: (int, int) = (42, 13)`, "variable_declaration_with_tuple_type")
}
