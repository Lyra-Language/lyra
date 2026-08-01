package typechecker_test

import (
	"strings"
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
	program, symTable, scopeTable, collectorErrors := c.Collect(tree.RootNode())
	if len(collectorErrors) > 0 {
		t.Fatalf("collector errors: %v", collectorErrors)
	}
	typeTable := typetable.New()
	tc := typechecker.New(symTable, scopeTable, typeTable)
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

// assertHasErrorContaining checks that *some* diagnostic mentions want. Use it when the
// program legitimately produces several — a definition cycle also trips the
// use-before-declaration check, and reports once per reference into the cycle — so
// assertErrorsAre's exact, ordered, full-set match would pin incidental diagnostics that
// have nothing to do with what the test is about.
func assertHasErrorContaining(t *testing.T, res checkResult, want string) {
	t.Helper()
	for _, e := range res.errors {
		if strings.Contains(e.Message, want) {
			return
		}
	}
	t.Errorf("expected a diagnostic containing %q, got %d:", want, len(res.errors))
	for _, e := range res.errors {
		t.Errorf("  %s", e.Message)
	}
}

// assertWarningsAre checks that only warnings (no errors) were produced and
// that their messages match expected in order.
func assertWarningsAre(t *testing.T, res checkResult, expected ...string) {
	t.Helper()
	var warnings []typechecker.TypeError
	for _, e := range res.errors {
		if e.Severity == typechecker.SeverityWarning {
			warnings = append(warnings, e)
		} else {
			t.Errorf("unexpected error (severity=error): %s", e.Message)
		}
	}
	if len(warnings) != len(expected) {
		t.Errorf("expected %d warning(s), got %d:", len(expected), len(warnings))
		for _, w := range warnings {
			t.Errorf("  %s", w.Message)
		}
		return
	}
	for idx, w := range warnings {
		if w.Message != expected[idx] {
			t.Errorf("expected warning: %q, got: %q", expected[idx], w.Message)
		}
	}
}
