package collector

import (
	"testing"

	"github.com/Lyra-Language/lyra/pkg/parser"
)

func TestCollector_BasicDataType(t *testing.T) {
	source := `pub stack data ColorName = Red | Green | Blue`
	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	collector := NewCollector([]byte(source))
	program, _, errors := collector.Collect(tree.RootNode())
	if len(errors) > 0 {
		t.Fatalf("Collector errors: %v", errors)
	}
	got := captureProgramPrint(program)
	want := `
	Program(1 statements) {
		TypeDeclStmt(ColorName) {
			IsPublic: true
			Type: {
				DataType(ColorName) {
					DataTypeConstructor(Red)
					DataTypeConstructor(Green)
					DataTypeConstructor(Blue)
				}
			}
		}
	}`
	if msg := cmpOutput(got, want); msg != "" {
		t.Fatal(msg)
	}
}

func TestCollector_DataTypeWithGenericParameter(t *testing.T) {
	source := `pub heap data Maybe<t> = Nil | Some t`
	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	collector := NewCollector([]byte(source))
	program, _, errors := collector.Collect(tree.RootNode())
	if len(errors) > 0 {
		t.Fatalf("Collector errors: %v", errors)
	}
	got := captureProgramPrint(program)
	want := `
	Program(1 statements) {
		TypeDeclStmt(Maybe) {
			IsPublic: true
			GenericParams: [t]
			Type: {
				DataType(Maybe) {
					DataTypeConstructor(Nil)
					DataTypeConstructor(Some) {
						Params: (
							GenericType(t)
						)
					}
				}
			}
		}
	}`
	if msg := cmpOutput(got, want); msg != "" {
		t.Fatal(msg)
	}
}

func TestCollector_DataTypeWithStructFields(t *testing.T) {
	source := `data Tree<t> = Nil | Leaf t | Node { left: Tree, value: t, right: Tree }`
	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	collector := NewCollector([]byte(source))
	program, _, errors := collector.Collect(tree.RootNode())
	if len(errors) > 0 {
		t.Fatalf("Collector errors: %v", errors)
	}
	got := captureProgramPrint(program)
	want := `
	Program(1 statements) {
		TypeDeclStmt(Tree) {
			GenericParams: [t]
			Type: {
				DataType(Tree) {
					DataTypeConstructor(Nil)
					DataTypeConstructor(Leaf) {
						Params: (
							GenericType(t)
						)
					}
					DataTypeConstructor(Node) {
						Fields: {
							left: Tree
							value: t
							right: Tree
						}
					}
				}
			}
		}
	}`
	if msg := cmpOutput(got, want); msg != "" {
		t.Fatal(msg)
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
	program, _, errors := collector.Collect(tree.RootNode())
	if len(errors) > 0 {
		t.Fatalf("Collector errors: %v", errors)
	}
	got := captureProgramPrint(program)
	want := `
	Program(2 statements) {
		TypeDeclStmt(CSSColor) {
			Type: {
				DataType(CSSColor) {
					DataTypeConstructor(ColorName) {
						Params: (
							CSSColorName
						)
					}
					DataTypeConstructor(Hex) {
						Params: (
							HexStr
						)
					}
					DataTypeConstructor(RGB) {
						Fields: {
							r: i8
							g: i8
							b: i8
						}
					}
					DataTypeConstructor(HSL) {
						Fields: {
							hue: Hue
							sat: float
							light: float
							alpha: Alpha
						}
					}
				}
			}
		}
		TypeDeclStmt(Hue) {
			Type: {
				DataType(Hue) {
					DataTypeConstructor(HueDeg) {
						Params: (
							Angle
						)
					}
					DataTypeConstructor(HueRadian) {
						Params: (
							Radian
						)
					}
					DataTypeConstructor(HueTurn) {
						Params: (
							Turn
						)
					}
				}
			}
		}
	}`
	if msg := cmpOutput(got, want); msg != "" {
		t.Fatal(msg)
	}
}
