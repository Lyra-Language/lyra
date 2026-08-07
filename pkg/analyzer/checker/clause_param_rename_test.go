package checker_test

import (
	"strings"
	"testing"
)

// A multi-clause function body binds its parameters positionally, so a clause may name a
// parameter something other than its declared name. Effect analysis resolves a call through
// that renamed binding to the parameter it destructures — the rename is a spelling, not a
// different value.
//
// It did not until 08/06: callableParams excluded clause lambdas outright, leaving only the
// name-keyed callback path, so the analysis was correct only when a clause happened to reuse
// the declared name. A rename made the call an unresolvable callee, charged AllEffects, and
// reported the enclosing function as both impure and allocating — with nothing at the
// declaration to explain either. The prelude's two `filter` combinators hit exactly this.
func TestPurity_ClauseRenamesCallbackParam_Ok(t *testing.T) {
	src := `
let apply = pure noalloc (self: Maybe<i64>, predicate: (i64) -> bool) -> bool {
    (Some v, pred) => pred(v),
    (None, _) => false,
}`
	assertPurityCount(t, checkPurity(t, src), 0)
}

// The same function with the clause reusing the declared name was always accepted. It is
// here so a regression that "fixes" the rename by loosening the whole check still has to
// keep this passing.
func TestPurity_ClauseReusesCallbackParamName_Ok(t *testing.T) {
	src := `
let apply = pure noalloc (self: Maybe<i64>, predicate: (i64) -> bool) -> bool {
    (Some v, predicate) => predicate(v),
    (None, _) => false,
}`
	assertPurityCount(t, checkPurity(t, src), 0)
}

// The clause form is sugar for this, and this had the same hole — the desugaring only made
// it reachable from something that reads as a plain function head. Fixing it at the match
// rather than at the clause list is what covers both.
func TestPurity_HandWrittenMatchRenamesCallbackParam_Ok(t *testing.T) {
	src := `
let apply = pure noalloc (self: Maybe<i64>, predicate: (i64) -> bool) -> bool => match (self, predicate) {
    (Some v, pred) => pred(v),
    (None, _) => false,
}`
	assertPurityCount(t, checkPurity(t, src), 0)
}

// The one-parameter case takes clauseScrutinee's other branch: the scrutinee is the
// parameter itself rather than a one-element tuple, and arm patterns are not tuples.
func TestPurity_SingleParamMatchRenamesCallback_Ok(t *testing.T) {
	src := `
let run = pure noalloc (f: pure noalloc () -> i64) -> i64 => match f {
    g => g(),
}`
	assertPurityCount(t, checkPurity(t, src), 0)
}

// A renamed parameter carrying a *declared* effect bound is resolved through the same map,
// so the bound is enforced under the new name. `f: () -> i64` promises nothing, so calling
// it from a `pure` function through the rename `g` must still be reported — the rename must
// not become a way to launder an unconstrained callback into a pure one.
func TestPurity_ClauseRenamesUnboundedCallbackInPureCaller_Reported(t *testing.T) {
	src := `
var counter = 0
let bump = () -> i64 => { counter += 1; counter }
let run = pure (self: Maybe<i64>, f: () -> i64) -> i64 {
    (Some v, _) => v,
    (None, g) => g(),
}
let caller = pure () -> i64 => run(None, bump)`
	errs := checkPurity(t, src)
	assertPurityCount(t, errs, 1)
	// The effect-polymorphism message, not the unresolvable-callee one — proof the call
	// through `g` resolved to the parameter rather than falling back to AllEffects, which
	// would also have produced *a* diagnostic here and so would not discriminate.
	if !strings.Contains(errs[0].Message, "the callback's effects are this call's") {
		t.Fatalf("expected the callback-argument diagnostic, got %q", errs[0].Message)
	}
	// Named as `run`'s signature spells it. `g` is the arm binding inside the body — a
	// name the caller cannot see and cannot act on.
	if !strings.Contains(errs[0].Message, "impure f argument") {
		t.Fatalf("expected the diagnostic to name the declared parameter `f`, got %q", errs[0].Message)
	}
}

// A parameter whose declared type carries an effect bound keeps it under the renamed
// binding: `f: pure () -> i64` may be called from a pure function, and the rename must not
// turn that into an unresolvable callee.
func TestPurity_ClauseRenamesBoundedCallback_Ok(t *testing.T) {
	src := `
let run = pure noalloc (self: Maybe<i64>, f: pure noalloc () -> i64) -> i64 {
    (Some v, _) => v,
    (None, g) => g(),
}`
	assertPurityCount(t, checkPurity(t, src), 0)
}

// The payload of a data pattern is not the argument. `v` names what is *inside* the first
// parameter, so a call through it must not be resolved against parameter 0's declared bound
// — that would charge the call against the wrong parameter, and would let a `Maybe<() -> t>`
// payload inherit a bound written on the `Maybe` itself.
func TestPurity_ClausePayloadIsNotTheArgument(t *testing.T) {
	src := `
let call_payload = pure (self: Maybe<() -> i64>, other: pure () -> i64) -> i64 {
    (Some v, _) => v(),
    (None, f) => f(),
}`
	// `v` is an unconstrained payload, so calling it is not provably pure: the point is
	// that it is judged on its own and not through `other`'s `pure` bound.
	errs := checkPurity(t, src)
	for _, e := range errs {
		if strings.Contains(e.Message, "other") {
			t.Fatalf("payload call was resolved against the wrong parameter: %v", errs)
		}
	}
}

// Clauses that disagree about which position a name refers to leave it unresolved rather
// than picking one: there is no single argument to charge a call through it against.
func TestPurity_ClausesDisagreeOnPosition_NoCrash(t *testing.T) {
	src := `
let swap = pure (a: pure () -> i64, b: pure () -> i64) -> i64 {
    (a, b) => a(),
    (b, a) => a(),
}`
	// No assertion on the diagnostics beyond not panicking and not silently resolving:
	// the conservative answer is what the clause form had before this was supported.
	checkPurity(t, src)
}
