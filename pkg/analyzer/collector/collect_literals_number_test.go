package collector

import (
	"testing"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/parser"
)

func TestCollectIntegerLiteralExpr(t *testing.T) {
	source := `let one_million = 1_000_000`
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

	varDeclStmt, ok := program.Statements[0].(*ast.VarDeclStmt)
	if !ok {
		t.Fatalf("Expected VarDeclStmt, got %T", program.Statements[0])
	}

	if varDeclStmt.Name != "one_million" {
		t.Fatalf("Expected Name to be \"one_million\", got %s", varDeclStmt.Name)
	}

	if varDeclStmt.Value == nil {
		t.Fatalf("Expected Value, got nil")
	}

	if varDeclStmt.Value.GetName() != "IntegerLiteralExpr(1000000, Base: 10)" {
		t.Fatalf("Expected Value name to be \"IntegerLiteralExpr(1000000, Base: 10)\", got %s", varDeclStmt.Value.GetName())
	}

	if varDeclStmt.Value.(*ast.IntegerLiteralExpr).Value != 1000000 {
		t.Fatalf("Expected Value value to be 1000000, got %d", varDeclStmt.Value.(*ast.IntegerLiteralExpr).Value)
	}

	if varDeclStmt.Value.(*ast.IntegerLiteralExpr).Base != ast.IntegerBase10 {
		t.Fatalf("Expected Value base to be 10, got %d", varDeclStmt.Value.(*ast.IntegerLiteralExpr).Base)
	}
}

func TestCollectBinaryIntegerLiteralExpr(t *testing.T) {
	source := `let fourty_two = 0b101010`
	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	collector := NewCollector([]byte(source))
	program, _, errors := collector.Collect(tree.RootNode())
	if len(errors) > 0 {
		t.Fatalf("Collector errors: %v", errors)
	}
	// program.Print("")

	if len(program.Statements) != 1 {
		t.Fatalf("Expected 1 statement, got %d", len(program.Statements))
	}

	varDeclStmt, ok := program.Statements[0].(*ast.VarDeclStmt)
	if !ok {
		t.Fatalf("Expected VarDeclStmt, got %T", program.Statements[0])
	}

	if varDeclStmt.Value == nil {
		t.Fatalf("Expected Value, got nil")
	}

	if varDeclStmt.Value.GetName() != "IntegerLiteralExpr(42, Base: 2)" {
		t.Fatalf("Expected Expression name to be \"IntegerLiteralExpr(42, Base: 2)\", got %s", varDeclStmt.Value.GetName())
	}

	if varDeclStmt.Value.(*ast.IntegerLiteralExpr).Value != 42 {
		t.Fatalf("Expected Expression value to be 42, got %d", varDeclStmt.Value.(*ast.IntegerLiteralExpr).Value)
	}

	if varDeclStmt.Value.(*ast.IntegerLiteralExpr).Base != ast.IntegerBase2 {
		t.Fatalf("Expected Value base to be 2, got %d", varDeclStmt.Value.(*ast.IntegerLiteralExpr).Base)
	}
}

func TestCollectOctalIntegerLiteralExpr(t *testing.T) {
	source := `let fourty_two = 0o52`
	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	collector := NewCollector([]byte(source))
	program, _, errors := collector.Collect(tree.RootNode())
	if len(errors) > 0 {
		t.Fatalf("Collector errors: %v", errors)
	}
	// program.Print("")

	if len(program.Statements) != 1 {
		t.Fatalf("Expected 1 statement, got %d", len(program.Statements))
	}
	varDeclStmt, ok := program.Statements[0].(*ast.VarDeclStmt)
	if !ok {
		t.Fatalf("Expected VarDeclStmt, got %T", program.Statements[0])
	}

	if varDeclStmt.Value == nil {
		t.Fatalf("Expected Value, got nil")
	}

	if varDeclStmt.Value.GetName() != "IntegerLiteralExpr(42, Base: 8)" {
		t.Fatalf("Expected Expression name to be \"IntegerLiteralExpr(42, Base: 8)\", got %s", varDeclStmt.Value.GetName())
	}

	if varDeclStmt.Value.(*ast.IntegerLiteralExpr).Value != 42 {
		t.Fatalf("Expected Expression value to be 42, got %d", varDeclStmt.Value.(*ast.IntegerLiteralExpr).Value)
	}

	if varDeclStmt.Value.(*ast.IntegerLiteralExpr).Base != ast.IntegerBase8 {
		t.Fatalf("Expected Value base to be 8, got %d", varDeclStmt.Value.(*ast.IntegerLiteralExpr).Base)
	}
}

