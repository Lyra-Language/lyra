package collector

import (
	"testing"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/parser"
	"github.com/Lyra-Language/lyra/pkg/types"
)

var intType = types.PrimitiveType{Name: "Int"}

func TestCollector_StructTypeDeclaration(t *testing.T) {
	source := `struct Point {
		x: Int,
		y: Int = 0,
	}`

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
	namedNode, ok := table.GlobalScope.Lookup("Point")
	if !ok {
		t.Fatalf("\"Point\" not found in global scope")
	}

	structDecl, ok := namedNode.(*ast.TypeDeclarationStmt)
	if !ok {
		t.Fatalf("\"Point\" is not a TypeDeclarationStmt, got %T", namedNode)
	}

	expectedFields := map[string]types.StructField{
		"x": {Name: "x", Type: intType, DefaultValue: nil},
		"y": {Name: "y", Type: intType, DefaultValue: ast.IntegerLiteral{Value: 0}},
	}
	if !types.TypesEqual(structDecl.Type, types.StructType{Name: "Point", Fields: expectedFields}) {
		t.Fatalf("\"Point\" type is not StructType. Got %v", structDecl.Type)
	}
}

func TestCollector_VariableDeclaration(t *testing.T) {
	source := `let the_answer: Int = 42`

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
	namedNode, ok := table.GlobalScope.Lookup("the_answer")
	if !ok {
		t.Fatalf("\"the_answer\" not found in global scope")
	}

	varDecl, ok := namedNode.(*ast.VariableDeclarationStmt)
	if !ok {
		t.Fatalf("\"the_answer\" is not a VariableDeclarationStmt, got %T", namedNode)
	}

	if !types.TypesEqual(varDecl.Type, intType) {
		t.Fatalf("\"the_answer\" type is not Int. Got %v", varDecl.Type)
	}

	// Note: Init expression type is not set during collection, only during type checking
	if varDecl.Value == nil {
		t.Fatalf("\"the_answer\" has no init value")
	}
}

func TestCollector_FunctionDefinition(t *testing.T) {
	source := `def sum: (Int, Int) -> Int = (a, b) => a + b`

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
