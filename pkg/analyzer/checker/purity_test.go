package checker_test

import (
	"testing"

	"github.com/Lyra-Language/lyra/pkg/analyzer/checker"
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector"
	"github.com/Lyra-Language/lyra/pkg/parser"
)

func checkPurity(t *testing.T, source string) []checker.PurityError {
	t.Helper()
	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	c := collector.NewCollector([]byte(source))
	program, _, _, _ := c.Collect(tree.RootNode())
	return checker.CheckPurity(program)
}

func assertPurityCount(t *testing.T, errs []checker.PurityError, want int) {
	t.Helper()
	if len(errs) != want {
		t.Fatalf("expected %d purity error(s), got %d: %v", want, len(errs), errs)
	}
}

// Local mutation inside a pure function is allowed: a `var` declared in the body
// can be freely reassigned/compound-assigned because the mutation never escapes.
func TestPurity_LocalMutation_Ok(t *testing.T) {
	src := `
let sum = pure (n: i64) -> i64 => {
    var acc = 0
    for var i = 0; i < n; i += 1 {
        acc += i
    }
    acc
}`
	assertPurityCount(t, checkPurity(t, src), 0)
}

// Reassigning a captured (outer-scope) binding from a pure function is an
// observable effect and must be reported.
func TestPurity_CapturedReassignment_Error(t *testing.T) {
	src := `
var counter = 0
let bump = pure (n: i64) -> i64 => {
    counter = n
    n
}`
	assertPurityCount(t, checkPurity(t, src), 1)
}

// Compound-assigning a captured binding is likewise reported.
func TestPurity_CapturedCompoundAssign_Error(t *testing.T) {
	src := `
var total = 0
let add = pure (n: i64) -> i64 => {
    total += n
    n
}`
	assertPurityCount(t, checkPurity(t, src), 1)
}

// Calling a known-impure builtin from a pure function is reported.
func TestPurity_CallsImpureBuiltin_Error(t *testing.T) {
	src := `
let shout = pure (msg: string) -> string => {
    println(msg)
    msg
}`
	assertPurityCount(t, checkPurity(t, src), 1)
}

// A `pure` function that calls a user-defined impure function is reported. The
// callee `logIt` is inferred impure (it mutates outer-scope `counter` and calls
// the impure builtin `println`); calling it from `pure record` then propagates
// the violation across the function boundary.
func TestPurity_CallsUserDefinedImpureFunction_Error(t *testing.T) {
	src := `
var counter = 0
let logIt = (msg: string) -> string => {
    println(msg)
    counter += 1
    msg
}
let record = pure (msg: string) -> string => {
    logIt(msg)
    msg
}`
	errs := checkPurity(t, src)
	assertPurityCount(t, errs, 1)
	// The single error is the cross-function call inside the pure function — not
	// the effects inside the (unconstrained) impure callee itself.
	if errs[0].Message != `pure function calls impure function "logIt"` {
		t.Errorf("unexpected message: %q", errs[0].Message)
	}
}

// The same operations in a non-pure function are fine — purity only constrains
// functions marked `pure`.
func TestPurity_ImpureFunction_NoConstraint(t *testing.T) {
	src := `
var counter = 0
let bump = (n: i64) -> i64 => {
    counter = n
    println("bumped")
    n
}`
	assertPurityCount(t, checkPurity(t, src), 0)
}

// Mutating through a `mut`-borrowed parameter writes to the caller's value, so
// it escapes the pure function and must be reported.
func TestPurity_MutBorrowParamMutation_Error(t *testing.T) {
	src := `
let reset = pure (p: mut Point) -> void => {
    p.x = 0
}`
	assertPurityCount(t, checkPurity(t, src), 1)
}

// An `own` parameter is an owned local copy: mutating it is invisible to the
// caller, so it is allowed inside a pure function.
func TestPurity_OwnParamMutation_Ok(t *testing.T) {
	src := `
let bump = pure (p: own Point) -> Point => {
    p.x = 1
    p
}`
	assertPurityCount(t, checkPurity(t, src), 0)
}

// Interior mutation of a captured outer binding likewise escapes and is reported.
func TestPurity_CapturedInteriorMutation_Error(t *testing.T) {
	src := `
var origin = Point { x: 0, y: 0 }
let shift = pure (n: i64) -> void => {
    origin.x = n
}`
	assertPurityCount(t, checkPurity(t, src), 1)
}

// An impure function may freely mutate through a `mut` parameter — the
// constraint only applies inside `pure`.
func TestPurity_MutBorrowParam_ImpureOk(t *testing.T) {
	src := `
let reset = (p: mut Point) -> void => {
    p.x = 0
}`
	assertPurityCount(t, checkPurity(t, src), 0)
}
