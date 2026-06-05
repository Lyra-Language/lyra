package checker_test

import (
	"testing"

	"github.com/Lyra-Language/lyra/pkg/analyzer/checker"
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector"
	"github.com/Lyra-Language/lyra/pkg/parser"
)

func parseCollectAndCheckYield(t *testing.T, source string) []checker.YieldOutsideGeneratorError {
	t.Helper()
	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	c := collector.NewCollector([]byte(source))
	program, _, _, _ := c.Collect(tree.RootNode())
	return checker.CheckYieldOutsideGenerator(program)
}

func assertNoYieldErrors(t *testing.T, errs []checker.YieldOutsideGeneratorError) {
	t.Helper()
	if len(errs) > 0 {
		t.Errorf("expected no errors, got %d: %v", len(errs), errs)
	}
}

func assertYieldErrorCount(t *testing.T, errs []checker.YieldOutsideGeneratorError, count int) {
	t.Helper()
	if len(errs) != count {
		t.Errorf("expected %d error(s), got %d: %v", count, len(errs), errs)
	}
}

// TestYield_NoDiag_InsideGenerator verifies that yield inside a generator is valid.
func TestYield_NoDiag_InsideGenerator(t *testing.T) {
	source := `
let counter = gen () -> Gen<i32> => {
    yield 1
    yield 2
}`
	assertNoYieldErrors(t, parseCollectAndCheckYield(t, source))
}

// TestYield_NoDiag_YieldFromInsideGenerator verifies that yield from inside a generator is valid.
func TestYield_NoDiag_YieldFromInsideGenerator(t *testing.T) {
	source := `
let gen = gen () -> Gen<i32> => {
    yield from other
}`
	assertNoYieldErrors(t, parseCollectAndCheckYield(t, source))
}

// TestYield_Diag_YieldTopLevel verifies that a top-level yield is rejected.
func TestYield_Diag_YieldTopLevel(t *testing.T) {
	source := `yield 1`
	errs := parseCollectAndCheckYield(t, source)
	assertYieldErrorCount(t, errs, 1)
	want := "yield expression outside of a generator function"
	if errs[0].Message != want {
		t.Errorf("message = %q, want %q", errs[0].Message, want)
	}
}

// TestYield_Diag_YieldFromTopLevel verifies that a top-level yield from is rejected.
func TestYield_Diag_YieldFromTopLevel(t *testing.T) {
	source := `yield from other`
	errs := parseCollectAndCheckYield(t, source)
	assertYieldErrorCount(t, errs, 1)
	want := "yield from expression outside of a generator function"
	if errs[0].Message != want {
		t.Errorf("message = %q, want %q", errs[0].Message, want)
	}
}

// TestYield_Diag_YieldInsideNonGeneratorFunction verifies that yield inside a
// regular (non-generator) function is rejected.
func TestYield_Diag_YieldInsideNonGeneratorFunction(t *testing.T) {
	source := `
let f = (x: i32) -> i32 => {
    yield x
}`
	assertYieldErrorCount(t, parseCollectAndCheckYield(t, source), 1)
}

// TestYield_Diag_YieldInsideNestedNonGenerator verifies that a yield inside a
// non-generator lambda nested inside a generator is rejected.
func TestYield_Diag_YieldInsideNestedNonGenerator(t *testing.T) {
	source := `
let outer = gen () -> Gen<i32> => {
    let inner = (x: i32) -> i32 => {
        yield x
    }
    yield 1
}`
	assertYieldErrorCount(t, parseCollectAndCheckYield(t, source), 1)
}

// TestYield_NoDiag_GeneratorNestedInsideNonGenerator verifies that a generator
// nested inside a regular function can use yield.
func TestYield_NoDiag_GeneratorNestedInsideNonGenerator(t *testing.T) {
	source := `
let f = (x: i32) -> i32 => {
    let inner = gen () -> Gen<i32> => {
        yield 1
    }
    0
}`
	assertNoYieldErrors(t, parseCollectAndCheckYield(t, source))
}

// TestYield_Diag_MultipleYieldsOutside verifies that multiple bad yields each produce an error.
func TestYield_Diag_MultipleYieldsOutside(t *testing.T) {
	source := `
yield 1
yield 2
yield from other`
	assertYieldErrorCount(t, parseCollectAndCheckYield(t, source), 3)
}