func TestCollectHexadecimalIntegerLiteralExpr(t *testing.T) {
	source := `let fourty_two = 0x2A`
	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	collector := NewCollector([]byte(source))
	program, _, errors := collector.Collect(tree.RootNode())
	if len(errors) > 0 {
		t.Fatalf("Collector errors: %v", errors)
	}
	// program.Print("")

	if len(program.Statements) != 1 {
		t.Fatalf("Expected 1 statement, got %d", len(program.Statements))
	}
	varDeclStmt, ok := program.Statements[0].(*ast.VarDeclStmt)
	if !ok {
		t.Fatalf("Expected VarDeclStmt, got %T", program.Statements[0])
	}

	if varDeclStmt.Value == nil {
		t.Fatalf("Expected Value, got nil")
	}

	if varDeclStmt.Value.GetName() != "IntegerLiteralExpr(42, Base: 16)" {
		t.Fatalf("Expected Expression name to be \"IntegerLiteralExpr(42, Base: 16)\", got %s", varDeclStmt.Value.GetName())
	}

	if varDeclStmt.Value.(*ast.IntegerLiteralExpr).Value != 42 {
		t.Fatalf("Expected Expression value to be 42, got %d", varDeclStmt.Value.(*ast.IntegerLiteralExpr).Value)
	}

	if varDeclStmt.Value.(*ast.IntegerLiteralExpr).Base != ast.IntegerBase16 {
		t.Fatalf("Expected Value base to be 16, got %d", varDeclStmt.Value.(*ast.IntegerLiteralExpr).Base)
	}
}

func TestCollectFloatLiteralExpr(t *testing.T) {
	source := `let pi = 3.14159`
	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	// p := printer.NewPrinter([]byte(source))
	// p.Print(tree.RootNode())

	collector := NewCollector([]byte(source))
	program, _, errors := collector.Collect(tree.RootNode())
	if len(errors) > 0 {
		t.Fatalf("Collector errors: %v", errors)
	}
	// program.Print("")

	if len(program.Statements) != 1 {
		t.Fatalf("Expected 1 statement, got %d", len(program.Statements))
	}

	varDeclStmt, ok := program.Statements[0].(*ast.VarDeclStmt)
	if !ok {
		t.Fatalf("Expected VarDeclStmt, got %T", program.Statements[0])
	}

	if varDeclStmt.Value == nil {
		t.Fatalf("Expected Value, got nil")
	}

	if varDeclStmt.Value.GetName() != "FloatLiteralExpr(3.14159)" {
		t.Fatalf("Expected Expression name to be \"FloatLiteralExpr(3.14159)\", got %s", varDeclStmt.Value.GetName())
	}

	if varDeclStmt.Value.(*ast.FloatLiteralExpr).Value != 3.14159 {
		t.Fatalf("Expected Expression value to be 3.14159, got %f", varDeclStmt.Value.(*ast.FloatLiteralExpr).Value)
	}
}

func TestCollectFloatLiteralExprWithExponent(t *testing.T) {
	source := `let pi = 0.03141592e2`
	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	collector := NewCollector([]byte(source))
	program, _, errors := collector.Collect(tree.RootNode())
	if len(errors) > 0 {
		t.Fatalf("Collector errors: %v", errors)
	}
	// program.Print("")

	if len(program.Statements) != 1 {
		t.Fatalf("Expected 1 statement, got %d", len(program.Statements))
	}
	varDeclStmt, ok := program.Statements[0].(*ast.VarDeclStmt)
	if !ok {
		t.Fatalf("Expected VarDeclStmt, got %T", program.Statements[0])
	}

	if varDeclStmt.Value == nil {
		t.Fatalf("Expected Value, got nil")
	}

	if varDeclStmt.Value.GetName() != "FloatLiteralExpr(3.141592)" {
		t.Fatalf("Expected Expression name to be \"FloatLiteralExpr(3.141592)\", got %s", varDeclStmt.Value.GetName())
	}

	if varDeclStmt.Value.(*ast.FloatLiteralExpr).Value != 0.03141592e2 {
		t.Fatalf("Expected Expression value to be 0.03141592e2, got %f", varDeclStmt.Value.(*ast.FloatLiteralExpr).Value)
	}
}

