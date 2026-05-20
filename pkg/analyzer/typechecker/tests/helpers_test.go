package typechecker_test

import (
	"testing"

	"github.com/Lyra-Language/lyra/pkg/analyzer/collector"
	"github.com/Lyra-Language/lyra/pkg/analyzer/typechecker"
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/ast/symbols"
	"github.com/Lyra-Language/lyra/pkg/parser"
	"github.com/Lyra-Language/lyra/pkg/printer"
	"github.com/Lyra-Language/lyra/pkg/typetable"
)

type checkResult struct {
	program   *ast.Program
	symTable  *symbols.SymbolTable
	typeTable *typetable.TypeTable
	errors    []typechecker.TypeError
}

func parseCollectAndCheck(t *testing.T, source string, printTree bool) checkResult {
	t.Helper()
	tree, err := parser.Parse(source)
	// Print tree
	if printTree {
		p := printer.NewPrinter()
		p.Print(tree.RootNode())
	}
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	c := collector.NewCollector([]byte(source))
	program, symTable, collectorErrors := c.Collect(tree.RootNode())
	if len(collectorErrors) > 0 {
		t.Fatalf("collector errors: %v", collectorErrors)
	}
	typeTable := typetable.New()
	tc := typechecker.New(symTable, typeTable)
	errors := tc.Check(program)
	return checkResult{program, symTable, typeTable, errors}
}

func assertNoErrors(t *testing.T, res checkResult) {
	t.Helper()
	if len(res.errors) > 0 {
		t.Errorf("expected no type errors, got %d:", len(res.errors))
		for _, e := range res.errors {
			t.Errorf("  %s", e.Message)
		}
	}
}

func assertErrorsAre(t *testing.T, res checkResult, expected ...string) {
	t.Helper()
	if len(res.errors) != len(expected) {
		t.Errorf("expected %d type error(s), got %d:", len(expected), len(res.errors))
		for _, e := range res.errors {
			t.Errorf("  %s", e.Message)
		}
		return
	}
	for idx, e := range res.errors {
		if e.Message != expected[idx] {
			t.Errorf("expected: %q, got: %q", expected[idx], e.Message)
		}
	}
}
