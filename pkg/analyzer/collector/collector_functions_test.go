package collector

import (
	"testing"

	"github.com/Lyra-Language/lyra/pkg/parser"
	"github.com/Lyra-Language/lyra/pkg/types"
)

func TestCollector_SimpleFunctionDefinition(t *testing.T) {
	source := `pub def sum<Int>: (Int, Int) -> Int = (a, b) => a + b`

	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	collector := NewCollector([]byte(source))
	program, table, errors := collector.Collect(tree.RootNode())
	if len(errors) > 0 {
		t.Fatalf("Collector errors: %v", errors)
	}
	// program.Print("")

	// Check AST was built
	if len(program.Statements) != 1 {
		t.Fatalf("Expected 1 statement, got %d", len(program.Statements))
	}

	// Check symbol table lookup
	funcDef, ok := table.Functions["sum"]
	if !ok {
		t.Fatalf("\"sum\" not found in functions")
	}

	if len(funcDef.Clauses) != 1 {
		t.Fatalf("\"sum\" should have 1 clause. Got %d", len(funcDef.Clauses))
	}

	if funcDef.Signature == nil {
		t.Fatalf("\"sum\" has no signature")
	}

	if !types.TypesEqual(funcDef.Signature.ParameterTypes[0].Type, intType) {
		t.Fatalf("\"sum\" first parameter type is not Int. Got %v", funcDef.Signature.ParameterTypes[0].Type)
	}
	if !types.TypesEqual(funcDef.Signature.ParameterTypes[1].Type, intType) {
		t.Fatalf("\"sum\" second parameter type is not Int. Got %v", funcDef.Signature.ParameterTypes[1].Type)
	}
	if !types.TypesEqual(funcDef.Signature.ReturnType, intType) {
		t.Fatalf("\"sum\" return type is not Int. Got %v", funcDef.Signature.ReturnType)
	}
}

func TestCollector_FunctionDefinitionWithGenericParams(t *testing.T) {
	source := `pub def sum<t>: (t, t) -> t = (a, b) => a + b`

	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	collector := NewCollector([]byte(source))
	program, table, errors := collector.Collect(tree.RootNode())
	if len(errors) > 0 {
		t.Fatalf("Collector errors: %v", errors)
	}
	// program.Print("")

	if len(program.Statements) != 1 {
		t.Fatalf("Expected 1 statement, got %d", len(program.Statements))
	}

	// Check symbol table lookup
	funcDef, ok := table.Functions["sum"]
	if !ok {
		t.Fatalf("\"sum\" not found in functions")
	}

	genericType := types.GenericType{Name: "t"}

	if !types.TypesEqual(funcDef.Signature.ParameterTypes[0].Type, genericType) {
		t.Fatalf("\"sum\" first parameter type is not {t}. Got %v", funcDef.Signature.ParameterTypes[0].Type)
	}
	if !types.TypesEqual(funcDef.Signature.ParameterTypes[1].Type, genericType) {
		t.Fatalf("\"sum\" second parameter type is not {t}. Got %v", funcDef.Signature.ParameterTypes[1].Type)
	}
	if !types.TypesEqual(funcDef.Signature.ReturnType, genericType) {
		t.Fatalf("\"sum\" return type is not {t}. Got %v", funcDef.Signature.ReturnType)
	}
}

func TestCollector_FunctionDefinitionWithMultipleClausesAndGuard(t *testing.T) {
	source := `
		def fib: (Int) -> Int = {
			(n) if n < 2 => n,
			(n) => fib(n-2) + fib(n-1),
		}
	`

	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	collector := NewCollector([]byte(source))
	program, table, errors := collector.Collect(tree.RootNode())
	if len(errors) > 0 {
		t.Fatalf("Collector errors: %v", errors)
	}
	// program.Print("")

	if len(program.Statements) != 1 {
		t.Fatalf("Expected 1 statement, got %d", len(program.Statements))
	}

	// Check symbol table lookup
	funcDef, ok := table.Functions["fib"]
	if !ok {
		t.Fatalf("\"fib\" not found in functions")
	}

	if len(funcDef.Clauses) != 2 {
		t.Fatalf("\"fib\" should have 2 clauses. Got %d", len(funcDef.Clauses))
	}

	if funcDef.Clauses[0].Guard == nil {
		t.Fatalf("\"fib\" first clause has no guard")
	}

	if funcDef.Clauses[0].Guard.Condition == nil {
		t.Fatalf("\"fib\" first clause guard condition is nil")
	}
	if funcDef.Clauses[0].Guard.Condition.GetName() != "n < 2" {
		t.Fatalf("\"fib\" first clause guard condition is not \"n < 2\". Got %s", funcDef.Clauses[0].Guard.Condition.GetName())
	}

	if funcDef.Signature == nil {
		t.Fatalf("\"fib\" has no signature")
	}

	if !types.TypesEqual(funcDef.Signature.ParameterTypes[0].Type, intType) {
		t.Fatalf("\"fib\" parameter type is not Int. Got %v", funcDef.Signature.ParameterTypes[0].Type)
	}
	if !types.TypesEqual(funcDef.Signature.ReturnType, intType) {
		t.Fatalf("\"fib\" return type is not Int. Got %v", funcDef.Signature.ReturnType)
	}
}
