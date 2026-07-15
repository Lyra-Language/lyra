package checker_test

import (
	"testing"

	"github.com/Lyra-Language/lyra/pkg/analyzer/checker"
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector"
	"github.com/Lyra-Language/lyra/pkg/analyzer/typechecker"
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/ast/symbols"
	"github.com/Lyra-Language/lyra/pkg/parser"
	"github.com/Lyra-Language/lyra/pkg/typetable"
)

// checkPurity runs the typechecker first (so method-call dispatch is
// resolved, same as the real LSP pipeline) and passes its MethodTable into
// CheckPurity, so tests exercising trait-method purity work through the same
// helper as every other purity test.
func checkPurity(t *testing.T, source string) []checker.PurityError {
	t.Helper()
	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	c := collector.NewCollector([]byte(source))
	program, symTable, scopeTable, _ := c.Collect(tree.RootNode())
	tt := typetable.New()
	tc := typechecker.New(symTable, scopeTable, tt)
	tc.Check(program)
	return checker.CheckPurity(program, scopeTable, tt, tc.MethodTable())
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

// A `var` declared in a pure function's body and mutated inside a `for … in`
// loop body is a local: the body block is a descendant scope the frame builder
// flattens in, so `acc` resolves as owned. The loop variable `x` lives in the
// loop scope itself, which the builder skips (it was never a body binding), but
// the loop *body* block is still walked — a regression that dropped the whole
// for-in subtree would misread `acc` as captured and falsely flag this.
func TestPurity_ForInBodyLocalMutation_Ok(t *testing.T) {
	src := `
let total = pure (items: [3]i64) -> i64 => {
    var acc = 0
    for x in items {
        acc += x
    }
    acc
}`
	assertPurityCount(t, checkPurity(t, src), 0)
}

// The counterpart: mutating an actual captured `var` inside a `for … in` body is
// still an escaping effect. Confirms skipping the loop scope's own symbols did
// not accidentally swallow its body (which would suppress this diagnostic).
func TestPurity_CapturedMutationInsideForIn_Error(t *testing.T) {
	src := `
var sink = 0
let leak = pure (items: [3]i64) -> i64 => {
    for x in items {
        sink += x
    }
    0
}`
	assertPurityCount(t, checkPurity(t, src), 1)
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

// `await` suspends on external I/O, an observable effect, so it may not appear
// in a pure function. Calling an unresolved function (here `fetch`, which is
// external/imported and unverifiable) is also a purity violation, so two errors
// are reported: one for the `await` and one for the call to `fetch`.
func TestPurity_Await_Error(t *testing.T) {
	src := `
let f = pure (h: Handle) -> i64 => {
    let x = await fetch(h)
    x
}`
	errs := checkPurity(t, src)
	if len(errs) < 1 {
		t.Fatalf("expected at least 1 purity error, got 0")
	}
	hasAwaitErr := false
	for _, e := range errs {
		if e.Message == "pure function performs `await`; awaiting suspends on external I/O and must not cross the function boundary" {
			hasAwaitErr = true
		}
	}
	if !hasAwaitErr {
		t.Errorf("expected await purity error, got: %v", errs)
	}
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

// --- InferredPureFunctions: bottom-up purity inference, not just impurity ---

// TestInferredPureFunctions_UnannotatedPureFunction_True: a function with no
// `pure` keyword is still reported pure if it has no detected effect, making
// *pure* (not just impure) a recorded result usable by callers other than the
// purity checker itself.
func TestInferredPureFunctions_UnannotatedPureFunction_True(t *testing.T) {
	src := `
let explicitPure = pure (n: i64) -> i64 => { n + 1 }
let inferredPure = (n: i64) -> i64 => { n + 1 }`
	program, scopeTable := parseAndCollectProgram(t, src)
	result := checker.InferredPureFunctions(program, scopeTable)
	if !result["explicitPure"] {
		t.Error("explicitPure should be reported pure")
	}
	if !result["inferredPure"] {
		t.Error("inferredPure should be reported pure despite no `pure` keyword")
	}
}

// TestInferredPureFunctions_ImpureFunction_False: a function with a direct
// effect, or that transitively calls one, is reported impure.
func TestInferredPureFunctions_ImpureFunction_False(t *testing.T) {
	src := `
var counter = 0
let actuallyImpure = (n: i64) -> i64 => { counter = n; n }
let callsImpure = (n: i64) -> i64 => { actuallyImpure(n) }`
	program, scopeTable := parseAndCollectProgram(t, src)
	result := checker.InferredPureFunctions(program, scopeTable)
	if result["actuallyImpure"] {
		t.Error("actuallyImpure should be reported impure")
	}
	if result["callsImpure"] {
		t.Error("callsImpure should be reported impure (transitively, via actuallyImpure)")
	}
}

// TestInferredEffects_DistinguishesMutFromIO: InferredEffects (the effect-row
// generalization of InferredPureFunctions, FP/Imperative todo #5) reports
// *which* effect was found, not just impure/pure — a mutation-only function
// should carry EffectMut but not EffectIO, a print-only function the
// reverse, and a function combining both should carry both bits.
func TestInferredEffects_DistinguishesMutFromIO(t *testing.T) {
	src := `
var counter = 0
let mutates = (n: i64) -> i64 => {
    counter = n
    n
}
let prints = (n: i64) -> i64 => {
    print(n)
    n
}
let both = (n: i64) -> i64 => {
    counter = n
    print(n)
    n
}
let pureFn = (n: i64) -> i64 => { n + 1 }`
	program, scopeTable := parseAndCollectProgram(t, src)
	effects := checker.InferredEffects(program, scopeTable)

	if !effects["mutates"].Has(checker.EffectMut) || effects["mutates"].Has(checker.EffectIO) {
		t.Errorf("mutates: want EffectMut only, got %v", effects["mutates"])
	}
	if !effects["prints"].Has(checker.EffectIO) || effects["prints"].Has(checker.EffectMut) {
		t.Errorf("prints: want EffectIO only, got %v", effects["prints"])
	}
	if !effects["both"].Has(checker.EffectMut) || !effects["both"].Has(checker.EffectIO) {
		t.Errorf("both: want EffectMut|EffectIO, got %v", effects["both"])
	}
	if !effects["pureFn"].IsPure() {
		t.Errorf("pureFn: want EffectNone, got %v", effects["pureFn"])
	}
}

// --- EffectAlloc: use-site allocation (FP/Imperative #5) ---
//
// Allocation is a use-site flavor read off the TypeTable (a construction bound
// to a `shared` slot), so it is detected only on the CheckPurity path (which has
// the TypeTable); the AST-only InferredEffects helper never sets EffectAlloc.
// The alloc-detection behavior itself (shared construction violates `noalloc`,
// stack does not, arena discharges) lives in det_noalloc_enforcement_test.go.

// A `pure`-annotated function that allocates a `shared` value produces no
// purity diagnostic — allocation is orthogonal to `pure` enforcement.
func TestPurity_PureFunctionMayAllocate(t *testing.T) {
	src := `
struct Node { v: i64 }
let build = pure () -> i64 => {
    let n: shared Node = Node { v: 1 }
    n.v
}`
	assertPurityCount(t, checkPurity(t, src), 0)
}

func parseAndCollectProgram(t *testing.T, source string) (*ast.Program, *symbols.ScopeTable) {
	t.Helper()
	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	c := collector.NewCollector([]byte(source))
	program, _, scopeTable, _ := c.Collect(tree.RootNode())
	return program, scopeTable
}

// --- trait-method purity ---

// A pure caller invoking a method whose impl is itself marked `pure` is fine.
func TestPurity_PureMethodCalledFromPure_Ok(t *testing.T) {
	src := `
trait Show {
    show: (Self) -> string
}
impl Show for i64 {
    show = pure (n) => "x"
}
let f = pure (n: i64) -> string => {
    n.show()
}`
	assertPurityCount(t, checkPurity(t, src), 0)
}

// A pure caller invoking a method whose impl is NOT marked `pure` and whose
// body genuinely performs an effect is reported — the same escaping-call
// check a non-pure function gets, now extended to dispatch resolved via the
// type-checker. (An unannotated method with no actual effect is inferred
// pure instead — see TestInferredPureMethods_UnannotatedPureMethod_NotFlagged
// below — so the body here must call a known-impure builtin to stay
// impure under inference.)
func TestPurity_NonPureMethodCalledFromPure_Error(t *testing.T) {
	src := `
trait Show {
    show: (Self) -> string
}
impl Show for i64 {
    show = (n) => { println(n); "x" }
}
let f = pure (n: i64) -> string => {
    n.show()
}`
	errs := checkPurity(t, src)
	assertPurityCount(t, errs, 1)
	if errs[0].Message != `pure function calls non-pure trait method "show"` {
		t.Errorf("unexpected message: %q", errs[0].Message)
	}
}

// The fully-qualified call form gets the same treatment as a `.`-call.
func TestPurity_NonPureMethodCalledFromPure_QualifiedForm_Error(t *testing.T) {
	src := `
trait Show {
    show: (Self) -> string
}
impl Show for i64 {
    show = (n) => { println(n); "x" }
}
let f = pure (n: i64) -> string => {
    Show::show(n)
}`
	assertPurityCount(t, checkPurity(t, src), 1)
}

// --- InferredPureFunctions / CheckPurity: bottom-up purity inference for
// unannotated trait-impl methods (FP/Imperative todo #3, the method half) ---

// An unannotated method (no `pure` keyword) whose body has no detected
// effect is inferred pure, so calling it from a pure function is not
// flagged — mirroring how an unannotated free function is treated.
func TestInferredPureMethods_UnannotatedPureMethod_NotFlagged(t *testing.T) {
	src := `
trait Show {
    show: (Self) -> string
}
impl Show for i64 {
    show = (n) => "x"
}
let f = pure (n: i64) -> string => {
    n.show()
}`
	assertPurityCount(t, checkPurity(t, src), 0)
}

// An unannotated method that transitively calls a free function with a
// genuine effect is still flagged impure (the fixpoint propagates from a
// function callee back up into the method that calls it, not just between
// functions or within a single method body).
func TestInferredPureMethods_TransitiveImpurity_Flagged(t *testing.T) {
	src := `
let describe = (n: i64) -> string => { println(n); "x" }
trait Show {
    show: (Self) -> string
}
impl Show for i64 {
    show = (n) => describe(n)
}
let f = pure (n: i64) -> string => {
    n.show()
}`
	errs := checkPurity(t, src)
	assertPurityCount(t, errs, 1)
	if errs[0].Message != `pure function calls non-pure trait method "show"` {
		t.Errorf("unexpected message: %q", errs[0].Message)
	}
}

// A method explicitly marked `pure` is checked for its own internal
// violations, the same as a pure lambda.
func TestPurity_PureMethodBodyViolation_Error(t *testing.T) {
	src := `
var counter = 0
trait Show {
    show: (Self) -> string
}
impl Show for i64 {
    show = pure (n) => {
        counter = n
        "x"
    }
}`
	errs := checkPurity(t, src)
	assertPurityCount(t, errs, 1)
	want := `pure function reassigns captured binding "counter"; mutation must not escape the function`
	if errs[0].Message != want {
		t.Errorf("unexpected message: %q", errs[0].Message)
	}
}

// Calling a non-pure method from a non-pure function is unconstrained —
// purity only governs `pure` contexts.
func TestPurity_NonPureMethodInImpureFunction_Ok(t *testing.T) {
	src := `
trait Show {
    show: (Self) -> string
}
impl Show for i64 {
    show = (n) => "x"
}
let f = (n: i64) -> string => {
    n.show()
}`
	assertPurityCount(t, checkPurity(t, src), 0)
}

// --- method-to-method call tracking (FP/Imperative #3) ---

// A method body that calls another method on the same receiver, where the
// callee method is impure, must propagate impurity up through the MethodTable
// fixpoint so a pure caller is flagged.
func TestPurity_MethodToMethod_TransitiveImpurity_Error(t *testing.T) {
	src := `
trait Fmt {
    raw: (Self) -> string,
    display: (Self) -> string
}
impl Fmt for i64 {
    raw = (n) => { println(n); "x" },
    display = (n) => n.raw()
}
let f = pure (n: i64) -> string => {
    n.display()
}`
	errs := checkPurity(t, src)
	assertPurityCount(t, errs, 1)
	if errs[0].Message != `pure function calls non-pure trait method "display"` {
		t.Errorf("unexpected message: %q", errs[0].Message)
	}
}

// A method body that calls another method on the same receiver, where the
// callee is inferred pure, must not be flagged from a pure caller.
func TestPurity_MethodToMethod_PureChain_Ok(t *testing.T) {
	src := `
trait Fmt {
    raw: (Self) -> string,
    display: (Self) -> string
}
impl Fmt for i64 {
    raw = (n) => "x",
    display = (n) => n.raw()
}
let f = pure (n: i64) -> string => {
    n.display()
}`
	assertPurityCount(t, checkPurity(t, src), 0)
}

// --- imported-function impurity ---

// TestPurity_ImportedFunctionCall_TreatedAsImpure: a `pure` function that calls
// an imported function is flagged — we can't verify the callee's purity.
func TestPurity_ImportedFunctionCall_TreatedAsImpure(t *testing.T) {
	src := `
import math.{ sqrt }
let f = pure (x: i64) -> i64 => {
    sqrt(x)
}`
	errs := checkPurity(t, src)
	assertPurityCount(t, errs, 1)
	if errs[0].Message != `pure function calls impure function "sqrt"` {
		t.Errorf("unexpected message: %q", errs[0].Message)
	}
}

// TestPurity_ImportedFunctionCall_InferredImpure: an unannotated function that
// calls an imported function is inferred impure via InferredEffects.
func TestPurity_ImportedFunctionCall_InferredImpure(t *testing.T) {
	src := `
import http.{ get }
let fetch = (url: string) -> string => {
    get(url)
}`
	program, scopeTable := parseAndCollectProgram(t, src)
	result := checker.InferredPureFunctions(program, scopeTable)
	if result["fetch"] {
		t.Error("fetch calls an imported function and should be inferred impure")
	}
}

// TestPurity_TypeConversionCall_StillPure: numeric type-conversion calls like
// `i64(x)` are pure and must not be treated as imported/external calls.
func TestPurity_TypeConversionCall_StillPure(t *testing.T) {
	src := `
let f = pure (x: i32) -> i64 => {
    i64(x)
}`
	assertPurityCount(t, checkPurity(t, src), 0)
}

// TestPurity_ImportedFunctionCallTransitive_Impure: a local function that calls
// an imported function is inferred impure, and any function calling it is too.
func TestPurity_ImportedFunctionCallTransitive_Impure(t *testing.T) {
	src := `
import fs.{ readFile }
let loadConfig = (path: string) -> string => {
    readFile(path)
}
let bootstrap = () -> string => {
    loadConfig("config.txt")
}`
	program, scopeTable := parseAndCollectProgram(t, src)
	result := checker.InferredPureFunctions(program, scopeTable)
	if result["loadConfig"] {
		t.Error("loadConfig calls imported readFile and should be inferred impure")
	}
	if result["bootstrap"] {
		t.Error("bootstrap calls impure loadConfig and should be inferred impure transitively")
	}
}
