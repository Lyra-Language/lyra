package checker_test

import (
	"slices"
	"testing"

	"github.com/Lyra-Language/lyra/pkg/analyzer/checker"
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/parser"
)

func parseAndCheckUnreachable(t *testing.T, source string) []diag.Diagnostic {
	t.Helper()
	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	c := collector.NewCollector([]byte(source))
	program, _, _, _ := c.Collect(tree.RootNode())
	return checker.CheckUnreachableCode(program)
}

func assertNoUnreachable(t *testing.T, diags []diag.Diagnostic) {
	t.Helper()
	if len(diags) > 0 {
		t.Errorf("expected no unreachable warnings, got %d: %v", len(diags), diags)
	}
}

func assertUnreachableCount(t *testing.T, diags []diag.Diagnostic, count int) {
	t.Helper()
	if len(diags) != count {
		t.Errorf("expected %d unreachable warning(s), got %d: %v", count, len(diags), diags)
	}
}

func assertUnreachableHasTags(t *testing.T, diags []diag.Diagnostic) {
	t.Helper()
	for i, d := range diags {
		if !slices.Contains(d.Tags, diag.TagUnnecessary) {
			t.Errorf("diagnostic[%d] missing TagUnnecessary: %v", i, d)
		}
	}
}

// TestUnreachable_NoDiag_NormalSequence verifies that normal sequential
// statements without terminators produce no warnings.
func TestUnreachable_NoDiag_NormalSequence(t *testing.T) {
	src := `
let f = () -> i32 => {
    let x = 1
    let y = 2
    return x + y
}
`
	assertNoUnreachable(t, parseAndCheckUnreachable(t, src))
}

// TestUnreachable_Diag_AfterReturn verifies that code after a return is flagged.
func TestUnreachable_Diag_AfterReturn(t *testing.T) {
	src := `
let f = () -> i32 => {
    return 1
    let x = 2
}
`
	diags := parseAndCheckUnreachable(t, src)
	assertUnreachableCount(t, diags, 1)
	assertUnreachableHasTags(t, diags)
}

// TestUnreachable_Diag_MultipleAfterReturn verifies that all statements after
// a return are flagged.
func TestUnreachable_Diag_MultipleAfterReturn(t *testing.T) {
	src := `
let f = () => {
    return 0
    let x = 1
    let y = 2
    let z = 3
}
`
	diags := parseAndCheckUnreachable(t, src)
	assertUnreachableCount(t, diags, 3)
	assertUnreachableHasTags(t, diags)
}

// TestUnreachable_Diag_AfterBreak verifies that code after a break is flagged.
func TestUnreachable_Diag_AfterBreak(t *testing.T) {
	src := `
let f = () => {
    for var i = 0; i < 10; i += 1 {
        break
        let x = 1
    }
}
`
	diags := parseAndCheckUnreachable(t, src)
	assertUnreachableCount(t, diags, 1)
	assertUnreachableHasTags(t, diags)
}

// TestUnreachable_Diag_AfterContinue verifies that code after a continue is flagged.
func TestUnreachable_Diag_AfterContinue(t *testing.T) {
	src := `
let f = () => {
    for var i = 0; i < 10; i += 1 {
        continue
        let x = 1
    }
}
`
	diags := parseAndCheckUnreachable(t, src)
	assertUnreachableCount(t, diags, 1)
	assertUnreachableHasTags(t, diags)
}

// TestUnreachable_NoDiag_ReturnInNestedIf verifies that a return inside an if
// branch does not make the subsequent sibling statements unreachable.
func TestUnreachable_NoDiag_ReturnInNestedIf(t *testing.T) {
	src := `
let f = (x: bool) -> i32 => {
    if x {
        return 1
    }
    return 2
}
`
	assertNoUnreachable(t, parseAndCheckUnreachable(t, src))
}

// TestUnreachable_Diag_NestedLambda verifies that unreachable code inside a
// nested lambda is detected independently.
func TestUnreachable_Diag_NestedLambda(t *testing.T) {
	src := `
let outer = () => {
    let inner = () => {
        return 1
        let dead = 2
    }
}
`
	diags := parseAndCheckUnreachable(t, src)
	assertUnreachableCount(t, diags, 1)
	assertUnreachableHasTags(t, diags)
}

// TestUnreachable_NoDiag_OnlyReturn verifies that a single return with no
// following statements produces no warning.
func TestUnreachable_NoDiag_OnlyReturn(t *testing.T) {
	src := `
let f = () -> i32 => {
    return 42
}
`
	assertNoUnreachable(t, parseAndCheckUnreachable(t, src))
}
