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

// **A closure calling its enclosing function's bounded parameter** costs what the bound
// permits, as the function's own call to it does: whoever supplies the argument is held to
// the bound. Until 10/27 the closure's call fell to the function-value rung and was charged
// every effect, so wrapping a pure callback — `(v) => g(v)`, lui's `on_change={arranged}` —
// was refused as mutating where it was handed on to a `pure` parameter. Handing the
// parameter on bare from inside the closure is verified the same way.
func TestDeclaredBound_ClosureOverABoundedParameter(t *testing.T) {
	src := `
let apply = pure (f: pure (i64) -> i64, x: i64) -> i64 => f(x)
let wrapped = pure (g: pure (i64) -> i64) -> i64 => apply((v: i64) -> i64 => g(v), 1)
let handed = pure (g: pure (i64) -> i64) -> i64 => apply((v: i64) -> i64 => apply(g, v), 1)`
	assertPurityCount(t, checkPurity(t, src), 0)
}

// ...but only a *bounded* parameter: an unconstrained one promises nothing to the closure
// either; a name the body binds again — over the parameter — is that binding, not it; and a
// function value whose annotation says `pure` is still not trusted from a closure.
func TestDeclaredBound_ClosureOverAnythingElseIsNotTrusted(t *testing.T) {
	for _, c := range []struct{ name, src string }{
		{"unconstrained", `
let apply = pure (f: pure (i64) -> i64, x: i64) -> i64 => f(x)
let wrapped = (g: (i64) -> i64) -> i64 => apply((v: i64) -> i64 => g(v), 1)`},
		{"rebound in the body", `
let apply = pure (f: pure (i64) -> i64, x: i64) -> i64 => f(x)
var log = 0
let noisy = (n: i64) -> i64 => { log = n  n }
let wrapped = (g: pure (i64) -> i64) -> i64 => {
  let g = noisy
  apply((v: i64) -> i64 => g(v), 1)
}`},
		{"an annotated let", `
let apply = pure (f: pure (i64) -> i64, x: i64) -> i64 => f(x)
var log = 0
let noisy = (n: i64) -> i64 => { log = n  n }
let wrapped = () -> i64 => {
  let h: pure (i64) -> i64 = noisy
  apply((v: i64) -> i64 => h(v), 1)
}`},
	} {
		if errs := checkPurity(t, c.src); len(errs) == 0 {
			t.Errorf("%s: a closure calling it was handed to a pure parameter unrefused", c.name)
		}
	}
}

// **A name the function's own body binds again is that binding, not the parameter** — the rule
// the closure above has always had, and a body's own uses had not: `let g = noisy` beside `g:
// pure (…)` was trusted as the parameter at a call and an argument handed on, so a `pure`
// function ran `noisy` unrefused (11/01). Each binder that can take the name, each use.
func TestDeclaredBound_ABodyBindingOverABoundedParameterIsNotTrusted(t *testing.T) {
	const common = `
let apply = pure (f: pure (i64) -> i64, x: i64) -> i64 => f(x)
let open_apply = pure (f: (i64) -> i64, x: i64) -> i64 => f(x)
var log = 0
let noisy = (n: i64) -> i64 => { log = n  n }
`
	for _, c := range []struct{ name, src string }{
		{"a let, called", `
let f = pure (g: pure (i64) -> i64) -> i64 => {
  let g = noisy
  g(1)
}`},
		{"a let, handed to a bound", `
let f = (g: pure (i64) -> i64) -> i64 => {
  let g = noisy
  apply(g, 1)
}`},
		{"a let, handed to an open callback", `
let f = pure (g: pure (i64) -> i64) -> i64 => {
  let g = noisy
  open_apply(g, 1)
}`},
		{"a local lambda, handed on", `
let f = pure (g: (i64) -> i64) -> i64 => {
  let g = (n: i64) -> i64 => { log = n  n }
  open_apply(g, 1)
}`},
		{"an unbounded parameter, called", `
let f = (g: (i64) -> i64) -> i64 => {
  let g = noisy
  g(1)
}
let caller = pure () -> i64 => f(pure (n: i64) -> i64 => n)`},
		{"a payload's binding", `
let f = pure (g: pure (i64) -> i64, m: Maybe<(i64) -> i64>) -> i64 => match m { Some(g) => g(1), None => 0 }`},
		{"a payload's binding, handed on", `
let f = pure (g: pure (i64) -> i64, m: Maybe<(i64) -> i64>) -> i64 => match m { Some(g) => apply(g, 1), None => 0 }`},
		{"a loop variable", `
let f = pure (g: pure (i64) -> i64, fs: [](i64) -> i64) -> i64 => {
  var t = 0
  for g in fs { t += apply(g, 1) }
  t
}`},
		{"a let inside an alias's arm", `
let f = pure (h: pure (i64) -> i64) -> i64 => match h {
  g => {
    let g = noisy
    g(1)
  }
}`},
	} {
		if errs := checkPurity(t, common+c.src); len(errs) == 0 {
			t.Errorf("%s: a rebound name was trusted as the parameter", c.name)
		}
	}
}

// ...and what stays trusted: the parameter itself, a whole-value alias of it from the body's
// own match (what a multi-clause function becomes), and a `let` of another name.
func TestDeclaredBound_AParameterAndItsAliasesStayTrusted(t *testing.T) {
	src := `
let apply = pure (f: pure (i64) -> i64, x: i64) -> i64 => f(x)
let open_apply = pure (f: (i64) -> i64, x: i64) -> i64 => f(x)
let direct = pure (g: pure (i64) -> i64) -> i64 => g(1) + apply(g, 2)
let aliased = pure (h: pure (i64) -> i64) -> i64 => match h { g => g(1) + apply(g, 2) }
let same_name = pure (g: pure (i64) -> i64) -> i64 => match g { g => g(1) }
let clauses = pure (a: pure (i64) -> i64, b: i64) -> i64 => match (a, b) { (f, 0) => f(1), (g, n) => apply(g, n) }
let other_local = pure (g: pure (i64) -> i64) -> i64 => {
  let y = g(1)
  apply(g, y)
}
let forwards = (g: (i64) -> i64) -> i64 => open_apply(g, 1) + g(2)
let pure_caller = pure () -> i64 => forwards(pure (n: i64) -> i64 => n)
let inc = pure (n: i64) -> i64 => n + 1
let at_top_level = apply(inc, 1)`
	assertPurityCount(t, checkPurity(t, src), 0)
}
