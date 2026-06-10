package checker_test

import (
	"slices"
	"testing"

	"github.com/Lyra-Language/lyra/pkg/analyzer/checker"
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/parser"
)

func parseAndCheckUnused(t *testing.T, source string) []diag.Diagnostic {
	t.Helper()
	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	c := collector.NewCollector([]byte(source))
	program, _, _, _ := c.Collect(tree.RootNode())
	return checker.CheckUnusedVariables(program)
}

func assertNoUnused(t *testing.T, diags []diag.Diagnostic) {
	t.Helper()
	if len(diags) > 0 {
		t.Errorf("expected no unused-variable warnings, got %d: %v", len(diags), diags)
	}
}

func assertUnusedCount(t *testing.T, diags []diag.Diagnostic, count int) {
	t.Helper()
	if len(diags) != count {
		t.Errorf("expected %d unused-variable warning(s), got %d: %v", count, len(diags), diags)
	}
}

func assertUnusedHasTags(t *testing.T, diags []diag.Diagnostic) {
	t.Helper()
	for i, d := range diags {
		if !slices.Contains(d.Tags, diag.TagUnnecessary) {
			t.Errorf("diagnostic[%d] missing TagUnnecessary: %v", i, d)
		}
	}
}

// TestUnused_NoDiag_AllUsed verifies that all-used variables produce no warnings.
func TestUnused_NoDiag_AllUsed(t *testing.T) {
	src := `
let f = () => {
    let x = 1
    let y = x + 2
    print(y)
}
`
	assertNoUnused(t, parseAndCheckUnused(t, src))
}

// TestUnused_Diag_SingleUnused verifies that an unused variable is flagged.
func TestUnused_Diag_SingleUnused(t *testing.T) {
	src := `
let f = () => {
    let x = 42
    let y = "hello"
    print(y)
}
`
	diags := parseAndCheckUnused(t, src)
	assertUnusedCount(t, diags, 1)
	assertUnusedHasTags(t, diags)
}

// TestUnused_Diag_MultipleUnused verifies that multiple unused variables are
// each reported.
func TestUnused_Diag_MultipleUnused(t *testing.T) {
	src := `
let f = () => {
    let a = 1
    let b = 2
    let c = 3
}
`
	diags := parseAndCheckUnused(t, src)
	assertUnusedCount(t, diags, 3)
	assertUnusedHasTags(t, diags)
}

// TestUnused_NoDiag_WildcardDiscard verifies that bare `let _ = ...` (wildcard
// discard) is never warned — the grammar parses it as a DestructuringDeclStmt
// with no named bindings.
func TestUnused_NoDiag_WildcardDiscard(t *testing.T) {
	src := `
let f = () => {
    let _ = "ok"
}
`
	assertNoUnused(t, parseAndCheckUnused(t, src))
}

// TestUnused_NoDiag_UnderscorePrefixedName verifies that _foo names are not
// warned — the leading _ is the conventional "intentionally unused" marker.
func TestUnused_NoDiag_UnderscorePrefixedName(t *testing.T) {
	src := `
let f = () => {
    let _unused = 42
}
`
	assertNoUnused(t, parseAndCheckUnused(t, src))
}

// TestUnused_NoDiag_TopLevel verifies that top-level bindings are not checked.
func TestUnused_NoDiag_TopLevel(t *testing.T) {
	src := `
let x = 42
let y = "never used in file"
`
	assertNoUnused(t, parseAndCheckUnused(t, src))
}

// TestUnused_NoDiag_UsedInClosure verifies that a variable used inside a
// nested lambda is not flagged (closure capture).
func TestUnused_NoDiag_UsedInClosure(t *testing.T) {
	src := `
let f = () => {
    let x = 10
    let g = () => x + 1
    g()
}
`
	assertNoUnused(t, parseAndCheckUnused(t, src))
}

// TestUnused_NoDiag_UsedInNestedBlock verifies that a variable used in a
// nested block is not flagged.
func TestUnused_NoDiag_UsedInNestedBlock(t *testing.T) {
	src := `
let f = () => {
    let x = 10
    {
        print(x)
    }
}
`
	assertNoUnused(t, parseAndCheckUnused(t, src))
}

// TestUnused_Diag_NestedLambdaUnused verifies that unused variables inside a
// nested lambda are detected independently.
func TestUnused_Diag_NestedLambdaUnused(t *testing.T) {
	src := `
let outer = () => {
    let g = () => {
        let dead = 99
    }
    g()
}
`
	diags := parseAndCheckUnused(t, src)
	assertUnusedCount(t, diags, 1)
	assertUnusedHasTags(t, diags)
}

// TestUnused_NoDiag_UsedViaSpread verifies that a variable used via spread is
// not flagged.
func TestUnused_NoDiag_UsedViaSpread(t *testing.T) {
	src := `
let f = () => {
    let xs = [1, 2, 3]
    print(...xs)
}
`
	assertNoUnused(t, parseAndCheckUnused(t, src))
}

// TestUnused_NoDiag_ForInLoop verifies that loop variables are not flagged
// when the loop body uses them.
func TestUnused_NoDiag_ForInLoop(t *testing.T) {
	src := `
let f = () => {
    for i in 0..10 {
        print(i)
    }
}
`
	assertNoUnused(t, parseAndCheckUnused(t, src))
}
