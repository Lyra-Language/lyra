package checker_test

import (
	"slices"
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
	return checker.CheckShadowing(program, nil)
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
	assertShadowingWarningContains(t, warns, "x shadows a name declared in an outer scope")
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
	assertShadowingWarningContains(t, warns, "x shadows a name declared in an outer scope")
	assertShadowingWarningContains(t, warns, "y shadows a name declared in an outer scope")
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
	assertShadowingWarningContains(t, warns, "x shadows a name declared in an outer scope")
}

// A for-in loop variable is a declaration like any other: one taking an outer name warns,
// as a `let` does. It was exempt until 10/04 — harmless while
// the typechecker ignored the shadow, misleading once the loop variable really shadowed.
func TestShadow_Diag_ForInIterationVar(t *testing.T) {
	source := `
let item = 5
for item in my_collection {
    println(item)
}`
	warns := parseCollectAndCheckShadowing(t, source)
	assertShadowingWarningCount(t, warns, 1)
	assertShadowingWarningContains(t, warns, "item shadows a name declared in an outer scope")
}

func TestShadow_ForInLoopVariables(t *testing.T) {
	cases := []struct {
		name   string
		source string
		want   []string // the names warned, in order
	}{
		{"a parameter", `
let f = (c: i64, xs: []i64) -> i64 => {
    var t = 0
    for c in xs { t += c }
    t + c
}`, []string{"c"}},
		{"both names of the two-name form", `
let i = 0
let x = 1
let f = (xs: []i64) -> void => {
    for i, x in xs { println("${i}") }
}`, []string{"i", "x"}},
		{"an underscore names nothing", `
let f = (xs: []i64) -> i64 => {
    let _ = 0
    var n = 0
    for _ in xs { n += 1 }
    n
}`, nil},
		{"a destructured element warns once per name", `
let a = 0
let f = (ps: [](i64, i64)) -> i64 => {
    var t = 0
    for (a, b) in ps { t += a + b }
    t
}`, []string{"a"}},
		{"nested destructuring loops do not report their element holder", `
let f = (ps: [](i64, i64), qs: [](i64, i64)) -> i64 => {
    var t = 0
    for (a, b) in ps {
        for (c, d) in qs { t += a + b + c + d }
    }
    t
}`, nil},
		{"a fresh name warns nothing, and the loop variable does not leak", `
let f = (xs: []i64) -> i64 => {
    var t = 0
    for v in xs { t += v }
    let v = t
    v
}`, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			warns := parseCollectAndCheckShadowing(t, c.source)
			var got []string
			for _, w := range warns {
				got = append(got, strings.Fields(w.Message)[0])
			}
			if !slices.Equal(got, c.want) {
				t.Errorf("warned %v, want %v", got, c.want)
			}
		})
	}
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
	want := "x shadows a name declared in an outer scope"
	if !strings.Contains(warns[0].Error(), want) {
		t.Errorf("warning message %q does not contain %q", warns[0].Error(), want)
	}
}
