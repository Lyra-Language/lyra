package checker_test

import (
	"testing"

	"github.com/Lyra-Language/lyra/pkg/analyzer/checker"
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector"
	"github.com/Lyra-Language/lyra/pkg/parser"
)

func parseCollectAndCheckReturn(t *testing.T, source string) []checker.ReturnOutsideFunctionError {
	t.Helper()
	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	c := collector.NewCollector([]byte(source))
	program, _, _, _ := c.Collect(tree.RootNode())
	return checker.CheckReturnOutsideFunction(program)
}

func assertNoReturnErrors(t *testing.T, errs []checker.ReturnOutsideFunctionError) {
	t.Helper()
	if len(errs) > 0 {
		t.Errorf("expected no errors, got %d: %v", len(errs), errs)
	}
}

func assertReturnErrorCount(t *testing.T, errs []checker.ReturnOutsideFunctionError, count int) {
	t.Helper()
	if len(errs) != count {
		t.Errorf("expected %d error(s), got %d: %v", count, len(errs), errs)
	}
}

// TestReturn_NoDiag_InsideFunction verifies that return inside a function body
// is accepted without error.
func TestReturn_NoDiag_InsideFunction(t *testing.T) {
	source := `
let f = (x: i32) -> i32 => {
    return x
}`
	errs := parseCollectAndCheckReturn(t, source)
	assertNoReturnErrors(t, errs)
}

// TestReturn_NoDiag_InsideNestedFunction verifies that a return inside a
// nested lambda is accepted.
func TestReturn_NoDiag_InsideNestedFunction(t *testing.T) {
	source := `
let outer = (x: i32) -> i32 => {
    let inner = (y: i32) -> i32 => {
        return y
    }
    return x
}`
	errs := parseCollectAndCheckReturn(t, source)
	assertNoReturnErrors(t, errs)
}

// TestReturn_Diag_TopLevel verifies that a top-level return is rejected.
func TestReturn_Diag_TopLevel(t *testing.T) {
	source := `
let x = 5
return x`
	errs := parseCollectAndCheckReturn(t, source)
	assertReturnErrorCount(t, errs, 1)
}

// TestReturn_Diag_InsideTopLevelBlock verifies that a return inside a top-level
// block expression (which is not a function body) is rejected.
func TestReturn_Diag_InsideTopLevelBlock(t *testing.T) {
	source := `
let result = {
    return 42
}`
	errs := parseCollectAndCheckReturn(t, source)
	assertReturnErrorCount(t, errs, 1)
}

// TestReturn_NoDiag_InsideFunctionBlock verifies that a return inside a block
// that is itself inside a function body is accepted.
func TestReturn_NoDiag_InsideFunctionBlock(t *testing.T) {
	source := `
let f = (x: i32) -> i32 => {
    let r = {
        return x
    }
    r
}`
	errs := parseCollectAndCheckReturn(t, source)
	assertNoReturnErrors(t, errs)
}

// TestReturn_Diag_MultipleTopLevel verifies that multiple top-level returns
// each produce an error.
func TestReturn_Diag_MultipleTopLevel(t *testing.T) {
	source := `
return 1
return 2`
	errs := parseCollectAndCheckReturn(t, source)
	assertReturnErrorCount(t, errs, 2)
}

// TestReturn_NoDiag_InsideForLoop verifies that return inside a for loop that
// is inside a function is accepted.
func TestReturn_NoDiag_InsideForLoop(t *testing.T) {
	source := `
let f = (xs: [i32]) -> i32 => {
    for var i = 0; i < 10; i + 1 {
        return i
    }
    return 0
}`
	errs := parseCollectAndCheckReturn(t, source)
	assertNoReturnErrors(t, errs)
}

// TestReturn_Diag_InsideTopLevelForLoop verifies that return inside a top-level
// for loop (not inside any function) is rejected.
func TestReturn_Diag_InsideTopLevelForLoop(t *testing.T) {
	source := `
for var i = 0; i < 10; i + 1 {
    return i
}`
	errs := parseCollectAndCheckReturn(t, source)
	assertReturnErrorCount(t, errs, 1)
}

// TestReturn_NoDiag_InsideLambdaClause verifies that return inside a lambda
// clause body is accepted.
func TestReturn_NoDiag_InsideLambdaClause(t *testing.T) {
	source := `
let f = (x: i32) -> i32 {
    (0) => { return 0 },
    (n) => { return n },
}`
	errs := parseCollectAndCheckReturn(t, source)
	assertNoReturnErrors(t, errs)
}

// TestReturn_Diag_ErrorMessage verifies the exact text of the error message.
func TestReturn_Diag_ErrorMessage(t *testing.T) {
	source := `return 1`
	errs := parseCollectAndCheckReturn(t, source)
	assertReturnErrorCount(t, errs, 1)
	want := "return statement outside of a function body"
	if errs[0].Message != want {
		t.Errorf("error message = %q, want %q", errs[0].Message, want)
	}
}
