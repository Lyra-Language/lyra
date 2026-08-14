package typechecker_test

import "testing"

// A supertrait is a promise that every implementer of a trait also implements the ones
// it names — which is what would let a `where t: B` bound reach `A`'s methods.
//
// It was never checked. `TraitDeclStmt.Bounds` was collected and read by nobody, found
// 08/07 by sweeping the AST for fields with no consumer: of 119 exported fields, this
// was one of two genuine phantoms. So `trait B: A` parsed, `impl B for S` compiled with
// no `A` in sight, and the declaration said something the compiler did not enforce.
func TestSupertrait_UnsatisfiedIsRefused(t *testing.T) {
	res := parseCollectAndCheck(t, `
		trait A { foo: (Self) -> i64 }
		trait B: A { bar: (Self) -> i64 }
		struct Pt { x: i64 }
		impl B for Pt { bar = (self) => 2 }
	`, false)
	assertErrorsAre(t, res,
		"impl of B for Pt: B requires A, which Pt does not implement")
}

func TestSupertrait_SatisfiedIsFine(t *testing.T) {
	res := parseCollectAndCheck(t, `
		trait A { foo: (Self) -> i64 }
		trait B: A { bar: (Self) -> i64 }
		struct Pt { x: i64 }
		impl A for Pt { foo = (self) => 1 }
		impl B for Pt { bar = (self) => 2 }
	`, false)
	assertNoErrors(t, res)
}

// Declaration order must not matter: the impls are gathered up front precisely so a
// call can dispatch against one declared later, and the same has to hold here.
func TestSupertrait_SatisfiedByALaterImpl(t *testing.T) {
	res := parseCollectAndCheck(t, `
		trait A { foo: (Self) -> i64 }
		trait B: A { bar: (Self) -> i64 }
		struct Pt { x: i64 }
		impl B for Pt { bar = (self) => 2 }
		impl A for Pt { foo = (self) => 1 }
	`, false)
	assertNoErrors(t, res)
}

// A trait with no supertraits is unaffected — the check must not fire on the ordinary
// case, which is nearly every trait.
func TestSupertrait_PlainTraitIsUnaffected(t *testing.T) {
	res := parseCollectAndCheck(t, `
		trait Show { show: (Self) -> string }
		struct Pt { x: i64 }
		impl Show for Pt { show = (self) => "pt" }
	`, false)
	assertNoErrors(t, res)
}

// The other half, landed 08/14: the obligation above buys nothing unless a *use* site
// can rely on it. `where t: B` promises t implements A too, so A's methods must resolve
// on a value of type t — this reported *"type parameter t has no method `foo`"* until
// the in-scope bound set was closed over supertraits.
func TestSupertrait_BoundReachesSupertraitMethod(t *testing.T) {
	res := parseCollectAndCheck(t, `
		trait A { foo: (Self) -> i64 }
		trait B: A { bar: (Self) -> i64 }
		struct Pt { x: i64 }
		impl A for Pt { foo = (self) => 1 }
		impl B for Pt { bar = (self) => 2 }
		let use_foo<t> where t: B = (v: t) -> i64 => v.foo()
		let main = () => println(use_foo(Pt { x: 1 }))
	`, false)
	assertNoErrors(t, res)
}

// Forwarding: a `where u: B` parameter passed to a callee bounded `where t: A`. The
// supertrait *is* the proof that A holds, and the diagnostic this used to produce asked
// the author to add `where u: A` — a bound `B` already guarantees.
func TestSupertrait_BoundForwardsToSupertraitBound(t *testing.T) {
	res := parseCollectAndCheck(t, `
		trait A { foo: (Self) -> i64 }
		trait B: A { bar: (Self) -> i64 }
		struct Pt { x: i64 }
		impl A for Pt { foo = (self) => 1 }
		impl B for Pt { bar = (self) => 2 }
		let needs_a<t> where t: A = (v: t) -> i64 => v.foo()
		let via<u> where u: B = (v: u) -> i64 => needs_a(v)
		let main = () => println(via(Pt { x: 1 }))
	`, false)
	assertNoErrors(t, res)
}

// The closure is transitive, not one level: `C: B` and `B: A` means a `where t: C`
// bound reaches A.
func TestSupertrait_ClosureIsTransitive(t *testing.T) {
	res := parseCollectAndCheck(t, `
		trait A { foo: (Self) -> i64 }
		trait B: A { bar: (Self) -> i64 }
		trait C: B { baz: (Self) -> i64 }
		struct Pt { x: i64 }
		impl A for Pt { foo = (self) => 1 }
		impl B for Pt { bar = (self) => 2 }
		impl C for Pt { baz = (self) => 3 }
		let use_foo<t> where t: C = (v: t) -> i64 => v.foo()
		let main = () => println(use_foo(Pt { x: 1 }))
	`, false)
	assertNoErrors(t, res)
}

