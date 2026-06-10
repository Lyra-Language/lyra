package checker_test

import (
	"slices"
	"testing"

	"github.com/Lyra-Language/lyra/pkg/analyzer/checker"
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/parser"
)

func parseAndCheckUnusedParams(t *testing.T, source string) []diag.Diagnostic {
	t.Helper()
	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	c := collector.NewCollector([]byte(source))
	program, _, _, _ := c.Collect(tree.RootNode())
	return checker.CheckUnusedParameters(program)
}

func assertNoUnusedParams(t *testing.T, diags []diag.Diagnostic) {
	t.Helper()
	if len(diags) > 0 {
		t.Errorf("expected no unused-parameter warnings, got %d: %v", len(diags), diags)
	}
}

func assertUnusedParamCount(t *testing.T, diags []diag.Diagnostic, count int) {
	t.Helper()
	if len(diags) != count {
		t.Errorf("expected %d unused-parameter warning(s), got %d: %v", count, len(diags), diags)
	}
}

func assertUnusedParamHasTags(t *testing.T, diags []diag.Diagnostic) {
	t.Helper()
	for i, d := range diags {
		if !slices.Contains(d.Tags, diag.TagUnnecessary) {
			t.Errorf("diagnostic[%d] missing TagUnnecessary: %v", i, d)
		}
	}
}

// TestUnusedParams_NoDiag_AllUsed verifies that all-used parameters produce no warnings.
func TestUnusedParams_NoDiag_AllUsed(t *testing.T) {
	src := `
let add = (x, y) => x + y
`
	assertNoUnusedParams(t, parseAndCheckUnusedParams(t, src))
}

// TestUnusedParams_Diag_SingleUnused verifies that one unused parameter is flagged.
func TestUnusedParams_Diag_SingleUnused(t *testing.T) {
	src := `
let f = (x, y) => x + 1
`
	diags := parseAndCheckUnusedParams(t, src)
	assertUnusedParamCount(t, diags, 1)
	assertUnusedParamHasTags(t, diags)
}

// TestUnusedParams_Diag_AllUnused verifies that all unused parameters are flagged.
func TestUnusedParams_Diag_AllUnused(t *testing.T) {
	src := `
let f = (a, b, c) => 42
`
	diags := parseAndCheckUnusedParams(t, src)
	assertUnusedParamCount(t, diags, 3)
	assertUnusedParamHasTags(t, diags)
}

// TestUnusedParams_NoDiag_UnderscorePrefix verifies that _-prefixed params are silently ignored.
func TestUnusedParams_NoDiag_UnderscorePrefix(t *testing.T) {
	src := `
let f = (_x, _y) => 0
`
	assertNoUnusedParams(t, parseAndCheckUnusedParams(t, src))
}

// TestUnusedParams_NoDiag_UsedInNestedLambda verifies that params closed over by
// an inner lambda are not flagged.
func TestUnusedParams_NoDiag_UsedInNestedLambda(t *testing.T) {
	src := `
let makeAdder = (n) => (x) => x + n
`
	assertNoUnusedParams(t, parseAndCheckUnusedParams(t, src))
}

// TestUnusedParams_NoDiag_ZeroParams verifies that a lambda with no parameters
// produces no warnings.
func TestUnusedParams_NoDiag_ZeroParams(t *testing.T) {
	src := `
let greet = () => print("hello")
`
	assertNoUnusedParams(t, parseAndCheckUnusedParams(t, src))
}

// TestUnusedParams_Diag_NestedLambdaOwnParam verifies that an unused parameter
// of a nested lambda is independently flagged.
func TestUnusedParams_Diag_NestedLambdaOwnParam(t *testing.T) {
	src := `
let outer = (x) => {
    let inner = (dead) => x + 1
    inner(0)
}
`
	diags := parseAndCheckUnusedParams(t, src)
	assertUnusedParamCount(t, diags, 1)
	assertUnusedParamHasTags(t, diags)
}

// TestUnusedParams_NoDiag_UsedInBlock verifies that a parameter used inside a
// nested block expression is not flagged.
func TestUnusedParams_NoDiag_UsedInBlock(t *testing.T) {
	src := `
let f = (x) => {
    let y = x + 1
    y
}
`
	assertNoUnusedParams(t, parseAndCheckUnusedParams(t, src))
}
