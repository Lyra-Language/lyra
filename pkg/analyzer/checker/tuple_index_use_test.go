package checker_test

import (
	"testing"

	"github.com/Lyra-Language/lyra/pkg/analyzer/checker"
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector"
	"github.com/Lyra-Language/lyra/pkg/parser"
)

// `p.0` is a *TupleIndexExpr, a different AST node from `p.x` (*MemberExpr), and
// `ast.walkExprChildren` had no case for it — so the shared walker never descended into
// the thing being indexed. Every pass built on that walker was therefore blind to
// anything reached through a tuple index, and each consequence read as a bug in the pass
// that suffered it rather than in the walker. These are those consequences, one test
// each; the fix is a single case, so they stand or fall together.
//
// The corresponding closure-capture failure (a capture used only as `p.0` did not lower:
// "unbound identifier") is an exec test in pkg/backend/llvm.

// The soundness one: `pure` is only as good as the walk that looks for effects, so an
// impure call written `noisy().0` was simply not seen. The struct-field spelling of the
// same program (`noisy().x`) was correctly rejected the whole time, which is what made
// this a hole rather than a missing feature.
func TestPurity_ImpureCallThroughTupleIndex(t *testing.T) {
	src := `
tuple Pair(i64, i64)
let noisy = () -> Pair => {
    print("side effect")
    Pair(1, 2)
}
let p = pure () -> i64 => noisy().0
`
	assertPurityCount(t, checkPurity(t, src), 1)
}

// A parameter used only as `p.0` is used.
func TestUnusedParams_TupleIndexCounts(t *testing.T) {
	assertNoUnusedParams(t, parseAndCheckUnusedParams(t, `
let f = (p: (i64, i64)) -> i64 => p.0 + p.1
`))
}

// The same for a local binding.
func TestUnusedVariables_TupleIndexCounts(t *testing.T) {
	src := `
let main = () -> u8 => {
    let t = (1, 2)
    u8(t.0 + t.1)
}
`
	tree, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	c := collector.NewCollector([]byte(src))
	program, _, _, _ := c.Collect(tree.RootNode())
	if diags := checker.CheckUnusedVariables(program); len(diags) > 0 {
		t.Errorf("expected no unused-variable warnings, got %d: %v", len(diags), diags)
	}
}

// And a use *before* the declaration is still a use.
func TestUseBeforeDeclaration_ThroughTupleIndex(t *testing.T) {
	assertErrorCount(t, parseCollectAndCheck(t, `
let main = () -> u8 => {
    let a = b.0
    let b = (1, 2)
    u8(a)
}
`), 1)
}
