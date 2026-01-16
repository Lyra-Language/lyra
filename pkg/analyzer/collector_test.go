package analyzer

import (
	"testing"

	"github.com/Lyra-Language/lyra/pkg/parser"
	"github.com/Lyra-Language/lyra/pkg/symbols"
	"github.com/Lyra-Language/lyra/pkg/types"
)

var intType = types.PrimitiveType{Name: "Int"}

func TestCollector_VariableDeclaration(t *testing.T) {
	source := `let the_answer: Int = 42`

	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	// printer := printer.NewPrinter([]byte(source))
	// printer.Print(tree.RootNode())

	collector := NewCollector([]byte(source))
	table, errors := collector.Collect(tree.RootNode())
	if len(errors) > 0 {
		t.Fatalf("Collector errors: %v", errors)
	}
	// t.Logf("Table: %v", table)
	// t.Logf("Errors: %v", errors)

	varSym, ok := table.GlobalScope.Lookup("the_answer")
	if !ok {
		t.Fatalf("\"the_answer\" not found in global scope")
	}
	if !types.TypesEqual(varSym.(*symbols.VariableSymbol).Type, types.PrimitiveType{Name: "Int"}) {
		t.Fatalf("\"the_answer\" type is not Int. Got %v", varSym.(*symbols.VariableSymbol).Type)
	}
	if !types.TypesEqual(varSym.(*symbols.VariableSymbol).InitExpression.Type, types.PrimitiveType{Name: "Int"}) {
		t.Fatalf("\"the_answer\" init expression type is not Int. Got %v", varSym.(*symbols.VariableSymbol).InitExpression.Type)
	}
}

func TestCollector_FunctionDefinition(t *testing.T) {
	source := `def sum: (Int, Int) -> Int = (a, b) => a + b`

	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	collector := NewCollector([]byte(source))
	table, errors := collector.Collect(tree.RootNode())
	if len(errors) > 0 {
		t.Fatalf("Collector errors: %v", errors)
	}

	funcSym, ok := table.Functions["sum"]
	if !ok {
		t.Fatalf("\"sum\" not found in functions")
	}
	if len(funcSym.Clauses) != 1 {
		t.Fatalf("\"sum\" should have 1 clause. Got %d", len(funcSym.Clauses))
	}
	if !types.TypesEqual(funcSym.Signature.ParameterTypes[0].Type, intType) {
		t.Fatalf("\"sum\" first parameter type is not Int. Got %v", funcSym.Signature.ParameterTypes[0].Type)
	}
	if !types.TypesEqual(funcSym.Signature.ParameterTypes[1].Type, intType) {
		t.Fatalf("\"sum\" second parameter type is not Int. Got %v", funcSym.Signature.ParameterTypes[1].Type)
	}
	if !types.TypesEqual(funcSym.Signature.ReturnType, intType) {
		t.Fatalf("\"sum\" return type is not Int. Got %v", funcSym.Signature.ReturnType)
	}
}
