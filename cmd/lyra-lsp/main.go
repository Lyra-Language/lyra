package main

import (
	"fmt"

	"github.com/Lyra-Language/lyra/pkg/analyzer"
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/parser"
	"github.com/Lyra-Language/lyra/pkg/printer"
)

func main() {
	source := `
def sum: (Int, Int) -> Int = (a, b) => a + b
let x: Float = sum(1, "2") // should produce two type errors
def say_hello: (Str) -> Str = (name) => 42 // should produce a type error (wrong return type)`

	tree, err := parser.Parse(source)
	if err != nil {
		fmt.Println("Parse error:", err)
		return
	}
	printer := printer.NewPrinter([]byte(source))
	printer.Print(tree.RootNode())

	collector := analyzer.NewCollector([]byte(source))
	program, table, errors := collector.Collect(tree.RootNode())
	// checker := analyzer.NewChecker([]byte(source), table)
	// typeErrors := checker.Check(tree.RootNode())

	if len(errors) > 0 {
		fmt.Println("Collection errors:")
		for _, e := range errors {
			fmt.Println("  -", e)
		}
	}
	// if len(typeErrors) > 0 {
	// 	fmt.Println("Type errors:")
	// 	for _, e := range typeErrors {
	// 		fmt.Println("  -", e)
	// 	}
	// }

	fmt.Printf("\n=== AST (%d statements) ===\n", len(program.Statements))

	fmt.Println("\n=== Types ===")
	for name, typeDecl := range table.Types {
		fmt.Printf("  %s (line %d)\n", name, typeDecl.Location.StartLine)
	}

	fmt.Println("\n=== Functions ===")
	for name, funcDef := range table.Functions {
		fmt.Printf("  %s (line %d, pure=%v, async=%v)\n", name, funcDef.Location.StartLine, funcDef.IsPure, funcDef.IsAsync)
		if funcDef.Signature != nil {
			fmt.Printf("    signature: %s\n", funcDef.Signature.GetName())
		}
		if funcDef.Clauses != nil {
			fmt.Printf("    clauses: %d\n", len(funcDef.Clauses))
		}
		for _, clause := range funcDef.Clauses {
			fmt.Printf("      parameters: %d\n", len(clause.Parameters))
			for _, param := range clause.Parameters {
				switch p := param.(type) {
				case *ast.IdentifierPattern:
					fmt.Printf("        %s\n", p.Name)
				case *ast.LiteralPattern:
					fmt.Printf("        %v\n", p.Value)
				}
			}
		}
	}
}
