package collector

import (
	"testing"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/parser"
	"github.com/Lyra-Language/lyra/pkg/types"
)

func TestCollector_BasicConstrainedTypeWithoutConstraints(t *testing.T) {
	source := `
	type Angle = float
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
	if len(angleDecl.Type.(*types.ConstrainedType).Constraints) != 0 {
		t.Fatalf("\"Angle\" should have no constraints. Got %d", len(angleDecl.Type.(*types.ConstrainedType).Constraints))
	}
	if !types.TypesEqual(angleDecl.Type.(*types.ConstrainedType).Type, types.PrimitiveType{Name: "float"}) {
		t.Fatalf("\"Angle\" should have type \"float\". Got %v", angleDecl.Type.(*types.ConstrainedType).Type)
	}
}

func TestCollector_ConstrainedTypeWithLiteralUnionConstraintWithStrings(t *testing.T) {
	source := `
	type Color = string where values("red", "green", "blue")
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

	namedNode, ok := table.GlobalScope.Lookup("Color")
	if !ok {
		t.Fatalf("\"Color\" not found in global scope")
	}
	colorDecl, ok := namedNode.(*ast.TypeDeclStmt)
	if !ok {
		t.Fatalf("\"Color\" is not a TypeDeclStmt, got %T", namedNode)
	}
	if colorDecl.GetName() != "Color" {
		t.Fatalf("\"Color\" should have name \"Color\". Got %s", colorDecl.GetName())
	}
	if len(colorDecl.Type.(*types.ConstrainedType).Constraints) != 1 {
		t.Fatalf("\"Color\" should have 1 constraint. Got %d", len(colorDecl.Type.(*types.ConstrainedType).Constraints))
	}
	if !types.TypesEqual(colorDecl.Type.(*types.ConstrainedType).Type, types.PrimitiveType{Name: types.String}) {
		t.Fatalf("\"Color\" should have type \"Str\". Got %v", colorDecl.Type.(*types.ConstrainedType).Type)
	}
	literalUnionConstraint, ok := colorDecl.Type.(*types.ConstrainedType).Constraints[0].(*types.LiteralUnionConstraint)
	if !ok {
		t.Fatalf("\"Color\" should have literal union constraint. Got %T", colorDecl.Type.(*types.ConstrainedType).Constraints[0])
	}
	if len(literalUnionConstraint.Values) != 3 {
		t.Fatalf("\"Color\" should have 3 values. Got %d", len(literalUnionConstraint.Values))
	}
	if literalUnionConstraint.Values[0].(*ast.StringLiteralExpr).Value != "red" {
		t.Fatalf("\"Color\" should have value \"red\". Got %s", literalUnionConstraint.Values[0].(*ast.StringLiteralExpr).Value)
	}
	if literalUnionConstraint.Values[1].(*ast.StringLiteralExpr).Value != "green" {
		t.Fatalf("\"Color\" should have value \"green\". Got %s", literalUnionConstraint.Values[1].(*ast.StringLiteralExpr).Value)
	}
	if literalUnionConstraint.Values[2].(*ast.StringLiteralExpr).Value != "blue" {
		t.Fatalf("\"Color\" should have value \"blue\". Got %s", literalUnionConstraint.Values[2].(*ast.StringLiteralExpr).Value)
	}
}

