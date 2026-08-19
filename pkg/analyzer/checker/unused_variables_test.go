package checker_test

import (
	"slices"
	"strings"
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

// loopBindingWarnings is the lyra-W020 half of the pass — a `for-in` binding the body
// never reads.
func loopBindingWarnings(t *testing.T, source string) []diag.Diagnostic {
	t.Helper()
	var out []diag.Diagnostic
	for _, d := range parseAndCheckUnused(t, source) {
		if d.Code == diag.CodeUnusedLoopBinding {
			out = append(out, d)
		}
	}
	return out
}

func assertLoopBindings(t *testing.T, source string, wantNames ...string) {
	t.Helper()
	got := loopBindingWarnings(t, source)
	if len(got) != len(wantNames) {
		t.Fatalf("want %d loop-binding warnings %v, got %d: %v", len(wantNames), wantNames, len(got), got)
	}
	for i, name := range wantNames {
		if !strings.Contains(got[i].Message, `"`+name+`"`) {
			t.Errorf("warning %d does not name %q: %s", i, name, got[i].Message)
		}
		if got[i].Location.StartLine == 0 {
			t.Errorf("warning %d has no location — a location-less diagnostic escapes the "+
				"driver's per-file filtering and appears on every file compiled", i)
		}
	}
}

// The base case. The fix the message names is `_`, not deleting the binding: the loop
// still has to iterate.
func TestUnusedLoopBinding_CounterNobodyReads(t *testing.T) {
	assertLoopBindings(t, `
let main = () -> void => {
  var n = 0
  for i in 0..<3 { n = n + 1 }
  println("${n}")
}`, "i")
}

// The two-name form is where it earns its keep — `for k, v in xs` reading only `v` is the
// case `for _, v in xs` exists for. Either position is checked.
func TestUnusedLoopBinding_EitherPositionOfTwo(t *testing.T) {
	assertLoopBindings(t, `
let main = () -> void => {
  var n = 0
  for k, v in [1, 2] { n = n + v }
  for a, b in [1, 2] { n = n + a }
  println("${n}")
}`, "k", "b")
}

// What must stay silent. `_` is the fix, so it cannot be the complaint; `_i` is the older
// spelling of the same intent and is exempt like an unused local; and a name read anywhere
// in the body counts, including inside a string interpolation and from a nested closure.
func TestUnusedLoopBinding_SilentCases(t *testing.T) {
	assertLoopBindings(t, `
let main = () -> void => {
  var n = 0
  for _ in 0..<3 { n = n + 1 }
  for _i in 0..<3 { n = n + 1 }
  for j in 0..<3 { n = n + j }
  for s in 0..<3 { println("${s}") }
  for c in 0..<3 {
    let f = () -> i64 => c
    n = n + f()
  }
  println("${n}")
}`)
}

// A write counts as a read, matching the unused-local rule: a binding assigned in the body
// is being used, whatever it was initialized to.
func TestUnusedLoopBinding_ReassignmentCounts(t *testing.T) {
	assertLoopBindings(t, `
let main = () -> void => {
  var n = 0
  for k, v in [1, 2] {
    var m = v
    m = k
    n = n + m
  }
  println("${n}")
}`)
}
