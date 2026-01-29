package collector

import (
	"testing"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/parser"
	"github.com/Lyra-Language/lyra/pkg/printer"
	"github.com/Lyra-Language/lyra/pkg/types"
)

func TestCollector_SimpleRangeConstrainedType(t *testing.T) {
	source := `
	type Angle = Float where range(0..<360)
	`
	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	p := printer.NewPrinter([]byte(source))
	p.Print(tree.RootNode())

	collector := NewCollector([]byte(source))
	program, table, errors := collector.Collect(tree.RootNode())
	if len(errors) > 0 {
		t.Fatalf("Collector errors: %v", errors)
	}

	// program.Print("")

	if len(program.Statements) != 1 {
		t.Fatalf("Expected 1 statement, got %d", len(program.Statements))
	}

	namedNode, ok := table.GlobalScope.Lookup("Angle")
	if !ok {
		t.Fatalf("\"Angle\" not found in global scope")
	}
	angleDecl, ok := namedNode.(*ast.TypeDeclStmt)
	if !ok {
		t.Fatalf("\"Angle\" is not a TypeDeclStmt, got %T", namedNode)
	}
	if angleDecl.GetName() != "Angle" {
		t.Fatalf("\"Angle\" should have name \"Angle\". Got %s", angleDecl.GetName())
	}
	if len(angleDecl.Type.(types.ConstrainedType).Constraints) != 1 {
		t.Fatalf("\"Angle\" should have 1 constraint. Got %d", len(angleDecl.Type.(types.ConstrainedType).Constraints))
	}
	if !types.TypesEqual(angleDecl.Type.(types.ConstrainedType).Type, types.PrimitiveType{Name: "Float"}) {
		t.Fatalf("\"Angle\" should have type \"Float\". Got %v", angleDecl.Type.(types.ConstrainedType).Type)
	}
	rangeConstraint, ok := angleDecl.Type.(types.ConstrainedType).Constraints[0].(types.RangeConstraint)
	if !ok {
		t.Fatalf("\"Angle\" should have range constraint. Got %T", angleDecl.Type.(types.ConstrainedType).Constraints[0])
	}
	if rangeConstraint.Start.GetName() != "0" {
		t.Fatalf("\"Angle\" should have start 0. Got %s", rangeConstraint.Start.GetName())
	}
	if rangeConstraint.Comparator != "<" {
		t.Fatalf("\"Angle\" should have comparator <. Got %s", rangeConstraint.Comparator)
	}
	if rangeConstraint.End.GetName() != "360" {
		t.Fatalf("\"Angle\" should have end 360. Got %s", rangeConstraint.End.GetName())
	}
}