func TestCollectFloatLiteralExprWithPositiveExponent(t *testing.T) {
	source := `let pi = 0.03141592e+2`
	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	collector := NewCollector([]byte(source))
	program, _, errors := collector.Collect(tree.RootNode())
	if len(errors) > 0 {
		t.Fatalf("Collector errors: %v", errors)
	}
	// program.Print("")

	if len(program.Statements) != 1 {
		t.Fatalf("Expected 1 statement, got %d", len(program.Statements))
	}
	varDeclStmt, ok := program.Statements[0].(*ast.VarDeclStmt)
	if !ok {
		t.Fatalf("Expected VarDeclStmt, got %T", program.Statements[0])
	}

	if varDeclStmt.Value == nil {
		t.Fatalf("Expected Value, got nil")
	}

	if varDeclStmt.Value.GetName() != "FloatLiteralExpr(3.141592)" {
		t.Fatalf("Expected Expression name to be \"FloatLiteralExpr(3.141592)\", got %s", varDeclStmt.Value.GetName())
	}

	if varDeclStmt.Value.(*ast.FloatLiteralExpr).Value != 0.03141592e+2 {
		t.Fatalf("Expected Expression value to be 0.03141592e+2, got %f", varDeclStmt.Value.(*ast.FloatLiteralExpr).Value)
	}
}

func TestCollectFloatLiteralExprWithNegativeExponent(t *testing.T) {
	source := `let pi = 314.1592e-2`
	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	collector := NewCollector([]byte(source))
	program, _, errors := collector.Collect(tree.RootNode())
	if len(errors) > 0 {
		t.Fatalf("Collector errors: %v", errors)
	}
	// program.Print("")

	if len(program.Statements) != 1 {
		t.Fatalf("Expected 1 statement, got %d", len(program.Statements))
	}
	varDeclStmt, ok := program.Statements[0].(*ast.VarDeclStmt)
	if !ok {
		t.Fatalf("Expected VarDeclStmt, got %T", program.Statements[0])
	}
	if varDeclStmt.Value == nil {
		t.Fatalf("Expected Value, got nil")
	}

	if varDeclStmt.Value.GetName() != "FloatLiteralExpr(3.141592)" {
		t.Fatalf("Expected Expression name to be \"FloatLiteralExpr(3.141592)\", got %s", varDeclStmt.Value.GetName())
	}

	if varDeclStmt.Value.(*ast.FloatLiteralExpr).Value != 314.1592e-2 {
		t.Fatalf("Expected Expression value to be 314.1592e-2, got %f", varDeclStmt.Value.(*ast.FloatLiteralExpr).Value)
	}
}

func TestCollectFloatLiteralExprWithUnderscores(t *testing.T) {
	source := `let pi = 3.141_59`
	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	collector := NewCollector([]byte(source))
	program, _, errors := collector.Collect(tree.RootNode())
	if len(errors) > 0 {
		t.Fatalf("Collector errors: %v", errors)
	}
	// program.Print("")

	if len(program.Statements) != 1 {
		t.Fatalf("Expected 1 statement, got %d", len(program.Statements))
	}
	varDeclStmt, ok := program.Statements[0].(*ast.VarDeclStmt)
	if !ok {
		t.Fatalf("Expected VarDeclStmt, got %T", program.Statements[0])
	}

	if varDeclStmt.Value == nil {
		t.Fatalf("Expected Value, got nil")
	}

	if varDeclStmt.Value.GetName() != "FloatLiteralExpr(3.14159)" { // NOTE: no trailing zeros
		t.Fatalf("Expected Expression name to be \"FloatLiteralExpr(3.14159)\", got %s", varDeclStmt.Value.GetName())
	}

	if varDeclStmt.Value.(*ast.FloatLiteralExpr).Value != 3.14159 {
		t.Fatalf("Expected Expression value to be 3.14159, got %f", varDeclStmt.Value.(*ast.FloatLiteralExpr).Value)
	}
}
