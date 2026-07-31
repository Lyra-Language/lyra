package checker_test

import (
	"strings"
	"testing"
)

// The *declared* half of effect polymorphism: `f: pure () -> t` on a parameter's type.
//
// The inferred half (effect_polymorphism_test.go) makes a higher-order function pure at the
// call sites that happen to pass a pure callback. A declared bound makes it pure for
// *every* caller, by constraining what may be passed — the claim a signature could not make
// before, since purity was not part of a function type at all.

// The function the declared half exists to make writable: `pure` on the definition, and the
// call through `f` costs nothing because the signature says what `f` may do.
func TestDeclaredBound_PureHigherOrderFunctionChecksAgainstItsBound(t *testing.T) {
	src := `
data Opt<t> = Nil | Just(t)
let or_else = pure (m: Opt<i64>, f: pure () -> i64) -> i64 => match m {
  Just(v) => v,
  Nil => f(),
}`
	assertPurityCount(t, checkPurity(t, src), 0)
}

// The bound binds *every* caller, not only pure ones: it is a property of the callee's
// signature, so an impure program may not quietly hand an impure callback to a pure slot.
func TestDeclaredBound_EnforcedEvenFromAnImpureCaller(t *testing.T) {
	src := `
let strict = pure (f: pure () -> i64) -> i64 => f()
var log = 0
let bump = () -> i64 => { log = 1  0 }
let caller = () -> i64 => strict(bump)`
	errs := checkPurity(t, src)
	assertPurityCount(t, errs, 1)
	if len(errs) == 1 && !strings.Contains(errs[0].Message, "mutates state outside itself") {
		t.Errorf("the diagnostic should say what the argument does, got %q", errs[0].Message)
	}
}

// The *inferred* effect is what is compared, not the annotation. Requiring the word `pure`
// on every callback a program writes would cost more than the bound is worth — and this is
// why assignability lets the two types through rather than reporting a shape mismatch.
func TestDeclaredBound_UnannotatedButInferredPureLambdaSatisfiesIt(t *testing.T) {
	src := `
let strict = pure (f: pure () -> i64) -> i64 => f()
let caller = () -> i64 => strict(() -> i64 => 41)`
	assertPurityCount(t, checkPurity(t, src), 0)
}

// A bound composes: a constrained parameter forwarded into a constrained slot is verified
// from its own declared type, since a parameter has no body to inspect.
func TestDeclaredBound_ConstrainedParameterForwards(t *testing.T) {
	src := `
let strict = pure (f: pure () -> i64) -> i64 => f()
let forward = pure (g: pure () -> i64) -> i64 => strict(g)`
	assertPurityCount(t, checkPurity(t, src), 0)
}

// ...and an *unconstrained* one does not: it promises nothing, so it cannot satisfy a bound.
// A bound the compiler cannot check is not a bound, so this is a rejection rather than a
// pass, and the message says what to do about it.
func TestDeclaredBound_UnconstrainedParameterCannotForward(t *testing.T) {
	src := `
let strict = pure (f: pure () -> i64) -> i64 => f()
let forward = (g: () -> i64) -> i64 => strict(g)`
	errs := checkPurity(t, src)
	assertPurityCount(t, errs, 1)
	if len(errs) == 1 && !strings.Contains(errs[0].Message, "declare the parameter it comes from") {
		t.Errorf("the diagnostic should be actionable, got %q", errs[0].Message)
	}
}

// `noalloc` is the orthogonal axis and travels the same way.
func TestDeclaredBound_NoAllocIsEnforced(t *testing.T) {
	src := `
data Node = Leaf | Branch(i64)
let hot = pure noalloc (f: pure noalloc () -> i64) -> i64 => f()
let allocates = () -> i64 => { let n: shared Node = Branch(1)  1 }
let bad = () -> i64 => hot(allocates)`
	errs := checkPurity(t, src)
	assertPurityCount(t, errs, 1)
	if len(errs) == 1 && !strings.Contains(errs[0].Message, "heap-allocates") {
		t.Errorf("expected the alloc violation to be named, got %q", errs[0].Message)
	}
}

// The ladder holds across bounds: `pure` ⊆ `det`, so a pure function satisfies a `det`
// requirement. Rejecting the stricter annotation for the looser slot would be backwards.
func TestDeclaredBound_PureSatisfiesADetBound(t *testing.T) {
	src := `
let timed = det (f: det () -> i64) -> i64 => f()
let caller = () -> i64 => timed(() -> i64 => 3)`
	assertPurityCount(t, checkPurity(t, src), 0)
}

// A parameter with no bound keeps the inferred behaviour — the two halves coexist, and
// adding the declared half must not have made every callback-taking function strict.
func TestDeclaredBound_UnboundedParameterStaysPolymorphic(t *testing.T) {
	src := `
let loose = (f: () -> i64) -> i64 => f()
var log = 0
let bump = () -> i64 => { log = 1  0 }
let impureCaller = () -> i64 => loose(bump)`
	assertPurityCount(t, checkPurity(t, src), 0)
}
