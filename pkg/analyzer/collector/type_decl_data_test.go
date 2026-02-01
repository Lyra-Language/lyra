package collector

import (
	"testing"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/parser"
	"github.com/Lyra-Language/lyra/pkg/types"
)

func TestCollector_BasicDataType(t *testing.T) {
	source := `
	data ColorName = Red | Green | Blue
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

	if len(program.Statements) != 1 {
		t.Fatalf("Expected 1 statement, got %d", len(program.Statements))
	}

	// program.Print("")

	namedNode, ok := table.GlobalScope.Lookup("ColorName")
	if !ok {
		t.Fatalf("\"ColorName\" not found in global scope")
	}

	dataTypeDecl, ok := namedNode.(*ast.TypeDeclStmt)
	if !ok {
		t.Fatalf("\"ColorName\" is not a TypeDeclStmt, got %T", namedNode)
	}

	if dataTypeDecl.Name != "ColorName" {
		t.Fatalf("\"ColorName\" should have name \"ColorName\". Got %s", dataTypeDecl.Name)
	}

	if len(dataTypeDecl.GenericParams) != 0 {
		t.Fatalf("\"ColorName\" should have no generic parameters. Got %v", dataTypeDecl.GenericParams)
	}

	if len(dataTypeDecl.Type.(types.DataType).Constructors) != 3 {
		t.Fatalf("\"ColorName\" should have 3 constructors. Got %d", len(dataTypeDecl.Type.(types.DataType).Constructors))
	}

	if _, ok := dataTypeDecl.Type.(types.DataType).Constructors["Red"]; !ok {
		t.Fatalf("\"ColorName\" should have constructor \"Red\". Got %v", dataTypeDecl.Type.(types.DataType).Constructors)
	}

	if _, ok := dataTypeDecl.Type.(types.DataType).Constructors["Green"]; !ok {
		t.Fatalf("\"ColorName\" should have constructor \"Green\". Got %v", dataTypeDecl.Type.(types.DataType).Constructors)
	}

	if _, ok := dataTypeDecl.Type.(types.DataType).Constructors["Blue"]; !ok {
		t.Fatalf("\"ColorName\" should have constructor \"Blue\". Got %v", dataTypeDecl.Type.(types.DataType).Constructors)
	}
}

func TestCollector_DataTypeWithGenericParameter(t *testing.T) {
	source := `
	data Maybe<t> = Nil | Some t
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

	namedNode, ok := table.GlobalScope.Lookup("Maybe")
	if !ok {
		t.Fatalf("\"Maybe\" not found in global scope")
	}

	dataTypeDecl, ok := namedNode.(*ast.TypeDeclStmt)
	if !ok {
		t.Fatalf("\"Maybe\" is not a TypeDeclStmt, got %T", namedNode)
	}

	if dataTypeDecl.Name != "Maybe" {
		t.Fatalf("\"Maybe\" should have name \"Maybe\". Got %s", dataTypeDecl.Name)
	}

	if len(dataTypeDecl.GenericParams) != 1 {
		t.Fatalf("\"Maybe\" should have 1 generic parameter. Got %v", dataTypeDecl.GenericParams)
	}

	if dataTypeDecl.GenericParams[0] != "t" {
		t.Fatalf("\"Maybe\" should have generic parameter \"t\". Got %s", dataTypeDecl.GenericParams[0])
	}

	if len(dataTypeDecl.Type.(types.DataType).Constructors) != 2 {
		t.Fatalf("\"Maybe\" should have 2 constructors. Got %d", len(dataTypeDecl.Type.(types.DataType).Constructors))
	}

	if _, ok := dataTypeDecl.Type.(types.DataType).Constructors["Nil"]; !ok {
		t.Fatalf("\"Maybe\" should have constructor \"Nil\". Got %v", dataTypeDecl.Type.(types.DataType).Constructors)
	}

	if _, ok := dataTypeDecl.Type.(types.DataType).Constructors["Some"]; !ok {
		t.Fatalf("\"Maybe\" should have constructor \"Some\". Got %v", dataTypeDecl.Type.(types.DataType).Constructors)
	}

	if len(dataTypeDecl.Type.(types.DataType).Constructors["Some"].Params) != 1 {
		t.Fatalf("\"Maybe\" should have 1 parameter in constructor \"Some\". Got %d", len(dataTypeDecl.Type.(types.DataType).Constructors["Some"].Params))
	}

	if dataTypeDecl.Type.(types.DataType).Constructors["Some"].Params[0].GetName() != "t" {
		t.Fatalf("\"Maybe\" should have parameter \"t\" in constructor \"Some\". Got %s", dataTypeDecl.Type.(types.DataType).Constructors["Some"].Params[0].GetName())
	}

	if len(dataTypeDecl.Type.(types.DataType).Constructors["Some"].Fields) != 0 {
		t.Fatalf("\"Maybe\" should have no fields in constructor \"Some\". Got %d", len(dataTypeDecl.Type.(types.DataType).Constructors["Some"].Fields))
	}
}

