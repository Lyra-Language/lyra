package tests

import (
	"path/filepath"
	"testing"
)

func TestCollector_VariableDeclarationWithoutValue(t *testing.T) {
	source := `
	var the_answer
	the_answer = 42`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "variable_declaration_without_value.golden"))
}

func TestCollector_VariableDeclarationWithLet(t *testing.T) {
	source := `let pi: float = 3.14159`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "variable_declaration_with_let.golden"))
}

func TestCollector_VariableDeclarationWithVar(t *testing.T) {
	source := `var the_answer: int = 42`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "variable_declaration_with_var.golden"))
}

func TestCollector_VariableDeclarationWithConst(t *testing.T) {
	source := `const PI: float = 3.14159`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "variable_declaration_with_const.golden"))
}

func TestCollector_VariableDeclarationWithoutTypeAnnotation(t *testing.T) {
	source := `let pi = 3.14159`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "variable_declaration_without_type_annotation.golden"))
}

func TestCollector_VariableDeclarationWithTupleType(t *testing.T) {
	source := `let the_answer: (int, int) = (42, 13)`
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", "variable_declaration_with_tuple_type.golden"))
}
