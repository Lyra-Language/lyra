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

// Names bound by an `if let` destructuring are locals the pure function owns, so
// reassigning one inside the branch is not an escaping effect.
func TestPurity_IfLetBoundReassign_Ok(t *testing.T) {
	src := `
let f = pure (arr: [3]i64) -> i64 => {
    if let [a, b, c] = arr {
        a = 5
    }
    0
}`
	assertPurityCount(t, checkPurity(t, src), 0)
}

// A named `with`-arena handle is a local owned binding, so mutating its interior
// from a pure function is allowed (the mutation never escapes the call).
func TestPurity_WithArenaInteriorMutation_Ok(t *testing.T) {
	src := `
let f = pure () -> i64 => {
    with frame = Arena.new(megabytes(4)) {
        frame.counter = 1
    }
    0
}`
	assertPurityCount(t, checkPurity(t, src), 0)
}

// Collecting if-let bindings as locals must not mask a genuine escaping effect:
// mutating an actual captured `var` inside the branch is still reported.
func TestPurity_CapturedMutationInsideIfLet_Error(t *testing.T) {
	src := `
var counter = 0
let f = pure (arr: [3]i64) -> i64 => {
    if let [a, b, c] = arr {
        counter = a
    }
    0
}`
	assertPurityCount(t, checkPurity(t, src), 1)
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

// `await` suspends on external I/O, an observable effect, so it may not appear in
// a pure function.
func TestPurity_Await_Error(t *testing.T) {
	src := `
let f = pure (h: Handle) -> i64 => {
    let x = await fetch(h)
    x
}`
	assertPurityCount(t, checkPurity(t, src), 1)
}

// A `pure` function calling an un-annotated function that awaits is reported: the
// callee is inferred impure because it performs an await.
func TestPurity_CallsAwaitingFunction_Error(t *testing.T) {
	src := `
let load = (h: Handle) -> i64 => {
    await fetch(h)
}
let f = pure (h: Handle) -> i64 => {
    load(h)
}`
	errs := checkPurity(t, src)
	assertPurityCount(t, errs, 1)
	if errs[0].Message != `pure function calls impure function "load"` {
		t.Errorf("unexpected message: %q", errs[0].Message)
	}
}

// `await` in a non-pure function is fine — purity only constrains `pure`.
func TestPurity_AwaitInImpureFunction_Ok(t *testing.T) {
	src := `
let f = (h: Handle) -> i64 => {
    await fetch(h)
}`
	assertPurityCount(t, checkPurity(t, src), 0)
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

// Reading a captured `var` global from a pure function is non-deterministic (the
// value can change between calls), so it breaks referential transparency.
func TestPurity_ReadsCapturedVar_Error(t *testing.T) {
	src := `
var counter = 0
let peek = pure (n: i64) -> i64 => {
    n + counter
}`
	errs := checkPurity(t, src)
	assertPurityCount(t, errs, 1)
	want := `pure function reads captured mutable binding "counter"; its value can change between calls, breaking referential transparency`
	if errs[0].Message != want {
		t.Errorf("unexpected message: %q", errs[0].Message)
	}
}

// Reading a captured `let mut` global (frozen name, mutable interior) is likewise
// non-deterministic: the interior can change between calls.
func TestPurity_ReadsCapturedLetMut_Error(t *testing.T) {
	src := `
let mut origin = Point { x: 0, y: 0 }
let peek = pure (n: i64) -> i64 => {
    n + origin.x
}`
	assertPurityCount(t, checkPurity(t, src), 1)
}

// Reading a captured plain `let` (deeply immutable) is fine — its value never
// changes, so referential transparency holds.
func TestPurity_ReadsCapturedLet_Ok(t *testing.T) {
	src := `
let base = 100
let add = pure (n: i64) -> i64 => {
    n + base
}`
	assertPurityCount(t, checkPurity(t, src), 0)
}

// A mutable global used as an array index is a genuine read (not part of an
// assignment target), so it is still flagged inside a pure function.
func TestPurity_ReadsMutableGlobalAsIndex_Error(t *testing.T) {
	src := `
var idx = 0
let get = pure (xs: own Grid) -> i64 => {
    xs[idx]
}`
	assertPurityCount(t, checkPurity(t, src), 1)
}

// Writing a captured binding's field must not also be double-reported as a read
// of the base: a single mutation diagnostic is expected.
func TestPurity_CapturedFieldWrite_NotDoubleReported(t *testing.T) {
	src := `
var origin = Point { x: 0, y: 0 }
let shift = pure (n: i64) -> void => {
    origin.x = n
}`
	assertPurityCount(t, checkPurity(t, src), 1)
}

// A pure function that calls a user-defined function which only reads mutable
// global state is reported: the callee is inferred impure (non-deterministic)
// and the violation propagates across the call boundary.
func TestPurity_CallsFunctionReadingMutableGlobal_Error(t *testing.T) {
	src := `
var counter = 0
let snapshot = (n: i64) -> i64 => {
    n + counter
}
let record = pure (n: i64) -> i64 => {
    snapshot(n)
}`
	errs := checkPurity(t, src)
	assertPurityCount(t, errs, 1)
	if errs[0].Message != `pure function calls impure function "snapshot"` {
		t.Errorf("unexpected message: %q", errs[0].Message)
	}
}

// Reading a captured `var` declared in a *non-top-level* enclosing function is
// just as non-deterministic as reading a top-level global: the nested pure
// function's capture-stack lookup must walk out through the intermediate
// (impure) scope to find it.
func TestPurity_ReadsNonTopLevelCapturedVar_Error(t *testing.T) {
	src := `
let outer = (n: i64) -> i64 => {
    var counter = 0
    let peek = pure () -> i64 => {
        counter
    }
    peek()
}`
	errs := checkPurity(t, src)
	assertPurityCount(t, errs, 1)
	want := `pure function reads captured mutable binding "counter"; its value can change between calls, breaking referential transparency`
	if errs[0].Message != want {
		t.Errorf("unexpected message: %q", errs[0].Message)
	}
}

// Same as above, but the mutable binding is two scopes out (top-level `var`
// captured through an intermediate plain function before reaching the nested
// pure lambda) — the capture stack must accumulate frames across every level.
func TestPurity_ReadsCapturedVarThroughIntermediateScope_Error(t *testing.T) {
	src := `
var counter = 0
let outer = (n: i64) -> i64 => {
    let peek = pure () -> i64 => {
        counter
    }
    peek()
}`
	assertPurityCount(t, checkPurity(t, src), 1)
}

// A `let` declared in a non-top-level enclosing scope shadows an outer `var` of
// the same name: the nested pure function reads the closer, immutable
// declaration, so this must NOT be flagged even though a same-named binding
// further out is mutable.
func TestPurity_ShadowedImmutableLocal_Ok(t *testing.T) {
	src := `
var counter = 0
let outer = (n: i64) -> i64 => {
    let counter = 5
    let peek = pure () -> i64 => {
        counter
    }
    peek()
}`
	assertPurityCount(t, checkPurity(t, src), 0)
}

// A `pure` function that calls a sibling function declared in the same
// *non-top-level* enclosing scope is reported, just like calling a top-level
// impure function. `logIt` is inferred impure (mutates the outer-scope
// `counter`); calling it from the nested `pure record` propagates the
// violation, even though neither function is a top-level binding.
func TestPurity_CallsNonTopLevelImpureFunction_Error(t *testing.T) {
	src := `
let outer = (n: i64) -> i64 => {
    var counter = 0
    let logIt = (msg: string) -> void => {
        counter += 1
    }
    let record = pure (msg: string) -> i64 => {
        logIt(msg)
        n
    }
    record("x")
}`
	errs := checkPurity(t, src)
	assertPurityCount(t, errs, 1)
	want := `pure function calls impure function "logIt"`
	if errs[0].Message != want {
		t.Errorf("unexpected message: %q", errs[0].Message)
	}
}

// Sibling resolution within a scope must not depend on declaration order: the
// fixpoint inference should still find that `record` (declared *before*
// `logIt` and `counter` here) calls an impure sibling.
func TestPurity_CallsSiblingDeclaredAfter_Error(t *testing.T) {
	src := `
let outer = (n: i64) -> i64 => {
    let record = pure (msg: string) -> i64 => {
        logIt(msg)
        n
    }
    var counter = 0
    let logIt = (msg: string) -> void => {
        counter += 1
    }
    record("x")
}`
	assertPurityCount(t, checkPurity(t, src), 1)
}

// Two unrelated functions in different scopes that happen to share a name
// must not be confused with each other: `helper` in `scopeA` is impure, but
// the *different* `helper` in `scopeB` is pure, and `scopeB`'s pure `run`
// calls its own local `helper` — this must resolve to scopeB's (pure) one and
// report nothing, even though a same-named function elsewhere is impure.
func TestPurity_NameCollisionAcrossScopes_NotConfused(t *testing.T) {
	src := `
let scopeA = (n: i64) -> i64 => {
    var counter = 0
    let helper = (m: i64) -> i64 => {
        counter += 1
        m
    }
    helper(n)
}
let scopeB = (n: i64) -> i64 => {
    let helper = (m: i64) -> i64 => {
        m + 1
    }
    let run = pure (m: i64) -> i64 => {
        helper(m)
    }
    run(n)
}`
	assertPurityCount(t, checkPurity(t, src), 0)
}
