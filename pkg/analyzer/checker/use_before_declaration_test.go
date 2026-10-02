package checker_test

import (
	"strings"
	"testing"

	"github.com/Lyra-Language/lyra/pkg/analyzer/checker"
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/parser"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func parseCollectAndCheck(t *testing.T, source string) []diag.Diagnostic {
	t.Helper()
	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	c := collector.NewCollector([]byte(source))
	program, _, _, _ := c.Collect(tree.RootNode())
	return checker.CheckUseBeforeDeclaration(program)
}

func assertNoErrors(t *testing.T, errs []diag.Diagnostic) {
	t.Helper()
	if len(errs) > 0 {
		t.Errorf("expected no errors, got %d: %v", len(errs), errs)
	}
}

func assertErrorCount(t *testing.T, errs []diag.Diagnostic, count int) {
	t.Helper()
	if len(errs) != count {
		t.Errorf("expected %d error(s), got %d: %v", count, len(errs), errs)
	}
}

func assertErrorContains(t *testing.T, errs []diag.Diagnostic, substr string) {
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

// An impl method's body is a function body, so a top-level name declared *below* the
// impl is not a use before declaration — exactly as it is not from a `let`'s lambda.
// Until 09/07 the impl's clause body was checked bare in the top-level scope, so
// `std.collections` had to hoist a helper above the impls that called it.
func TestUBD_NoDiag_ImplMethodCallsLaterTopLevel(t *testing.T) {
	errs := parseCollectAndCheck(t, `
trait Hh { h: (Self) -> u64 }
impl Hh for u8 { h = (self) => helper(u64(self)) }
let helper = (x: u64) -> u64 => x + 1
`)
	assertNoErrors(t, errs)
}

// The same for a trait's default method body.
func TestUBD_NoDiag_TraitDefaultCallsLaterTopLevel(t *testing.T) {
	errs := parseCollectAndCheck(t, `
trait Hh { h: (Self) -> u64 = (self) => helper(1) }
let helper = (x: u64) -> u64 => x + 1
`)
	assertNoErrors(t, errs)
}

// The fresh scope is still a scope: a local declared later *inside* the method body is
// still reported, and the receiver is in scope from the start.
func TestUBD_Diag_ImplMethodBodyStillChecked(t *testing.T) {
	errs := parseCollectAndCheck(t, `
trait Hh { h: (Self) -> u64 }
impl Hh for u8 { h = (self) => { let a = b + u64(self); let b = 1; a } }
`)
	assertErrorCount(t, errs, 1)
	assertErrorContains(t, errs, `"b"`)
}

// A nested scope is entered with the names already visible from outside it, so a `let`
// there that shadows one of them reads the outer binding in its own initializer — and a use
// before it is a use of the outer one, not a use before declaration. Each block used to be
// entered bare, which refused these shadows (lyra-E002) though LANGUAGE.md promises them
// and the typechecker resolves them.
func TestUBD_NoDiag_ShadowInANestedScope(t *testing.T) {
	for name, source := range map[string]string{
		"parameter, nested block": `
let f = (n: i64) -> i64 => {
    var total = 0
    {
        let n = n * 10
        total += n
    }
    total + n
}`,
		"outer let, nested block": `
let f = () -> i64 => {
    let x = 1
    {
        let x = x + 1
        x
    }
}`,
		"pattern binding, arm block": `
let f = (o: Opt) -> i64 => match o {
    Has(v) => {
        let v = v * 3
        v
    },
    Nothing => 0,
}`,
		"parameter, loop body": `
let f = (n: i64, xs: []i64) -> i64 => {
    var total = 0
    for x in xs {
        let n = n + x
        total += n
    }
    total
}`,
		"loop variable, loop body": `
let f = (xs: []i64) -> i64 => {
    var total = 0
    for x in xs {
        let x = x * 2
        total += x
    }
    total
}`,
		"use before the shadow reads the outer binding": `
let total = 7
let f = () -> i64 => {
    let a = total
    let total = 5
    a + total
}`,
	} {
		t.Run(name, func(t *testing.T) {
			assertNoErrors(t, parseCollectAndCheck(t, source))
		})
	}
}

// A name visible from nowhere outside is still a use before its declaration in a nested
// scope: the outer names are what is seeded, not the block's own.
func TestUBD_Diag_UseBeforeDecl_NestedWithNoOuterBinding(t *testing.T) {
	source := `
let f = (n: i64) -> i64 => {
    {
        let a = y
        let y = n
        a + y
    }
}`
	errs := parseCollectAndCheck(t, source)
	assertErrorCount(t, errs, 1)
	assertErrorContains(t, errs, `"y"`)
}