func TestCollector_ConstrainedTypeWithLiteralUnionConstraintWithNumbers(t *testing.T) {
	source := `
	type Status = i32 where values(200, 404, 500)
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
	namedNode, ok := table.GlobalScope.Lookup("Status")
	if !ok {
		t.Fatalf("\"Status\" not found in global scope")
	}
	statusDecl, ok := namedNode.(*ast.TypeDeclStmt)
	if !ok {
		t.Fatalf("\"Status\" is not a TypeDeclStmt, got %T", namedNode)
	}
	if statusDecl.GetName() != "Status" {
		t.Fatalf("\"Status\" should have name \"Status\". Got %s", statusDecl.GetName())
	}
	if len(statusDecl.Type.(*types.ConstrainedType).Constraints) != 1 {
		t.Fatalf("\"Status\" should have 1 constraint. Got %d", len(statusDecl.Type.(*types.ConstrainedType).Constraints))
	}
	if !types.TypesEqual(statusDecl.Type.(*types.ConstrainedType).Type, types.PrimitiveType{Name: types.Int32}) {
		t.Fatalf("\"Status\" should have type \"i32\". Got %v", statusDecl.Type.(*types.ConstrainedType).Type)
	}
	literalUnionConstraint, ok := statusDecl.Type.(*types.ConstrainedType).Constraints[0].(*types.LiteralUnionConstraint)
	if !ok {
		t.Fatalf("\"Status\" should have literal union constraint. Got %T", statusDecl.Type.(*types.ConstrainedType).Constraints[0])
	}
	if len(literalUnionConstraint.Values) != 3 {
		t.Fatalf("\"Status\" should have 3 values. Got %d", len(literalUnionConstraint.Values))
	}
	if literalUnionConstraint.Values[0].(*ast.IntegerLiteralExpr).Value != 200 {
		t.Fatalf("\"Status\" should have value 200. Got %v", literalUnionConstraint.Values[0])
	}
	if literalUnionConstraint.Values[1].(*ast.IntegerLiteralExpr).Value != 404 {
		t.Fatalf("\"Status\" should have value 404. Got %v", literalUnionConstraint.Values[1])
	}
	if literalUnionConstraint.Values[2].(*ast.IntegerLiteralExpr).Value != 500 {
		t.Fatalf("\"Status\" should have value 500. Got %v", literalUnionConstraint.Values[2])
	}
}

func TestCollector_SimpleRangeConstrainedType(t *testing.T) {
	source := `
	type Angle = float where range(0..<360)
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
	if len(angleDecl.Type.(*types.ConstrainedType).Constraints) != 1 {
		t.Fatalf("\"Angle\" should have 1 constraint. Got %d", len(angleDecl.Type.(*types.ConstrainedType).Constraints))
	}
	if !types.TypesEqual(angleDecl.Type.(*types.ConstrainedType).Type, types.PrimitiveType{Name: "float"}) {
		t.Fatalf("\"Angle\" should have type \"float\". Got %v", angleDecl.Type.(*types.ConstrainedType).Type)
	}
	rangeConstraint, ok := angleDecl.Type.(*types.ConstrainedType).Constraints[0].(*types.RangeConstraint)
	if !ok {
		t.Fatalf("\"Angle\" should have range constraint. Got %T", angleDecl.Type.(*types.ConstrainedType).Constraints[0])
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
	type Radian = float where range(0..<PI*2)
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
	if len(radianDecl.Type.(*types.ConstrainedType).Constraints) != 1 {
		t.Fatalf("\"Radian\" should have 1 constraint. Got %d", len(radianDecl.Type.(*types.ConstrainedType).Constraints))
	}
	if !types.TypesEqual(radianDecl.Type.(*types.ConstrainedType).Type, types.PrimitiveType{Name: "float"}) {
		t.Fatalf("\"Radian\" should have type \"float\". Got %v", radianDecl.Type.(*types.ConstrainedType).Type)
	}
	rangeConstraint, ok := radianDecl.Type.(*types.ConstrainedType).Constraints[0].(*types.RangeConstraint)
	if !ok {
		t.Fatalf("\"Radian\" should have range constraint. Got %T", radianDecl.Type.(*types.ConstrainedType).Constraints[0])
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

func TestCollector_RangeConstrainedTypeWithVariableAdditionAndSubtraction(t *testing.T) {
	source := `
	let pi = 3.14159
	type Radian = float where range(pi-3..<pi+3)
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
	if len(radianDecl.Type.(*types.ConstrainedType).Constraints) != 1 {
		t.Fatalf("\"Radian\" should have 1 constraint. Got %d", len(radianDecl.Type.(*types.ConstrainedType).Constraints))
	}
	if !types.TypesEqual(radianDecl.Type.(*types.ConstrainedType).Type, types.PrimitiveType{Name: "float"}) {
		t.Fatalf("\"Radian\" should have type \"float\". Got %v", radianDecl.Type.(*types.ConstrainedType).Type)
	}
	rangeConstraint, ok := radianDecl.Type.(*types.ConstrainedType).Constraints[0].(*types.RangeConstraint)
	if !ok {
		t.Fatalf("\"Radian\" should have range constraint. Got %T", radianDecl.Type.(*types.ConstrainedType).Constraints[0])
	}
	if rangeConstraint.Start == nil {
		t.Fatalf("\"Radian\" should have start. Got nil")
	}
	if rangeConstraint.Start.GetName() != "pi - 3" {
		t.Fatalf("\"Radian\" should have start pi - 3. Got %s", rangeConstraint.Start.GetName())
	}
	if rangeConstraint.Start.(*types.MathConstraintBinaryOpExpr).Operator != types.MathConstraintBinaryOpSub {
		t.Fatalf("\"Radian\" should have operator -. Got %s", rangeConstraint.Start.(*types.MathConstraintBinaryOpExpr).Operator)
	}
	if rangeConstraint.Start.(*types.MathConstraintBinaryOpExpr).Left.GetName() != "pi" {
		t.Fatalf("\"Radian\" should have left pi. Got %s", rangeConstraint.Start.(*types.MathConstraintBinaryOpExpr).Left.GetName())
	}
	if rangeConstraint.Start.(*types.MathConstraintBinaryOpExpr).Right.GetName() != "3" {
		t.Fatalf("\"Radian\" should have right 3. Got %s", rangeConstraint.Start.(*types.MathConstraintBinaryOpExpr).Right.GetName())
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

func TestCollector_MultipleConstraints(t *testing.T) {
	source := `
	type Radian = float where range(0..<PI*2), precision(0.01)
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
	if len(radianDecl.Type.(*types.ConstrainedType).Constraints) != 2 {
		t.Fatalf("\"Radian\" should have 2 constraints. Got %d", len(radianDecl.Type.(*types.ConstrainedType).Constraints))
	}
	if !types.TypesEqual(radianDecl.Type.(*types.ConstrainedType).Type, types.PrimitiveType{Name: "float"}) {
		t.Fatalf("\"Radian\" should have type \"float\". Got %v", radianDecl.Type.(*types.ConstrainedType).Type)
	}
	rangeConstraint, ok := radianDecl.Type.(*types.ConstrainedType).Constraints[0].(*types.RangeConstraint)
	if !ok {
		t.Fatalf("\"Radian\" should have range constraint. Got %T", radianDecl.Type.(*types.ConstrainedType).Constraints[0])
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
	if rangeConstraint.End == nil {
		t.Fatalf("\"Radian\" should have end. Got nil")
	}
	if rangeConstraint.End.GetName() != "PI * 2" {
		t.Fatalf("\"Radian\" should have end PI * 2. Got %s", rangeConstraint.End.GetName())
	}
	precisionConstraint, ok := radianDecl.Type.(*types.ConstrainedType).Constraints[1].(*types.PrecisionConstraint)
	if !ok {
		t.Fatalf("\"Radian\" should have precision constraint. Got %T", radianDecl.Type.(*types.ConstrainedType).Constraints[1])
	}
	if precisionConstraint.Value.GetName() != "0.01" {
		t.Fatalf("\"Radian\" should have value 0.01. Got %s", precisionConstraint.Value.GetName())
	}
	if precisionConstraint.RoundingMode != types.RoundingModeNearestEven {
		t.Fatalf("\"Radian\" should have rounding mode nearest even. Got %s", precisionConstraint.RoundingMode)
	}
}

func TestCollector_StepConstrainedType(t *testing.T) {
	source := `
	type CompassHeading = int where range(0..<360), step(15)
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
	namedNode, ok := table.GlobalScope.Lookup("CompassHeading")
	if !ok {
		t.Fatalf("\"CompassHeading\" not found in global scope")
	}
	compassHeadingDecl, ok := namedNode.(*ast.TypeDeclStmt)
	if !ok {
		t.Fatalf("\"CompassHeading\" is not a TypeDeclStmt, got %T", namedNode)
	}
	if compassHeadingDecl.GetName() != "CompassHeading" {
		t.Fatalf("\"CompassHeading\" should have name \"CompassHeading\". Got %s", compassHeadingDecl.GetName())
	}
	if len(compassHeadingDecl.Type.(*types.ConstrainedType).Constraints) != 2 {
		t.Fatalf("\"CompassHeading\" should have 2 constraints. Got %d", len(compassHeadingDecl.Type.(*types.ConstrainedType).Constraints))
	}
	if !types.TypesEqual(compassHeadingDecl.Type.(*types.ConstrainedType).Type, types.PrimitiveType{Name: "int"}) {
		t.Fatalf("\"CompassHeading\" should have type \"int\". Got %v", compassHeadingDecl.Type.(*types.ConstrainedType).Type)
	}
	rangeConstraint, ok := compassHeadingDecl.Type.(*types.ConstrainedType).Constraints[0].(*types.RangeConstraint)
	if !ok {
		t.Fatalf("\"CompassHeading\" should have range constraint. Got %T", compassHeadingDecl.Type.(*types.ConstrainedType).Constraints[0])
	}
	if rangeConstraint.Start == nil {
		t.Fatalf("\"CompassHeading\" should have start. Got nil")
	}
	if rangeConstraint.Start.GetName() != "0" {
		t.Fatalf("\"CompassHeading\" should have start 0. Got %s", rangeConstraint.Start.GetName())
	}
	if rangeConstraint.Comparator != "<" {
		t.Fatalf("\"CompassHeading\" should have comparator <. Got %s", rangeConstraint.Comparator)
	}
	if rangeConstraint.End == nil {
		t.Fatalf("\"CompassHeading\" should have end. Got nil")
	}
	if rangeConstraint.End.GetName() != "360" {
		t.Fatalf("\"CompassHeading\" should have end 360. Got %s", rangeConstraint.End.GetName())
	}
	stepConstraint, ok := compassHeadingDecl.Type.(*types.ConstrainedType).Constraints[1].(*types.StepConstraint)
	if !ok {
		t.Fatalf("\"CompassHeading\" should have step constraint. Got %T", compassHeadingDecl.Type.(*types.ConstrainedType).Constraints[1])
	}
	if stepConstraint.Value.GetName() != "15" {
		t.Fatalf("\"CompassHeading\" should have value 15. Got %s", stepConstraint.Value.GetName())
	}
	if !types.TypesEqual(stepConstraint.Value.(*types.MathConstraintLiteralExpr).Type, types.PrimitiveType{Name: types.Int}) {
		t.Fatalf("\"CompassHeading\" should have value type int. Got %v", stepConstraint.Value.(*types.MathConstraintLiteralExpr).Type)
	}
	if stepConstraint.Value.(*types.MathConstraintLiteralExpr).Value != int64(15) {
		t.Fatalf("\"CompassHeading\" should have value 15. Got %v", stepConstraint.Value.(*types.MathConstraintLiteralExpr).Value)
	}
}

func TestCollector_PatternConstrainedType(t *testing.T) {
	source := `
	type HexStr = string where pattern(r/^#(?:[0-9a-fA-F]{3}){1,2}$/)
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
	namedNode, ok := table.GlobalScope.Lookup("HexStr")
	if !ok {
		t.Fatalf("\"HexStr\" not found in global scope")
	}
	hexStrDecl, ok := namedNode.(*ast.TypeDeclStmt)
	if !ok {
		t.Fatalf("\"HexStr\" is not a TypeDeclStmt, got %T", namedNode)
	}
	if hexStrDecl.GetName() != "HexStr" {
		t.Fatalf("\"HexStr\" should have name \"HexStr\". Got %s", hexStrDecl.GetName())
	}
	if len(hexStrDecl.Type.(*types.ConstrainedType).Constraints) != 1 {
		t.Fatalf("\"HexStr\" should have 1 constraint. Got %d", len(hexStrDecl.Type.(*types.ConstrainedType).Constraints))
	}
	if !types.TypesEqual(hexStrDecl.Type.(*types.ConstrainedType).Type, types.PrimitiveType{Name: types.String}) {
		t.Fatalf("\"HexStr\" should have type \"Str\". Got %v", hexStrDecl.Type.(*types.ConstrainedType).Type)
	}
	patternConstraint, ok := hexStrDecl.Type.(*types.ConstrainedType).Constraints[0].(*types.PatternConstraint)
	if !ok {
		t.Fatalf("\"HexStr\" should have pattern constraint. Got %T", hexStrDecl.Type.(*types.ConstrainedType).Constraints[0])
	}
	if patternConstraint.Pattern != "r/^#(?:[0-9a-fA-F]{3}){1,2}$/" {
		t.Fatalf("\"HexStr\" should have pattern r/^#(?:[0-9a-fA-F]{3}){1,2}$/. Got %s", patternConstraint.Pattern)
	}
}
