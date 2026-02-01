package collector

import (
	"testing"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/parser"
	"github.com/Lyra-Language/lyra/pkg/printer"
	"github.com/Lyra-Language/lyra/pkg/types"
)

func TestCollectSimpleStaticArrayLiteral(t *testing.T) {
	source := `let arr = [1, 2, 3]`

	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	p := printer.NewPrinter([]byte(source))
	p.Print(tree.RootNode())

	collector := NewCollector([]byte(source))
	program, _, errors := collector.Collect(tree.RootNode())

	program.Print("")

	if len(errors) > 0 {
		t.Fatalf("Expected no errors, got %v", errors)
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

	if stmt.Value.GetType() != nil {
		t.Fatalf("Expected Expression with type nil, got %s", stmt.Value.GetType().GetName())
	}
}

func TestCollectSimpleDynamicArrayLiteral(t *testing.T) {
	source := `let arr: []int = [1, 2, 3]`

	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	p := printer.NewPrinter([]byte(source))
	p.Print(tree.RootNode())

	collector := NewCollector([]byte(source))
	program, _, errors := collector.Collect(tree.RootNode())

	program.Print("")

	if len(errors) > 0 {
		t.Fatalf("Expected no errors, got %v", errors)
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

	if stmt.Value.GetType() != nil {
		t.Fatalf("Expected Expression with type nil, got %s", stmt.Value.GetType().GetName())
	}

	if stmt.Type == nil {
		t.Fatalf("Expected Type, got nil")
	}

	if !types.TypesEqual(stmt.Type, types.DynamicArrayType{ElementType: intType}) {
		t.Fatalf("Expected Type to be DynamicArray<int>, got %s", stmt.Type.GetName())
	}
}

func TestCollectArrayRepeatInitialization(t *testing.T) {
	source := `let arr = [0; 8]`

	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	p := printer.NewPrinter([]byte(source))
	p.Print(tree.RootNode())

	collector := NewCollector([]byte(source))
	program, _, errors := collector.Collect(tree.RootNode())

	program.Print("")

	if len(errors) > 0 {
		t.Fatalf("Expected no errors, got %v", errors)
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

	if stmt.Value.GetType() == nil {
		t.Fatalf("Expected Expression with type, got nil")
	}

	if !types.TypesEqual(stmt.Value.GetType(), types.StaticArrayType{ElementType: intType, Size: 8}) {
		t.Fatalf("Expected StaticArray<int, 8>, got %s", stmt.Value.GetType().GetName())
	}

	if stmt.Type == nil {
		t.Fatalf("Expected Type, got nil")
	}

	if !types.TypesEqual(stmt.Type, types.StaticArrayType{ElementType: intType, Size: 8}) {
		t.Fatalf("Expected Type to be StaticArray<int, 8>, got %s", stmt.Type.GetName())
	}
}

func TestCollectArrayRepeatInitializationWithCompileTimeConstantCount(t *testing.T) {
	source := `let arr = [0; SIZE]`

	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	p := printer.NewPrinter([]byte(source))
	p.Print(tree.RootNode())

	collector := NewCollector([]byte(source))
	program, _, errors := collector.Collect(tree.RootNode())

	program.Print("")

	if len(errors) > 0 {
		t.Fatalf("Expected no errors, got %v", errors)
	}

	if len(program.Statements) != 1 {
		t.Fatalf("Expected 1 statement, got %d", len(program.Statements))
	}

	stmt, ok := program.Statements[0].(*ast.VarDeclStmt)
	if !ok {
		t.Fatalf("Expected VarDeclStmt, got %T", program.Statements[0])
	}

	if stmt.Type == nil {
		t.Fatalf("Expected Type, got nil")
	}

	if stmt.Type.(types.StaticArrayType).Size != -1 {
		t.Fatalf("Expected Size to be -1, got %d", stmt.Type.(types.StaticArrayType).Size)
	}

	if !types.TypesEqual(stmt.Type, types.StaticArrayType{ElementType: intType, Size: -1}) {
		t.Fatalf("Expected Type to be StaticArray<int, -1>, got %s", stmt.Type.GetName())
	}

	if stmt.Value.(*ast.ArrayRepeatExpr).Value.(*ast.IntegerLiteralExpr).Value != 0 {
		t.Fatalf("Expected Value to be 0, got %d", stmt.Value.(*ast.ArrayRepeatExpr).Value.(*ast.IntegerLiteralExpr).Value)
	}

	if stmt.Value.(*ast.ArrayRepeatExpr).Count.GetName() != "SIZE" {
		t.Fatalf("Expected Count to be SIZE, got %s", stmt.Value.(*ast.ArrayRepeatExpr).Count.GetName())
	}
}
