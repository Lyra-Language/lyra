package main

import (
	"fmt"

	"avrameisner.com/lyra-lsp/pkg/analyzer"
	"avrameisner.com/lyra-lsp/pkg/parser"
)

func main() {
	source := `
struct Point { x: Int, y: Int }

data Maybe<t> = Nil | Some(t)

data Tree<t> = Nil | Leaf(t) | Node { left: Tree, value: t, right: Tree }

def sum: (Int, Int) -> Int = (a, b) => a + b

def fib: (Int) -> Int = {
  (0) => 0,
  (1) => 1,
  (n) => fib(n-2) + fib(n-1),
}
`
	tree, err := parser.Parse(source)
	if err != nil {
		fmt.Println("Parse error:", err)
		return
	}

	collector := analyzer.NewCollector([]byte(source))
	table, errors := collector.Collect(tree.RootNode())

	if len(errors) > 0 {
		fmt.Println("Collection errors:")
		for _, e := range errors {
			fmt.Println("  -", e)
		}
	}

	fmt.Println("=== Types ===")
	for name, sym := range table.Types {
		fmt.Printf("  %s (line %d)\n", name, sym.Location.StartLine)
	}

	fmt.Println("\n=== Functions ===")
	for name, sym := range table.Functions {
		fmt.Printf("  %s (line %d, pure=%v, async=%v)\n",
			name, sym.Location.StartLine, sym.IsPure, sym.IsAsync)
		if sym.Signature != nil {
			fmt.Printf("    signature: %d params -> return\n", len(sym.Signature.Parameters))
		}
	}
}