// A supertrait cycle is legal — it says the two traits are always implemented together,
// which checkTraitImpl then requires of every implementer — so the closure walk carries
// a visited set rather than assuming a DAG. Without one this hangs the typechecker,
// which is the failure mode a test has to pin: a compiler that never returns reads as
// an editor that froze.
func TestSupertrait_CycleTerminates(t *testing.T) {
	res := parseCollectAndCheck(t, `
		trait A: B { foo: (Self) -> i64 }
		trait B: A { bar: (Self) -> i64 }
		struct Pt { x: i64 }
		impl A for Pt { foo = (self) => 1 }
		impl B for Pt { bar = (self) => 2 }
		let use_both<t> where t: A = (v: t) -> i64 => v.foo() + v.bar()
		let main = () => println(use_both(Pt { x: 1 }))
	`, false)
	assertNoErrors(t, res)
}

// The same closure has to hold for a bound written on an **impl**, not only on a
// binding. pushGenericBounds and checkTraitImpl are twins by their own comment's
// admission, and a bound that reaches A's methods in one form and not the other is a
// bound that means different things depending on where it is written.
func TestSupertrait_ImplWhereClauseReachesSupertraitMethod(t *testing.T) {
	res := parseCollectAndCheck(t, `
		struct Box<t> { value: t }
		trait A { foo: (Self) -> i64 }
		trait B: A { bar: (Self) -> i64 }
		trait Peek { peek: (Self) -> i64 }
		impl Peek<t> for Box<t> where t: B {
			peek = (self) => self.value.foo()
		}
	`, false)
	assertNoErrors(t, res)
}

// An **umbrella** trait: no methods of its own, existing only to name a bundle of
// supertraits. This is the shape supertraits are written for, and it was a *syntax*
// error until 08/14 — a member list was non-empty by construction, a decision that was
// correct while a method-less trait meant nothing.
//
// Both halves have to hold: `impl Arithmetic for Vec2 {}` must satisfy the obligation
// (which requires the two impls beneath it), and `where t: Arithmetic` must reach both
// operators through the closure.
func TestSupertrait_UmbrellaTraitBundlesItsSupertraits(t *testing.T) {
	res := parseCollectAndCheck(t, `
		trait Addable { (_+_): (Self, Self) -> Self }
		trait Mulable { (_*_): (Self, Self) -> Self }
		trait Arithmetic: Addable + Mulable {}
		struct Vec2 { x: i64 }
		impl Addable for Vec2 { (_+_) = (self, o) => Vec2 { x: self.x + o.x } }
		impl Mulable for Vec2 { (_*_) = (self, o) => Vec2 { x: self.x * o.x } }
		impl Arithmetic for Vec2 {}
		let combine<t> where t: Arithmetic = (a: t, b: t) -> t => a * b + a
		let main = () => println(combine(Vec2 { x: 2 }, Vec2 { x: 3 }).x)
	`, false)
	assertNoErrors(t, res)
}

// The umbrella's obligation is the ordinary one, and it must still bite: an
// `impl Arithmetic for Vec2 {}` whose supertrait impls are missing is refused. Without
// this, "declares no methods" would quietly become "requires nothing".
func TestSupertrait_UmbrellaStillRequiresItsSupertraitImpls(t *testing.T) {
	res := parseCollectAndCheck(t, `
		trait Addable { (_+_): (Self, Self) -> Self }
		trait Mulable { (_*_): (Self, Self) -> Self }
		trait Arithmetic: Addable + Mulable {}
		struct Vec2 { x: i64 }
		impl Addable for Vec2 { (_+_) = (self, o) => Vec2 { x: self.x + o.x } }
		impl Arithmetic for Vec2 {}
	`, false)
	assertErrorsAre(t, res,
		"impl of Arithmetic for Vec2: Arithmetic requires Mulable, which Vec2 does not implement")
}

// An unsatisfied bound is still an error — the closure must widen what satisfies a
// bound, not stop checking. `t: A` does not give you B's methods; the edge runs one way.
func TestSupertrait_SubtraitMethodIsNotReachedFromSupertraitBound(t *testing.T) {
	res := parseCollectAndCheck(t, `
		trait A { foo: (Self) -> i64 }
		trait B: A { bar: (Self) -> i64 }
		let use_bar<t> where t: A = (v: t) -> i64 => v.bar()
	`, false)
	assertErrorsAre(t, res,
		"type parameter t has no method \"bar\"; add a `where t: Trait` bound whose trait declares it")
}
