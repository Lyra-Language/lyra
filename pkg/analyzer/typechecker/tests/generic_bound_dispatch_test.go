package typechecker_test

import (
	"strings"
	"testing"
)

// Bounded polymorphism in a method body: with `where t: Show`, a call on a
// value of type `t` (`self.value.show()`) dispatches through the bound's trait
// signature — even though there is no concrete impl for `t` at this point.
func TestBoundDispatch_MethodOnGenericField(t *testing.T) {
	res := parseCollectAndCheck(t, `
struct Box<t> { value: t }
trait Show { show: (Self) -> string }
impl Show for i64 { show = (n) => "i" }
impl Show<t> for Box<t> where t: Show {
    show = (self) => self.value.show()
}`, false)
	assertNoErrors(t, res)
}

// Without a bound, calling a trait method on a bare type parameter is an error
// with actionable guidance — the value's type `t` provides no such method.
func TestBoundDispatch_NoBound_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
struct Box<t> { value: t }
trait Show { show: (Self) -> string }
impl Show<t> for Box<t> {
    show = (self) => self.value.show()
}`, false)
	assertErrorsAre(t, res,
		"type parameter t has no method \"show\"; add a `where t: Trait` bound whose trait declares it")
}

// The bound must actually declare the called method: `t: Show` does not provide
// `compare`, so the call is rejected.
func TestBoundDispatch_WrongMethod_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
struct Box<t> { value: t }
trait Show { show: (Self) -> string }
trait Ord { compare: (Self, Self) -> i64 }
impl Show<t> for Box<t> where t: Show {
    show = (self) => self.value.compare(self.value)
}`, false)
	assertErrorsAre(t, res,
		"type parameter t has no method \"compare\"; add a `where t: Trait` bound whose trait declares it")
}

// The bound-dispatched call's return type flows through: `show` returns string,
// observed by binding it to a typed local. (Impl method bodies aren't yet
// return-type-checked against the trait signature, so a typed `let` is the
// cleanest place to observe the inferred type.)
func TestBoundDispatch_ReturnTypeFlowsThrough(t *testing.T) {
	res := parseCollectAndCheck(t, `
struct Box<t> { value: t }
trait Show { show: (Self) -> string }
impl Show<t> for Box<t> where t: Show {
    show = (self) => {
        let n: i64 = self.value.show()
        "x"
    }
}`, false)
	assertErrorsAre(t, res, "n: cannot assign string to i64")
}

// A bound whose method returns Self resolves to the type parameter: dup's Self
// return substitutes to `t`, so binding the result to an i64 is a mismatch
// reporting `t` — proving Self resolved to the parameter, not something bogus.
func TestBoundDispatch_MethodReturningSelf(t *testing.T) {
	res := parseCollectAndCheck(t, `
struct Box<t> { value: t }
trait Dup { dup: (Self) -> Self }
impl Dup<t> for Box<t> where t: Dup {
    dup = (self) => {
        let n: i64 = self.value.dup()
        self
    }
}`, false)
	assertErrorsAre(t, res, "n: cannot assign t to i64")
}

// ── A matched impl's own `where` bounds are verified (08/14) ────────────────
//
// Matching on the target's *head* alone meant a generic impl satisfied a bound for every
// instantiation of its target, including ones it explicitly excludes. That was a
// documented first-cut limit — "the recursive obligation surfaces when that impl is itself
// dispatched" — and what it misses is the impl that **is** its constraint: an umbrella
// impl has no methods, so there is no later dispatch, and the bound check is the only
// place the question is ever asked.

const boxArith = `
trait Add { (_+_): (Self, Self) -> Self }
trait Arith: Add {}
struct Box<t> { v: t }
impl Add for Box<t> where t: Arith { (_+_) = (self, o) => Box { v: self.v + o.v } }
impl Arith for Box<t> where t: Arith {}
impl Add for i64 { (_+_) = (self, o) => self + o }
impl Arith for i64 {}
let twice<u> where u: Arith = (v: u) -> u => v + v
`

// The bug: `Box<string>` satisfied `where u: Arith` through `impl Arith for Box<t>`,
// because only the head `Box` was compared. It type-checked clean and died in the backend.
func TestBoundSatisfaction_ImplConstraintExcludesTheInstantiation(t *testing.T) {
	res := parseCollectAndCheck(t, boxArith+`
let main = () => { let s = Box { v: "x" }; println(twice(s).v) }
`, false)
	if len(res.errors) == 0 {
		t.Fatal("Box<string> must not satisfy `where u: Arith` — its impl requires t: Arith")
	}
	// The diagnostic has to name the *inner* failure: the outer type alone says
	// nothing about which part of it was wrong.
	if !strings.Contains(res.errors[0].Error(), "string does not implement Arith") {
		t.Errorf("want the nested reason, got: %v", res.errors[0])
	}
}

// The other direction, which is the one a fix like this breaks: an instantiation the
// impl's constraint *does* admit must still satisfy the bound.
func TestBoundSatisfaction_ImplConstraintAdmitsTheInstantiation(t *testing.T) {
	res := parseCollectAndCheck(t, boxArith+`
let main = () => { let n = Box { v: 1 }; println(twice(n).v) }
`, false)
	assertNoErrors(t, res)
}

