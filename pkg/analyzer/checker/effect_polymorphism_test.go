package checker_test

import (
	"strings"
	"testing"
)

// Effect polymorphism over function-typed parameters.
//
// A higher-order function's effects are not a property of the function alone: what
// `or_else(m, f)` does depends on `f`. Before this, an unresolvable callee tainted
// AllEffects, so every combinator was maximally impure and the taint spread to its
// callers — no callback-taking function could be called from `pure` code at all.
//
// A function's stored effect is now its *base* (what its own body does) plus the set of
// parameters it calls; a call site pays base ∪ the effects of the arguments supplied for
// those parameters.

const orElseDecl = `
data Opt<t> = Nil | Just(t)
let or_else = (m: Opt<i64>, f: () -> i64) -> i64 => match m {
  Just(v) => v,
  Nil => f(),
}`

// The case the whole thing is for: a pure caller passing a pure callback.
func TestEffectPoly_PureCallerPureCallback(t *testing.T) {
	src := orElseDecl + `
let good = pure (m: Opt<i64>) -> i64 => or_else(m, () -> i64 => 0)`
	assertPurityCount(t, checkPurity(t, src), 0)
}

// And the case that must not become legal along with it. The diagnostic points at the
// *argument*, not the callee: `or_else` is innocent, and naming it would send the reader
// to the wrong file.
func TestEffectPoly_PureCallerImpureCallback(t *testing.T) {
	src := orElseDecl + `
var counter = 0
let bump = () -> i64 => { counter = counter + 1  counter }
let bad = pure (m: Opt<i64>) -> i64 => or_else(m, bump)`
	errs := checkPurity(t, src)
	assertPurityCount(t, errs, 1)
	if len(errs) == 1 && !strings.Contains(errs[0].Message, "impure f argument") {
		t.Errorf("the diagnostic should name the callback parameter, got %q", errs[0].Message)
	}
}

// An inline impure lambda is caught the same way — the argument does not have to be a
// named function for its effects to be attributed to the call.
func TestEffectPoly_InlineImpureCallback(t *testing.T) {
	src := orElseDecl + `
var log = 0
let bad = pure (m: Opt<i64>) -> i64 => or_else(m, () -> i64 => { log = 2  0 })`
	assertPurityCount(t, checkPurity(t, src), 1)
}

// A higher-order function may now be *annotated* `pure`: the annotation constrains its own
// body, and effects arriving through its parameters are the caller's. Without this the
// prelude could not annotate `unwrap_or_else` at all, which is what started this.
func TestEffectPoly_HigherOrderFunctionMayBeAnnotatedPure(t *testing.T) {
	src := `
data Opt<t> = Nil | Just(t)
let or_else = pure noalloc (m: Opt<i64>, f: () -> i64) -> i64 => match m {
  Just(v) => v,
  Nil => f(),
}`
	assertPurityCount(t, checkPurity(t, src), 0)
}

// A callback handed straight on stays polymorphic rather than being charged for the
// hand-off — otherwise a combinator built out of another combinator would be exactly as
// poisoned as before, and the standard library is full of those.
func TestEffectPoly_CallbackPassedOnwardStaysPolymorphic(t *testing.T) {
	src := orElseDecl + `
let wrapper = (m: Opt<i64>, g: () -> i64) -> i64 => or_else(m, g)
let good = pure (m: Opt<i64>) -> i64 => wrapper(m, () -> i64 => 7)`
	assertPurityCount(t, checkPurity(t, src), 0)
}

// ...and the impure callback is still caught through that extra level.
func TestEffectPoly_ImpureCallbackThroughAWrapper(t *testing.T) {
	src := orElseDecl + `
let wrapper = (m: Opt<i64>, g: () -> i64) -> i64 => or_else(m, g)
var log = 0
let bump = () -> i64 => { log = 1  0 }
let bad = pure (m: Opt<i64>) -> i64 => wrapper(m, bump)`
	assertPurityCount(t, checkPurity(t, src), 1)
}

// Polymorphism is about *callbacks*, not about calls in general: a function that calls a
// known-impure function is still impure at every call site, callbacks or not.
func TestEffectPoly_BodyEffectsAreStillCharged(t *testing.T) {
	src := orElseDecl + `
var log = 0
let logging = (m: Opt<i64>, f: () -> i64) -> i64 => { log = 1; or_else(m, f) }
let bad = pure (m: Opt<i64>) -> i64 => logging(m, () -> i64 => 0)`
	errs := checkPurity(t, src)
	assertPurityCount(t, errs, 1)
	if len(errs) == 1 && !strings.Contains(errs[0].Message, `impure function "logging"`) {
		t.Errorf("a callee impure in its own body should be named, got %q", errs[0].Message)
	}
}

// A parameter that is never called is not a callback, so passing an impure function for it
// costs nothing — the effect is only charged where the call actually happens.
func TestEffectPoly_UncalledParameterIsNotACallback(t *testing.T) {
	src := `
let ignores = (f: () -> i64) -> i64 => 1
var log = 0
let bump = () -> i64 => { log = 1  0 }
let good = pure () -> i64 => ignores(bump)`
	assertPurityCount(t, checkPurity(t, src), 0)
}

// A namespace-qualified callee (`maybe.map(…)`) resolves through its last segment. It used
// to have no resolution at all, so **every** cross-module call from a pure function was
// reported impure — which made the std.maybe/std.result split unusable from pure code
// however pure the callee was.
func TestEffectPoly_NamespaceQualifiedCalleeResolves(t *testing.T) {
	src := `
let double = (n: i64) -> i64 => n * 2
let good = pure (n: i64) -> i64 => util.double(n)`
	assertPurityCount(t, checkPurity(t, src), 0)
}

// The namespace fallback must not fire when the object names a *binding*: `holder.run` is
// then an ordinary field read, and resolving it to some unrelated top-level `run` would
// attribute the wrong body's effects to the call.
func TestEffectPoly_NamespaceFallbackDoesNotOvertakeALocalBinding(t *testing.T) {
	src := `
var log = 0
let run = () -> i64 => { log = 1  0 }
let f = pure (holder: i64) -> i64 => holder.run()`
	// Not resolvable to the top-level `run` — the object is a binding — so it stays the
	// conservative "external callee" case and is reported.
	assertPurityCount(t, checkPurity(t, src), 1)
}
