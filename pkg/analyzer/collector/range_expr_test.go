package collector

import (
	"testing"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/parser"
	"github.com/Lyra-Language/lyra/pkg/types"
)

func TestCollector_RangeExpression(t *testing.T) {
	source := `0..<10`
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

	exprStatement, ok := program.Statements[0].(*ast.ExpressionStmt)
	if !ok {
		t.Fatalf("Expected ExpressionStmt, got %T", program.Statements[0])
	}

	if exprStatement.Expression == nil {
		t.Fatalf("Expected Expression, got nil")
	}

	if !types.TypesEqual(exprStatement.Expression.GetType(), types.RangeType{Start: types.PrimitiveType{Name: types.Int}, End: types.PrimitiveType{Name: types.Int}, EndOperator: "<", Step: nil}) {
		exprStatement.Expression.GetType().Print("")
		t.Fatalf("Expected Expression type to be types.RangeType, got %T", exprStatement.Expression.GetType())
	}

	rangeExpr, ok := exprStatement.Expression.(*ast.RangeExpr)
	if !ok {
		t.Fatalf("Expected RangeExpr, got %T", exprStatement.Expression)
	}

	if rangeExpr.Start.GetType().GetName() != "int" {
		t.Fatalf("Expected Start to be int, got %s", rangeExpr.Start.GetType().GetName())
	}

	if rangeExpr.End.GetType().GetName() != "int" {
		t.Fatalf("Expected End to be int, got %s", rangeExpr.End.GetType().GetName())
	}

	if rangeExpr.EndOperator != "<" {
		t.Fatalf("Expected EndOperator to be <, got %s", rangeExpr.EndOperator)
	}

	if rangeExpr.Step != nil {
		t.Fatalf("Expected Step to be nil, got %T", rangeExpr.Step)
	}
}

func TestCollector_RangeExpressionWithStep(t *testing.T) {
	source := `0..=10,2`
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

	exprStatement, ok := program.Statements[0].(*ast.ExpressionStmt)
	if !ok {
		t.Fatalf("Expected ExpressionStmt, got %T", program.Statements[0])
	}

	if exprStatement.Expression == nil {
		t.Fatalf("Expected Expression, got nil")
	}

	if !types.TypesEqual(
		exprStatement.Expression.GetType(),
		types.RangeType{
			Start:       types.PrimitiveType{Name: types.Int},
			End:         types.PrimitiveType{Name: types.Int},
			EndOperator: "=",
			Step:        types.PrimitiveType{Name: types.Int},
		},
	) {
		t.Fatalf("Expected Expression type to be types.RangeType, got %T", exprStatement.Expression.GetType())
	}

	rangeExpr, ok := exprStatement.Expression.(*ast.RangeExpr)
	if !ok {
		t.Fatalf("Expected RangeExpr, got %T", exprStatement.Expression)
	}

	if rangeExpr.Start.GetType().GetName() != "int" {
		t.Fatalf("Expected Start to be int, got %s", rangeExpr.Start.GetType().GetName())
	}

	if rangeExpr.End.GetType().GetName() != "int" {
		t.Fatalf("Expected End to be int, got %s", rangeExpr.End.GetType().GetName())
	}

	if rangeExpr.EndOperator != "=" {
		t.Fatalf("Expected EndOperator to be =, got %s", rangeExpr.EndOperator)
	}

	if rangeExpr.Step.GetType().GetName() != "int" {
		t.Fatalf("Expected Step to be int, got %s", rangeExpr.Step.GetType().GetName())
	}
}

func TestCollector_RangeExpressionWithStartExpression(t *testing.T) {
	source := `start*2..=end-1`
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

	exprStatement, ok := program.Statements[0].(*ast.ExpressionStmt)
	if !ok {
		t.Fatalf("Expected ExpressionStmt, got %T", program.Statements[0])
	}

	if exprStatement.Expression == nil {
		t.Fatalf("Expected Expression, got nil")
	}

	// Range type is derived from Start/End sub-expressions; start and end identifiers
	// are not resolved by the collector, so we only assert shape and EndOperator.
	rangeType, ok := exprStatement.Expression.GetType().(types.RangeType)
	if !ok {
		t.Fatalf("Expected Expression type to be types.RangeType, got %T", exprStatement.Expression.GetType())
	}
	if rangeType.EndOperator != "=" {
		t.Fatalf("Expected EndOperator =, got %s", rangeType.EndOperator)
	}
	if rangeType.Step != nil {
		t.Fatalf("Expected Step to be nil")
	}
	// TODO: Test Start/End types

	rangeExpr, ok := exprStatement.Expression.(*ast.RangeExpr)
	if !ok {
		t.Fatalf("Expected RangeExpr, got %T", exprStatement.Expression)
	}

	if rangeExpr.Start.GetName() != "start * 2" {
		t.Fatalf("Expected Start to be MathBinaryOpExpr, got %s", rangeExpr.Start.GetName())
	}

	if rangeExpr.End.GetName() != "end - 1" {
		t.Fatalf("Expected End to be MathBinaryOpExpr, got %s", rangeExpr.End.GetName())
	}
}
