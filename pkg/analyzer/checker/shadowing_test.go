package checker_test

import (
	"strings"
	"testing"

	"github.com/Lyra-Language/lyra/pkg/analyzer/checker"
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector"
	"github.com/Lyra-Language/lyra/pkg/parser"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func parseCollectAndCheckShadowing(t *testing.T, source string) []checker.ShadowingWarning {
	t.Helper()
	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	c := collector.NewCollector([]byte(source))
	program, _, _, _ := c.Collect(tree.RootNode())
	return checker.CheckShadowing(program)
}

func assertNoShadowingWarnings(t *testing.T, warns []checker.ShadowingWarning) {
	t.Helper()
	if len(warns) > 0 {
		t.Errorf("expected no warnings, got %d: %v", len(warns), warns)
	}
}

func assertShadowingWarningCount(t *testing.T, warns []checker.ShadowingWarning, count int) {
	t.Helper()
	if len(warns) != count {
		t.Errorf("expected %d warning(s), got %d: %v", count, len(warns), warns)
	}
}

func assertShadowingWarningContains(t *testing.T, warns []checker.ShadowingWarning, substr string) {
	t.Helper()
	for _, w := range warns {
		if strings.Contains(w.Error(), substr) {
			return
		}
	}
	t.Errorf("expected a warning containing %q, got: %v", substr, warns)
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestShadow_NoDiag_TopLevel verifies that declarations at the top level (no
// outer scope) never produce shadowing warnings.
func TestShadow_NoDiag_TopLevel(t *testing.T) {
	source := `
let x = 5
let y = 10
let z = x + y`
	warns := parseCollectAndCheckShadowing(t, source)
	assertNoShadowingWarnings(t, warns)
}

// TestShadow_Diag_NestedBlock verifies that declaring a name inside a nested
// block that already exists in an outer scope produces exactly one warning.
func TestShadow_Diag_NestedBlock(t *testing.T) {
	source := `
let x = 5
let result = {
    let x = 10
    x
}`
	warns := parseCollectAndCheckShadowing(t, source)
	assertShadowingWarningCount(t, warns, 1)
	assertShadowingWarningContains(t, warns, "x shadows a variable declared in an outer scope")
}

// TestShadow_NoDiag_SiblingBlocks verifies that two sibling blocks that each
// declare the same name do not produce shadowing warnings, because neither
// block encloses the other.
func TestShadow_NoDiag_SiblingBlocks(t *testing.T) {
	source := `
let a = {
    let x = 1
    x
}
let b = {
    let x = 2
    x
}`
	warns := parseCollectAndCheckShadowing(t, source)
	assertNoShadowingWarnings(t, warns)
}

// TestShadow_Diag_MultipleLevels verifies that each level of nesting that
// re-declares a name from an enclosing scope produces its own warning.
func TestShadow_Diag_MultipleLevels(t *testing.T) {
	source := `
let x = 1
let result = {
    let y = 2
    let inner = {
        let x = 3
        let y = 4
        x + y
    }
    inner
}`
	warns := parseCollectAndCheckShadowing(t, source)
	assertShadowingWarningCount(t, warns, 2)
	assertShadowingWarningContains(t, warns, "x shadows a variable declared in an outer scope")
	assertShadowingWarningContains(t, warns, "y shadows a variable declared in an outer scope")
}

// TestShadow_NoDiag_LambdaParam verifies that a lambda parameter whose name
// matches an outer variable does NOT produce a shadowing warning — lambda
// parameters are pattern-bound names and are silently allowed.
func TestShadow_NoDiag_LambdaParam(t *testing.T) {
	source := `
let x = 5
let f = (x: i32) -> i32 => x`
	warns := parseCollectAndCheckShadowing(t, source)
	assertNoShadowingWarnings(t, warns)
}

// TestShadow_Diag_VarInsideLambdaBodyShadowsOuter verifies that a VarDeclStmt
// inside a lambda body that shadows a name from the enclosing scope (outside
// the lambda) DOES produce a warning.
func TestShadow_Diag_VarInsideLambdaBodyShadowsOuter(t *testing.T) {
	source := `
let x = 5
let f = (y: i32) -> i32 => {
    let x = y
    x
}`
	warns := parseCollectAndCheckShadowing(t, source)
	assertShadowingWarningCount(t, warns, 1)
	assertShadowingWarningContains(t, warns, "x shadows a variable declared in an outer scope")
}

// TestShadow_NoDiag_ForInIterationVar verifies that a for-in loop's iteration
// variable is NOT warned about even when it matches an outer-scope name.
func TestShadow_NoDiag_ForInIterationVar(t *testing.T) {
	source := `
let item = 5
for item in my_collection {
    println(item)
}`
	warns := parseCollectAndCheckShadowing(t, source)
	assertNoShadowingWarnings(t, warns)
}

// TestShadow_Diag_ForLoopInitVar verifies that a for-loop's init variable DOES
// produce a warning when it shadows an outer-scope name.
func TestShadow_Diag_ForLoopInitVar(t *testing.T) {
	source := `
let i = 100
for var i = 0; i < 10; i + 1 {
    println(i)
}`
	warns := parseCollectAndCheckShadowing(t, source)
	assertShadowingWarningCount(t, warns, 1)
	assertShadowingWarningContains(t, warns, "i shadows a variable declared in an outer scope")
}

// TestShadow_NoDiag_MatchArmPatternBinding verifies that a match arm's pattern-
// bound variable does NOT produce a warning even when it matches an outer name.
func TestShadow_NoDiag_MatchArmPatternBinding(t *testing.T) {
	source := `
let x = 5
match foo {
    Some x => x,
    _ => 0,
}`
	warns := parseCollectAndCheckShadowing(t, source)
	assertNoShadowingWarnings(t, warns)
}

// TestShadow_Diag_DestructuringDecl verifies that a destructuring declaration
// DOES produce a warning when a bound name shadows an outer-scope variable.
// Destructuring declarations are explicit binding statements, so they follow
// the same shadowing rule as plain VarDeclStmts.
func TestShadow_Diag_DestructuringDecl(t *testing.T) {
	source := `
let a = 1
let result = {
    let [a, b] = some_array
    a
}`
	warns := parseCollectAndCheckShadowing(t, source)
	assertShadowingWarningContains(t, warns, "a")
}

// TestShadow_NoDiag_DestructuringIf verifies that an if-destructuring
// statement's pattern names do NOT produce warnings even when they shadow outer
// names.
func TestShadow_NoDiag_DestructuringIf(t *testing.T) {
	source := `
let a = 1
if let [a, b] = some_array {
    println(a)
}`
	warns := parseCollectAndCheckShadowing(t, source)
	assertNoShadowingWarnings(t, warns)
}

// TestShadow_WarningMessage verifies the exact format of the warning message.
func TestShadow_WarningMessage(t *testing.T) {
	source := `
let x = 1
let r = {
    let x = 2
    x
}`
	warns := parseCollectAndCheckShadowing(t, source)
	assertShadowingWarningCount(t, warns, 1)
	want := "x shadows a variable declared in an outer scope"
	if !strings.Contains(warns[0].Error(), want) {
		t.Errorf("warning message %q does not contain %q", warns[0].Error(), want)
	}
}
