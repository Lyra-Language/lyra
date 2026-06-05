package checker_test

import (
	"testing"

	"github.com/Lyra-Language/lyra/pkg/analyzer/checker"
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector"
	"github.com/Lyra-Language/lyra/pkg/parser"
)

func parseCollectAndCheckBCL(t *testing.T, source string) []checker.BreakContinueOutsideLoopError {
	t.Helper()
	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	c := collector.NewCollector([]byte(source))
	program, _, _, _ := c.Collect(tree.RootNode())
	return checker.CheckBreakContinueOutsideLoop(program)
}

func assertNoBCLErrors(t *testing.T, errs []checker.BreakContinueOutsideLoopError) {
	t.Helper()
	if len(errs) > 0 {
		t.Errorf("expected no errors, got %d: %v", len(errs), errs)
	}
}

func assertBCLErrorCount(t *testing.T, errs []checker.BreakContinueOutsideLoopError, count int) {
	t.Helper()
	if len(errs) != count {
		t.Errorf("expected %d error(s), got %d: %v", count, len(errs), errs)
	}
}

// TestBCL_NoDiag_BreakInsideForLoop verifies that break inside a for loop is valid.
func TestBCL_NoDiag_BreakInsideForLoop(t *testing.T) {
	source := `
for var i = 0; i < 10; i + 1 {
    break
}`
	assertNoBCLErrors(t, parseCollectAndCheckBCL(t, source))
}

// TestBCL_NoDiag_ContinueInsideForLoop verifies that continue inside a for loop is valid.
func TestBCL_NoDiag_ContinueInsideForLoop(t *testing.T) {
	source := `
for var i = 0; i < 10; i + 1 {
    continue
}`
	assertNoBCLErrors(t, parseCollectAndCheckBCL(t, source))
}

// TestBCL_NoDiag_BreakInsideForInLoop verifies that break inside a for-in loop is valid.
func TestBCL_NoDiag_BreakInsideForInLoop(t *testing.T) {
	source := `
let xs = [1, 2, 3]
for x in xs {
    break
}`
	assertNoBCLErrors(t, parseCollectAndCheckBCL(t, source))
}

// TestBCL_NoDiag_ContinueInsideForInLoop verifies that continue inside a for-in loop is valid.
func TestBCL_NoDiag_ContinueInsideForInLoop(t *testing.T) {
	source := `
let xs = [1, 2, 3]
for x in xs {
    continue
}`
	assertNoBCLErrors(t, parseCollectAndCheckBCL(t, source))
}

// TestBCL_Diag_BreakTopLevel verifies that a top-level break is rejected.
func TestBCL_Diag_BreakTopLevel(t *testing.T) {
	errs := parseCollectAndCheckBCL(t, `break`)
	assertBCLErrorCount(t, errs, 1)
	want := "break statement outside of a loop"
	if errs[0].Message != want {
		t.Errorf("message = %q, want %q", errs[0].Message, want)
	}
}

// TestBCL_Diag_ContinueTopLevel verifies that a top-level continue is rejected.
func TestBCL_Diag_ContinueTopLevel(t *testing.T) {
	errs := parseCollectAndCheckBCL(t, `continue`)
	assertBCLErrorCount(t, errs, 1)
	want := "continue statement outside of a loop"
	if errs[0].Message != want {
		t.Errorf("message = %q, want %q", errs[0].Message, want)
	}
}

// TestBCL_Diag_BreakInsideFunction verifies that break inside a function body (not a loop) is rejected.
func TestBCL_Diag_BreakInsideFunction(t *testing.T) {
	source := `
let f = () -> void => {
    break
}`
	assertBCLErrorCount(t, parseCollectAndCheckBCL(t, source), 1)
}

// TestBCL_NoDiag_BreakInsideFunctionLoop verifies that break inside a loop inside a function is valid.
func TestBCL_NoDiag_BreakInsideFunctionLoop(t *testing.T) {
	source := `
let f = () -> void => {
    for var i = 0; i < 10; i + 1 {
        break
    }
}`
	assertNoBCLErrors(t, parseCollectAndCheckBCL(t, source))
}

// TestBCL_Diag_BreakInsideLambdaInsideLoop verifies that a lambda nested inside a loop
// resets the loop context — break inside the lambda is an error.
func TestBCL_Diag_BreakInsideLambdaInsideLoop(t *testing.T) {
	source := `
for var i = 0; i < 10; i + 1 {
    let f = () -> void => {
        break
    }
}`
	assertBCLErrorCount(t, parseCollectAndCheckBCL(t, source), 1)
}

// TestBCL_NoDiag_LabeledBreak verifies that break with a valid label is accepted.
func TestBCL_NoDiag_LabeledBreak(t *testing.T) {
	source := `
outer: for var i = 0; i < 10; i + 1 {
    for var j = 0; j < 10; j + 1 {
        break outer
    }
}`
	assertNoBCLErrors(t, parseCollectAndCheckBCL(t, source))
}

// TestBCL_NoDiag_LabeledContinue verifies that continue with a valid label is accepted.
func TestBCL_NoDiag_LabeledContinue(t *testing.T) {
	source := `
outer: for var i = 0; i < 10; i + 1 {
    for var j = 0; j < 10; j + 1 {
        continue outer
    }
}`
	assertNoBCLErrors(t, parseCollectAndCheckBCL(t, source))
}

// TestBCL_Diag_UnknownLabelBreak verifies that break with an unknown label is rejected.
func TestBCL_Diag_UnknownLabelBreak(t *testing.T) {
	source := `
for var i = 0; i < 10; i + 1 {
    break unknown
}`
	errs := parseCollectAndCheckBCL(t, source)
	assertBCLErrorCount(t, errs, 1)
	want := `break with unknown label "unknown"`
	if errs[0].Message != want {
		t.Errorf("message = %q, want %q", errs[0].Message, want)
	}
}

// TestBCL_Diag_UnknownLabelContinue verifies that continue with an unknown label is rejected.
func TestBCL_Diag_UnknownLabelContinue(t *testing.T) {
	source := `
for var i = 0; i < 10; i + 1 {
    continue unknown
}`
	errs := parseCollectAndCheckBCL(t, source)
	assertBCLErrorCount(t, errs, 1)
	want := `continue with unknown label "unknown"`
	if errs[0].Message != want {
		t.Errorf("message = %q, want %q", errs[0].Message, want)
	}
}

// TestBCL_Diag_UnknownLabelTopLevel verifies that break with an unknown label at top level is rejected.
func TestBCL_Diag_UnknownLabelTopLevel(t *testing.T) {
	source := `break foo`
	assertBCLErrorCount(t, parseCollectAndCheckBCL(t, source), 1)
}

// TestBCL_NoDiag_NestedLoops verifies that break/continue in nested loops are valid.
func TestBCL_NoDiag_NestedLoops(t *testing.T) {
	source := `
for var i = 0; i < 3; i + 1 {
    for var j = 0; j < 3; j + 1 {
        break
        continue
    }
    continue
}`
	assertNoBCLErrors(t, parseCollectAndCheckBCL(t, source))
}
