package collector

import (
	"testing"

	"github.com/Lyra-Language/lyra/pkg/ast"
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

	// p := printer.NewPrinter([]byte(source))
	// p.Print(tree.RootNode())

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

func TestCollector_FunctionDefinitionWithModifiedParameters(t *testing.T) {
	source := `
		def add: (mut Point, ref Vec3) -> Point = (point, vec) => point + vec
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
	funcDef, ok := table.Functions["add"]
	if !ok {
		t.Fatalf("\"add\" not found in functions")
	}

	if len(funcDef.Clauses) != 1 {
		t.Fatalf("\"add\" should have 1 clause. Got %d", len(funcDef.Clauses))
	}

	if funcDef.Signature == nil {
		t.Fatalf("\"add\" has no signature")
	}

	// Check parameter types
	if funcDef.Signature.ParameterTypes[0].Type.GetName() != "Point" {
		t.Fatalf("\"add\" first parameter type is not Point. Got %v", funcDef.Signature.ParameterTypes[0].GetName())
	}
	if funcDef.Signature.ParameterTypes[1].Type.GetName() != "Vec3" {
		t.Fatalf("\"add\" second parameter type is not Vec3. Got %v", funcDef.Signature.ParameterTypes[1].GetName())
	}
	if funcDef.Signature.ReturnType.GetName() != "Point" {
		t.Fatalf("\"add\" return type is not Point. Got %v", funcDef.Signature.ReturnType)
	}

	// Check parameter modifiers
	if funcDef.Signature.ParameterTypes[0].Modifier != types.Mut {
		t.Fatalf("\"add\" first parameter modifier is not mut. Got %v", funcDef.Signature.ParameterTypes[0].Modifier)
	}
	if funcDef.Signature.ParameterTypes[1].Modifier != types.Ref {
		t.Fatalf("\"add\" second parameter modifier is not ref. Got %v", funcDef.Signature.ParameterTypes[1].Modifier)
	}

	// Check function clauses
	if len(funcDef.Clauses) != 1 {
		t.Fatalf("\"add\" should have 1 clause. Got %d", len(funcDef.Clauses))
	}

	if funcDef.Clauses[0].Parameters[0].GetName() != "point" {
		t.Fatalf("\"add\" first parameter is not point. Got %v", funcDef.Clauses[0].Parameters[0].GetName())
	}
	if funcDef.Clauses[0].Parameters[1].GetName() != "vec" {
		t.Fatalf("\"add\" second parameter is not vec. Got %v", funcDef.Clauses[0].Parameters[1].GetName())
	}

	if funcDef.Clauses[0].Body == nil {
		t.Fatalf("\"add\" first clause body is nil")
	}

	if funcDef.Clauses[0].Body.GetName() != "point + vec" {
		t.Fatalf("\"add\" first clause body is not point + vec. Got %v", funcDef.Clauses[0].Body.GetName())
	}
}

func TestCollector_FunctionDefinitionWithDefaultParameters(t *testing.T) {
	source := `
		def add: (Int, Int) -> Int = (a, b = 0) => a + b
	`

	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	// p := printer.NewPrinter([]byte(source))
	// p.Print(tree.RootNode())

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
	funcDef, ok := table.Functions["add"]
	if !ok {
		t.Fatalf("\"add\" not found in functions")
	}

	if len(funcDef.Clauses) != 1 {
		t.Fatalf("\"add\" should have 1 clause. Got %d", len(funcDef.Clauses))
	}

	if funcDef.Signature == nil {
		t.Fatalf("\"add\" has no signature")
	}

	if !types.TypesEqual(funcDef.Signature.ParameterTypes[0].Type, intType) {
		t.Fatalf("\"add\" first parameter type is not Int. Got %v", funcDef.Signature.ParameterTypes[0].Type)
	}
	if !types.TypesEqual(funcDef.Signature.ParameterTypes[1].Type, intType) {
		t.Fatalf("\"add\" second parameter type is not Int. Got %v", funcDef.Signature.ParameterTypes[1].Type)
	}
	if !types.TypesEqual(funcDef.Signature.ReturnType, intType) {
		t.Fatalf("\"add\" return type is not Int. Got %v", funcDef.Signature.ReturnType)
	}

	// Check default parameters
	if funcDef.Clauses[0].Parameters[0].DefaultValue != nil {
		t.Fatalf("\"add\" first parameter default value is not nil. Got %v", funcDef.Clauses[0].Parameters[0].DefaultValue)
	}
	if funcDef.Clauses[0].Parameters[1].DefaultValue == nil {
		t.Fatalf("\"add\" second parameter default value is nil")
	}
	intLit, ok := funcDef.Clauses[0].Parameters[1].DefaultValue.(*ast.IntegerLiteralExpr)
	if !ok {
		t.Fatalf("\"add\" second parameter default value is not an IntegerLiteralExpr")
	}
	if intLit.Value != 0 {
		t.Fatalf("\"add\" second parameter default value is not 0. Got %v", intLit.Value)
	}
}