func TestCollector_DataTypeWithStructFields(t *testing.T) {
	source := `
	data Tree<t> = Nil | Leaf t | Node { left: Tree, value: t, right: Tree }
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

	namedNode, ok := table.GlobalScope.Lookup("Tree")
	if !ok {
		t.Fatalf("\"Tree\" not found in global scope")
	}

	dataTypeDecl, ok := namedNode.(*ast.TypeDeclStmt)
	if !ok {
		t.Fatalf("\"Tree\" is not a TypeDeclStmt, got %T", namedNode)
	}

	if dataTypeDecl.Name != "Tree" {
		t.Fatalf("\"Tree\" should have name \"Tree\". Got %s", dataTypeDecl.Name)
	}

	if len(dataTypeDecl.GenericParams) != 1 {
		t.Fatalf("\"Tree\" should have 1 generic parameter. Got %v", dataTypeDecl.GenericParams)
	}

	if dataTypeDecl.GenericParams[0] != "t" {
		t.Fatalf("\"Tree\" should have generic parameter \"t\". Got %s", dataTypeDecl.GenericParams[0])
	}

	if len(dataTypeDecl.Type.(types.DataType).Constructors) != 3 {
		t.Fatalf("\"Tree\" should have 3 constructors. Got %d", len(dataTypeDecl.Type.(types.DataType).Constructors))
	}

	if _, ok := dataTypeDecl.Type.(types.DataType).Constructors["Nil"]; !ok {
		t.Fatalf("\"Tree\" should have constructor \"Nil\". Got %v", dataTypeDecl.Type.(types.DataType).Constructors)
	}

	if _, ok := dataTypeDecl.Type.(types.DataType).Constructors["Leaf"]; !ok {
		t.Fatalf("\"Tree\" should have constructor \"Leaf\". Got %v", dataTypeDecl.Type.(types.DataType).Constructors)
	}

	if _, ok := dataTypeDecl.Type.(types.DataType).Constructors["Node"]; !ok {
		t.Fatalf("\"Tree\" should have constructor \"Node\". Got %v", dataTypeDecl.Type.(types.DataType).Constructors)
	}

	if len(dataTypeDecl.Type.(types.DataType).Constructors["Node"].Fields) != 3 {
		t.Fatalf("\"Tree\" should have 3 fields in constructor \"Node\". Got %d", len(dataTypeDecl.Type.(types.DataType).Constructors["Node"].Fields))
	}

	if _, ok := dataTypeDecl.Type.(types.DataType).Constructors["Node"].Fields["left"]; !ok {
		t.Fatalf("\"Tree\" should have field \"left\" in constructor \"Node\". Got %v", dataTypeDecl.Type.(types.DataType).Constructors["Node"].Fields)
	}
	if dataTypeDecl.Type.(types.DataType).Constructors["Node"].Fields["left"].Type.GetName() != "Tree" {
		t.Fatalf("\"Tree\" should have field \"left\" in constructor \"Node\" with type \"Tree\". Got %s", dataTypeDecl.Type.(types.DataType).Constructors["Node"].Fields["left"].Type.GetName())
	}

	if _, ok := dataTypeDecl.Type.(types.DataType).Constructors["Node"].Fields["value"]; !ok {
		t.Fatalf("\"Tree\" should have field \"value\" in constructor \"Node\". Got %v", dataTypeDecl.Type.(types.DataType).Constructors["Node"].Fields)
	}
	if dataTypeDecl.Type.(types.DataType).Constructors["Node"].Fields["value"].Type.GetName() != "t" {
		t.Fatalf("\"Tree\" should have field \"value\" in constructor \"Node\" with type \"t\". Got %s", dataTypeDecl.Type.(types.DataType).Constructors["Node"].Fields["value"].Type.GetName())
	}

	if _, ok := dataTypeDecl.Type.(types.DataType).Constructors["Node"].Fields["right"]; !ok {
		t.Fatalf("\"Tree\" should have field \"right\" in constructor \"Node\". Got %v", dataTypeDecl.Type.(types.DataType).Constructors["Node"].Fields)
	}
	if dataTypeDecl.Type.(types.DataType).Constructors["Node"].Fields["right"].Type.GetName() != "Tree" {
		t.Fatalf("\"Tree\" should have field \"right\" in constructor \"Node\" with type \"Tree\". Got %s", dataTypeDecl.Type.(types.DataType).Constructors["Node"].Fields["right"].Type.GetName())
	}
}

func TestCollector_ComplexDataType(t *testing.T) {
	source := `
	data CSSColor =
		| ColorName CSSColorName
		| Hex HexStr
		| RGB { r: i8, g: i8, b: i8 }
		| HSL { hue: Hue, sat: float, light: float, alpha: Alpha }

	data Hue = HueDeg Angle | HueRadian Radian | HueTurn Turn
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

	if len(program.Statements) != 2 {
		t.Fatalf("Expected 2 statements, got %d", len(program.Statements))
	}

	// test CSSColor
	cssColorDecl, ok := table.GlobalScope.Lookup("CSSColor")
	if !ok {
		t.Fatalf("\"CSSColor\" not found in global scope")
	}
	if _, ok := cssColorDecl.(*ast.TypeDeclStmt); !ok {
		t.Fatalf("\"CSSColor\" is not a TypeDeclStmt, got %T", cssColorDecl)
	}
	if cssColorDecl.GetName() != "CSSColor" {
		t.Fatalf("\"CSSColor\" should have name \"CSSColor\". Got %s", cssColorDecl.GetName())
	}

	// test CSSColor constructors
	if len(cssColorDecl.(*ast.TypeDeclStmt).Type.(types.DataType).Constructors) != 4 {
		t.Fatalf("\"CSSColor\" should have 4 constructors. Got %d", len(cssColorDecl.(*ast.TypeDeclStmt).Type.(types.DataType).Constructors))
	}
	if _, ok := cssColorDecl.(*ast.TypeDeclStmt).Type.(types.DataType).Constructors["ColorName"]; !ok {
		t.Fatalf("\"CSSColor\" should have constructor \"ColorName\". Got %v", cssColorDecl.(*ast.TypeDeclStmt).Type.(types.DataType).Constructors)
	}
	if _, ok := cssColorDecl.(*ast.TypeDeclStmt).Type.(types.DataType).Constructors["Hex"]; !ok {
		t.Fatalf("\"CSSColor\" should have constructor \"Hex\". Got %v", cssColorDecl.(*ast.TypeDeclStmt).Type.(types.DataType).Constructors)
	}
	if _, ok := cssColorDecl.(*ast.TypeDeclStmt).Type.(types.DataType).Constructors["RGB"]; !ok {
		t.Fatalf("\"CSSColor\" should have constructor \"RGB\". Got %v", cssColorDecl.(*ast.TypeDeclStmt).Type.(types.DataType).Constructors)
	}
	if _, ok := cssColorDecl.(*ast.TypeDeclStmt).Type.(types.DataType).Constructors["HSL"]; !ok {
		t.Fatalf("\"CSSColor\" should have constructor \"HSL\". Got %v", cssColorDecl.(*ast.TypeDeclStmt).Type.(types.DataType).Constructors)
	}

	// test Hue
	hueDecl, ok := table.GlobalScope.Lookup("Hue")
	if !ok {
		t.Fatalf("\"Hue\" not found in global scope")
	}
	if _, ok := hueDecl.(*ast.TypeDeclStmt); !ok {
		t.Fatalf("\"Hue\" is not a TypeDeclStmt, got %T", hueDecl)
	}
	if hueDecl.GetName() != "Hue" {
		t.Fatalf("\"Hue\" should have name \"Hue\". Got %s", hueDecl.GetName())
	}
	// test Hue constructors
	if len(hueDecl.(*ast.TypeDeclStmt).Type.(types.DataType).Constructors) != 3 {
		t.Fatalf("\"Hue\" should have 3 constructors. Got %d", len(hueDecl.(*ast.TypeDeclStmt).Type.(types.DataType).Constructors))
	}
	if _, ok := hueDecl.(*ast.TypeDeclStmt).Type.(types.DataType).Constructors["HueDeg"]; !ok {
		t.Fatalf("\"Hue\" should have constructor \"HueDeg\". Got %v", hueDecl.(*ast.TypeDeclStmt).Type.(types.DataType).Constructors)
	}
	if _, ok := hueDecl.(*ast.TypeDeclStmt).Type.(types.DataType).Constructors["HueRadian"]; !ok {
		t.Fatalf("\"Hue\" should have constructor \"HueRadian\". Got %v", hueDecl.(*ast.TypeDeclStmt).Type.(types.DataType).Constructors)
	}
	if _, ok := hueDecl.(*ast.TypeDeclStmt).Type.(types.DataType).Constructors["HueTurn"]; !ok {
		t.Fatalf("\"Hue\" should have constructor \"HueTurn\". Got %v", hueDecl.(*ast.TypeDeclStmt).Type.(types.DataType).Constructors)
	}
}
