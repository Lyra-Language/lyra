package collector

import (
	"testing"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/parser"
	"github.com/Lyra-Language/lyra/pkg/types"
)

func TestCollector_SimpleAnonymousTupleLiteral(t *testing.T) {
	source := `let tuple: (int, int) = (1, 2)`
	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	collector := NewCollector([]byte(source))
	program, _, errors := collector.Collect(tree.RootNode())
	if len(errors) > 0 {
		t.Fatalf("Collector errors: %v", errors)
	}

	if len(program.Statements) != 1 {
		t.Fatalf("Expected 1 statement, got %d", len(program.Statements))
	}

	stmt, ok := program.Statements[0].(*ast.VarDeclStmt)
	if !ok {
		t.Fatalf("Expected VarDeclStmt, got %T", program.Statements[0])
	}

	if stmt.Value == nil {
		t.Fatalf("Expected Value, got nil")
	}

	if stmt.Value.GetName() != "tuple_literal_expr" {
		t.Fatalf("Expected Value name to be \"tuple_literal_expr\", got %s", stmt.Value.GetName())
	}
	expectedType := types.TupleType{Name: "?", Elements: []types.Type{intType, intType}}
	if !types.TypesEqual(stmt.Value.GetType(), expectedType) {
		t.Fatalf("Expected Expression with type %s, got %s", expectedType.GetName(), stmt.Value.GetType().GetName())
	}

	if stmt.Type == nil {
		t.Fatalf("Expected tuple type, got nil")
	}

	if !types.TypesEqual(stmt.Type, types.TupleType{Name: "?", Elements: []types.Type{intType, intType}}) {
		t.Fatalf("Expected tuple type to be (int, int), got %s", stmt.Type.GetName())
	}
}