// **A binding that is itself a type variable is skipped, not failed.** `impl Arith for
// Box<t> where t: Arith` is checked for its supertrait obligation with `t` still abstract;
// answering "t does not implement Add" there would report against the impl's own
// parameter, making every constrained generic impl an error. Whether `t` satisfies the
// bound is the enclosing declaration's question, which is checkGenericBounds' own rule.
func TestBoundSatisfaction_AbstractParameterIsNotAFailure(t *testing.T) {
	res := parseCollectAndCheck(t, boxArith, false)
	assertNoErrors(t, res)
}

// Nesting must terminate. Each step strips a layer of type argument, so the recursion ends
// on its own here — but the in-progress set is what keeps a chain that does *not* shrink
// from hanging the typechecker, which presents as a frozen editor rather than a crash.
func TestBoundSatisfaction_NestedInstantiationTerminates(t *testing.T) {
	res := parseCollectAndCheck(t, boxArith+`
let main = () => { let n = Box { v: Box { v: 1 } }; println(twice(n).v.v) }
`, false)
	assertNoErrors(t, res)
}

// **A method call on a bare type parameter reaches UFCS**, below the bound and above the
// error. A free function generic in its own receiver — which is how the prelude writes
// `min`/`max`/`clamp` — accepts a type-variable receiver, so `a.pick(b)` resolves to
// `pick(a, b)` and the two spellings of one call agree. Before 08/26 the method spelling
// reported *"type parameter t has no method"*, whose advice names a `where` bound the
// author has already written.
func TestGenericReceiver_ReachesAUFCSFunction(t *testing.T) {
	res := parseCollectAndCheck(t, `
trait Pickable { pure rank: (Self) -> i64 }
let pick<t> where t: Pickable = pure (self: t, other: t) -> t =>
  if self.rank() >= other.rank() { self } else { other }
let best<t> where t: Pickable = pure (a: t, b: t, c: t) -> t => a.pick(b).pick(c)
`, false)
	assertNoErrors(t, res)
}

// And the rung cannot conjure a method out of a free function written for some other
// receiver: a concrete `self` does not unify with a type variable, so the diagnostic a
// genuine miss deserves is unchanged.
func TestGenericReceiver_AConcreteSelfDoesNotMatchATypeParameter(t *testing.T) {
	res := parseCollectAndCheck(t, `
trait Pickable { pure rank: (Self) -> i64 }
let shout = pure (self: string) -> string => self
let nope<t> where t: Pickable = pure (a: t) -> string => a.shout()
`, false)
	assertErrorsAre(t, res,
		`type parameter t has no method "shout"; add a `+"`where t: Trait`"+` bound whose trait declares it`)
}

// **`Self` needs different advice**, because a trait method has no `where` clause to write
// one on. Inside a default body the type variable is `Self`, which no program declares and
// no method may constrain — so advising `where Self: Trait` names a spelling that does not
// exist, which is the failure mode lyra-E035 and lyra-E066 were both written to avoid.
//
// The answer is a **supertrait**: `trait Doubled: Marked` is how a default body demands
// something of every implementer, and it is what lyra-E040 then requires of each `impl`.
func TestBoundSatisfaction_SelfIsToldToUseASupertrait(t *testing.T) {
	res := parseCollectAndCheck(t, `
trait Marked { pure mark: (Self) -> i64 }
let twice<t> where t: Marked = pure (v: t) -> i64 => v.mark() * 2
trait Doubled {
  pure one: (Self) -> Self
  pure both: (Self) -> i64 = (self) => twice(self.one())
}
impl Doubled for i64 { one = pure (self) => self }
`, false)
	assertHasErrorContaining(t, res, "is instantiated at `Self`, which is not required to implement Marked")
	assertHasErrorContaining(t, res, "A trait method has no `where` clause")
	assertHasErrorContaining(t, res, "write `trait Doubled: Marked`")
}

// And taking that advice compiles: the supertrait puts the bound on `Self` for every
// implementer, which is exactly what the default body needed.
func TestBoundSatisfaction_ASupertraitSatisfiesSelfsBound(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
trait Marked { pure mark: (Self) -> i64 }
let twice<t> where t: Marked = pure (v: t) -> i64 => v.mark() * 2
trait Doubled: Marked {
  pure one: (Self) -> Self
  pure both: (Self) -> i64 = (self) => twice(self.one())
}
impl Marked for i64 { mark = pure (self) => self }
impl Doubled for i64 { one = pure (self) => self }
`, false))
}

// An ordinary type parameter keeps the `where` advice, which is the spelling that *does*
// exist for it — the two messages differ because the two fixes differ.
func TestBoundSatisfaction_AnOrdinaryParameterKeepsTheWhereAdvice(t *testing.T) {
	res := parseCollectAndCheck(t, `
trait Marked { pure mark: (Self) -> i64 }
let twice<t> where t: Marked = pure (v: t) -> i64 => v.mark() * 2
let outer<u> = pure (v: u) -> i64 => twice(v)
`, false)
	assertHasErrorContaining(t, res, "add `where u: Marked` to the enclosing declaration")
}
