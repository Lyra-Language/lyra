package main

import (
	"fmt"

	"github.com/Lyra-Language/lyra/pkg/analyzer"
	"github.com/Lyra-Language/lyra/pkg/parser"
	"github.com/Lyra-Language/lyra/pkg/printer"
	"github.com/Lyra-Language/lyra/pkg/symbols"
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
	table, errors := collector.Collect(tree.RootNode())
	checker := analyzer.NewChecker([]byte(source), table)
	typeErrors := checker.Check(tree.RootNode())

	if len(errors) > 0 {
		fmt.Println("Collection errors:")
		for _, e := range errors {
			fmt.Println("  -", e)
		}
	}
	if len(typeErrors) > 0 {
		fmt.Println("Type errors:")
		for _, e := range typeErrors {
			fmt.Println("  -", e)
		}
	}

	fmt.Println("=== Types ===")
	for name, sym := range table.Types {
		fmt.Printf("  %s (line %d)\n", name, sym.Location.StartLine)
	}

	fmt.Println("\n=== Functions ===")
	for name, sym := range table.Functions {
		fmt.Printf("  %s (line %d, pure=%v, async=%v)\n", name, sym.Location.StartLine, sym.IsPure, sym.IsAsync)
		fmt.Printf("    signature: %s\n", sym.Signature.GetName())
		if sym.Clauses != nil {
			fmt.Printf("    clauses: %d\n", len(sym.Clauses))
		}
		for _, clause := range sym.Clauses {
			fmt.Printf("      parameters: %d\n", len(clause.ParameterPatterns))
			for _, param := range clause.ParameterPatterns {
				switch p := param.(type) {
				case symbols.IdentifierPattern:
					fmt.Printf("        %s\n", p.Name)
				case symbols.LiteralPattern:
					fmt.Printf("        %v\n", p.Value)
				}
			}
		}
	}
}
