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

func parseCollectAndCheck(t *testing.T, source string) []checker.UseBeforeDeclarationError {
	t.Helper()
	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	c := collector.NewCollector([]byte(source))
	program, _, _, _ := c.Collect(tree.RootNode())
	return checker.CheckUseBeforeDeclaration(program)
}

func assertNoErrors(t *testing.T, errs []checker.UseBeforeDeclarationError) {
	t.Helper()
	if len(errs) > 0 {
		t.Errorf("expected no errors, got %d: %v", len(errs), errs)
	}
}

func assertErrorCount(t *testing.T, errs []checker.UseBeforeDeclarationError, count int) {
	t.Helper()
	if len(errs) != count {
		t.Errorf("expected %d error(s), got %d: %v", count, len(errs), errs)
	}
}

func assertErrorContains(t *testing.T, errs []checker.UseBeforeDeclarationError, substr string) {
	t.Helper()
	for _, e := range errs {
		if strings.Contains(e.Error(), substr) {
			return
		}
	}
	t.Errorf("expected an error containing %q, got: %v", substr, errs)
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

// TestUBD_NoDiag_DeclareBeforeUse verifies that a normal declaration-then-use
// pattern at the top level produces no errors.
func TestUBD_NoDiag_DeclareBeforeUse(t *testing.T) {
	source := `
let x = 5
let y = x + 1`
	errs := parseCollectAndCheck(t, source)
	assertNoErrors(t, errs)
}

// TestUBD_NoDiag_OuterScopeVisible verifies that a variable from an outer scope
// used inside a block is NOT flagged — it is not in the block's declared set.
func TestUBD_NoDiag_OuterScopeVisible(t *testing.T) {
	source := `
let outer = 10
let result = { outer + 1 }`
	errs := parseCollectAndCheck(t, source)
	assertNoErrors(t, errs)
}

// TestUBD_Diag_UseBeforeDecl_InBlock verifies that using a variable before its
// declaration within the same block produces exactly one error.
func TestUBD_Diag_UseBeforeDecl_InBlock(t *testing.T) {
	// Inside the block: declared = {a, y}.
	// When processing `let a = y`, y is in declared but not yet seen → error.
	source := `
let result = {
    let a = y
    let y = 10
    y
}`
	errs := parseCollectAndCheck(t, source)
	assertErrorCount(t, errs, 1)
	assertErrorContains(t, errs, `"y"`)
}

// TestUBD_NoDiag_UseAfterDecl_InBlock verifies that using a variable after its
// declaration within the same block is clean.
func TestUBD_NoDiag_UseAfterDecl_InBlock(t *testing.T) {
	source := `
let result = {
    let y = 10
    y
}`
	errs := parseCollectAndCheck(t, source)
	assertNoErrors(t, errs)
}

// TestUBD_Diag_SelfReferential verifies that a self-referential initializer at
// the top level is flagged: `let x = x + 1` uses x before x is seen.
func TestUBD_Diag_SelfReferential(t *testing.T) {
	source := `let x = x + 1`
	errs := parseCollectAndCheck(t, source)
	assertErrorCount(t, errs, 1)
	assertErrorContains(t, errs, `"x"`)
}

// TestUBD_NoDiag_LambdaParams verifies that lambda parameters used inside the
// body are never flagged: the lambda's fresh scope has an empty declared set,
// so x and y are simply not in it.
func TestUBD_NoDiag_LambdaParams(t *testing.T) {
	source := `let f = (x: i32, y: i32) -> i32 => x + y`
	errs := parseCollectAndCheck(t, source)
	assertNoErrors(t, errs)
}

// TestUBD_NoDiag_ForInBody verifies that the iteration variable of a for-in loop
// used inside the body is not flagged: it is not a VarDeclStmt in the body, so
// it never appears in that scope's declared set.
func TestUBD_NoDiag_ForInBody(t *testing.T) {
	source := `
for item in my_collection {
    println(item)
}`
	errs := parseCollectAndCheck(t, source)
	assertNoErrors(t, errs)
}

// TestUBD_Diag_UseBeforeDecl_TopLevel verifies that using a top-level variable
// before its declaration is flagged.
func TestUBD_Diag_UseBeforeDecl_TopLevel(t *testing.T) {
	source := `
let r = z + 1
let z = 5`
	errs := parseCollectAndCheck(t, source)
	assertErrorCount(t, errs, 1)
	assertErrorContains(t, errs, `"z"`)
}

// TestUBD_NoDiag_RecursiveLambda verifies that a recursive lambda body that
// calls its own name is not flagged: inside the lambda body the outer declared
// set is not visible (fresh scope), so `factorial` is not in declared there.
func TestUBD_NoDiag_RecursiveLambda(t *testing.T) {
	source := `let factorial = (n: i32) -> i32 => {
    if n <= 1 {
        1
    } else {
        n * factorial(n - 1)
    }
}`
	errs := parseCollectAndCheck(t, source)
	assertNoErrors(t, errs)
}
