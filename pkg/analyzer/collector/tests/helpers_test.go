package collector_test

import (
	"bytes"
	"io"
	"os"
	"testing"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/ast/symbols"
	"github.com/Lyra-Language/lyra/pkg/parser"
	"github.com/Lyra-Language/lyra/pkg/printer"

	"github.com/Lyra-Language/lyra/pkg/analyzer/collector"
)

// parseAndCollect parses source, runs the collector, and returns the resulting
// program and symbol table. The test is failed immediately on any parse or
// collector error.
func parseAndCollect(t *testing.T, source string) (*ast.Program, *symbols.SymbolTable) {
	t.Helper()
	return parseAndCollectFull(t, source, false)
}

func parseAndCollectFull(t *testing.T, source string, printTree bool) (*ast.Program, *symbols.SymbolTable) {
	t.Helper()
	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	if printTree {
		p := printer.NewPrinter([]byte(source))
		p.Print(tree.RootNode())
	}
	c := collector.NewCollector([]byte(source))
	program, table, errors := c.Collect(tree.RootNode())
	if len(errors) > 0 {
		t.Fatalf("Collector errors: %v", errors)
	}
	return program, table
}

// captureProgramPrint runs program.Print("") with stdout redirected and returns the output.
// This lets tests assert on the full collected AST shape without manual type assertions.
func captureProgramPrint(program *ast.Program) string {
	r, w, err := os.Pipe()
	if err != nil {
		panic(err)
	}
	old := os.Stdout
	os.Stdout = w
	program.Print("")
	w.Close()
	os.Stdout = old
	var buf bytes.Buffer
	io.Copy(&buf, r)
	return buf.String()
}
