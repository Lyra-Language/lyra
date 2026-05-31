package collector_test

import (
	"path/filepath"
	"strings"
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
		p := printer.NewPrinter()
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
	return printer.PrintAST(program)
}

// parseAndCollectErrors parses source, runs the collector, and returns all
// collector errors without failing the test. It is the counterpart to
// parseAndCollect for tests that deliberately expect errors.
func parseAndCollectErrors(t *testing.T, source string) []error {
	t.Helper()
	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	c := collector.NewCollector([]byte(source))
	_, _, errors := c.Collect(tree.RootNode())
	return errors
}

// assertCollectorErrorContains verifies that at least one of the collected
// errors has a message that contains the given substring.
func assertCollectorErrorContains(t *testing.T, errors []error, substr string) {
	t.Helper()
	for _, e := range errors {
		if strings.Contains(e.Error(), substr) {
			return
		}
	}
	t.Errorf("expected a collector error containing %q, got: %v", substr, errors)
}

func runGoldenTest(t *testing.T, source string, goldenFileName string) {
	t.Helper()
	program, _ := parseAndCollect(t, source)
	got := captureProgramPrint(program)
	checkGolden(t, got, filepath.Join("testdata", goldenFileName+".golden"))
}