func TestCollector_RangeConstrainedTypeWithConstantMultiplication(t *testing.T) {
	source := `
	type Radian = Float where range(0..<PI*2)
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

	namedNode, ok := table.GlobalScope.Lookup("Radian")
	if !ok {
		t.Fatalf("\"Radian\" not found in global scope")
	}
	radianDecl, ok := namedNode.(*ast.TypeDeclStmt)
	if !ok {
		t.Fatalf("\"Radian\" is not a TypeDeclStmt, got %T", namedNode)
	}
	if radianDecl.GetName() != "Radian" {
		t.Fatalf("\"Radian\" should have name \"Radian\". Got %s", radianDecl.GetName())
	}
	if len(radianDecl.Type.(types.ConstrainedType).Constraints) != 1 {
		t.Fatalf("\"Radian\" should have 1 constraint. Got %d", len(radianDecl.Type.(types.ConstrainedType).Constraints))
	}
	if !types.TypesEqual(radianDecl.Type.(types.ConstrainedType).Type, types.PrimitiveType{Name: "Float"}) {
		t.Fatalf("\"Radian\" should have type \"Float\". Got %v", radianDecl.Type.(types.ConstrainedType).Type)
	}
	rangeConstraint, ok := radianDecl.Type.(types.ConstrainedType).Constraints[0].(types.RangeConstraint)
	if !ok {
		t.Fatalf("\"Radian\" should have range constraint. Got %T", radianDecl.Type.(types.ConstrainedType).Constraints[0])
	}
	if rangeConstraint.Start == nil {
		t.Fatalf("\"Radian\" should have start. Got nil")
	}
	if rangeConstraint.Start.GetName() != "0" {
		t.Fatalf("\"Radian\" should have start 0. Got %s", rangeConstraint.Start.GetName())
	}
	if rangeConstraint.Comparator != "<" {
		t.Fatalf("\"Radian\" should have comparator <. Got %s", rangeConstraint.Comparator)
	}
	endExpr, ok := rangeConstraint.End.(*types.MathConstraintBinaryOpExpr)
	if !ok {
		t.Fatalf("\"Radian\" should have end expression. Got %T", rangeConstraint.End)
	}
	if endExpr.Operator != types.MathConstraintBinaryOpMul {
		t.Fatalf("\"Radian\" should have operator *. Got %s", endExpr.Operator)
	}
	if endExpr.Left.GetName() != "PI" {
		t.Fatalf("\"Radian\" should have left PI. Got %s", endExpr.Left.GetName())
	}
	if endExpr.Right.GetName() != "2" {
		t.Fatalf("\"Radian\" should have right 2. Got %s", endExpr.Right.GetName())
	}
}

func TestCollector_RangeConstrainedTypeWithVariableAddition(t *testing.T) {
	source := `
	let pi = 3.14159
	type Radian = Float where range(0..<pi+3)
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

	if len(program.Statements) != 2 {
		t.Fatalf("Expected 2 statements, got %d", len(program.Statements))
	}

	namedNode, ok := table.GlobalScope.Lookup("pi")
	if !ok {
		t.Fatalf("\"pi\" not found in global scope")
	}
	piDecl, ok := namedNode.(*ast.VarDeclStmt)
	if !ok {
		t.Fatalf("\"pi\" is not a VarDeclStmt, got %T", namedNode)
	}
	if piDecl.GetName() != "pi" {
		t.Fatalf("\"pi\" should have name \"pi\". Got %s", piDecl.GetName())
	}

	namedNode, ok = table.GlobalScope.Lookup("Radian")
	if !ok {
		t.Fatalf("\"Radian\" not found in global scope")
	}
	radianDecl, ok := namedNode.(*ast.TypeDeclStmt)
	if !ok {
		t.Fatalf("\"Radian\" is not a TypeDeclStmt, got %T", namedNode)
	}
	if radianDecl.GetName() != "Radian" {
		t.Fatalf("\"Radian\" should have name \"Radian\". Got %s", radianDecl.GetName())
	}
	if len(radianDecl.Type.(types.ConstrainedType).Constraints) != 1 {
		t.Fatalf("\"Radian\" should have 1 constraint. Got %d", len(radianDecl.Type.(types.ConstrainedType).Constraints))
	}
	if !types.TypesEqual(radianDecl.Type.(types.ConstrainedType).Type, types.PrimitiveType{Name: "Float"}) {
		t.Fatalf("\"Radian\" should have type \"Float\". Got %v", radianDecl.Type.(types.ConstrainedType).Type)
	}
	rangeConstraint, ok := radianDecl.Type.(types.ConstrainedType).Constraints[0].(types.RangeConstraint)
	if !ok {
		t.Fatalf("\"Radian\" should have range constraint. Got %T", radianDecl.Type.(types.ConstrainedType).Constraints[0])
	}
	if rangeConstraint.Start == nil {
		t.Fatalf("\"Radian\" should have start. Got nil")
	}
	if rangeConstraint.Start.GetName() != "0" {
		t.Fatalf("\"Radian\" should have start 0. Got %s", rangeConstraint.Start.GetName())
	}
	if rangeConstraint.Comparator != "<" {
		t.Fatalf("\"Radian\" should have comparator <. Got %s", rangeConstraint.Comparator)
	}
	endExpr, ok := rangeConstraint.End.(*types.MathConstraintBinaryOpExpr)
	if !ok {
		t.Fatalf("\"Radian\" should have end expression. Got %T", rangeConstraint.End)
	}
	if endExpr.Operator != types.MathConstraintBinaryOpAdd {
		t.Fatalf("\"Radian\" should have operator +. Got %s", endExpr.Operator)
	}
	if endExpr.Left.GetName() != "pi" {
		t.Fatalf("\"Radian\" should have left pi. Got %s", endExpr.Left.GetName())
	}
	if endExpr.Right.GetName() != "3" {
		t.Fatalf("\"Radian\" should have right 3. Got %s", endExpr.Right.GetName())
	}
}
