package analyzer

import (
	"testing"

	"github.com/Lyra-Language/lyra/pkg/parser"
)

func TestChecker(t *testing.T) {
	source := `let the_answer: Int = 42`

	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	// printer := printer.NewPrinter([]byte(source))
	// printer.Print(tree.RootNode())

	collector := NewCollector([]byte(source))
	table, errors := collector.Collect(tree.RootNode())
	if len(errors) > 0 {
		t.Fatalf("Collector errors: %v", errors)
	}
	// t.Logf("Table: %v", table)
	// t.Logf("Errors: %v", errors)

	checker := NewChecker([]byte(source), table)
	typeErrors := checker.Check(tree.RootNode())
	if len(typeErrors) > 0 {
		t.Fatalf("Checker type errors: %v", typeErrors)
	}
}

func TestChecker_TypeErrors(t *testing.T) {
	source := `let the_answer: Int = "42"`

	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}

	collector := NewCollector([]byte(source))
	table, errors := collector.Collect(tree.RootNode())
	if len(errors) > 0 {
		t.Fatalf("Collector errors: %v", errors)
	}

	checker := NewChecker([]byte(source), table)
	typeErrors := checker.Check(tree.RootNode())
	if len(typeErrors) == 0 {
		t.Fatalf("Checker should have type errors")
	}
	if len(typeErrors) != 1 {
		t.Fatalf("Checker should have 1 type error. Got %d", len(typeErrors))
	}
	expectedMessage := "cannot assign Str to variable the_answer of type Int"
	if typeErrors[0].Message != expectedMessage {
		t.Fatalf("Checker type error message is incorrect. Expected \"%s\". Got \"%s\"", expectedMessage, typeErrors[0].Message)
	}
}
