package llvm

import (
	"strings"
	"testing"
)

// A call dispatched through a `where` bound, lowered (08/07). The typechecker
// resolves it *abstractly* — to a trait and a method name — because at check time the
// receiver is a type variable and every implementing type answers the same. It
// becomes concrete only when a specialization fixes that variable, which is what these
// exercise: one generic body, two instantiations, two different impls called.
//
// Before this it was a hard error ("does not lower yet"), so `where t: Show` could be
// written and checked but never built — the last thing between the bound work and a
// usable `Show`.
const showImpls = `
trait Show { show: (Self) -> string }
impl Show for i64 {
  show = (self) => "an int"
}
impl Show for bool {
  show = (self) => "a bool"
}
`

func TestExec_BoundDispatchSelectsTheImplPerSpecialization(t *testing.T) {
	t.Parallel()
	src := showImpls + `
let describe<t> where t: Show = (v: t) -> string => v.show()
let main = () -> void => {
  println(describe(7));
  println(describe(true));
}
`
	out, code := buildAndRunCapture(t, src)
	if code != 0 {
		t.Fatalf("exited %d:\n%s", code, out)
	}
	if got := strings.TrimSpace(out); got != "an int\na bool" {
		t.Errorf("got %q; want \"an int\\na bool\"", got)
	}
}

// A call reaching a **supertrait's** method through a bound (08/14). The typechecker
// closes the in-scope bound set over supertrait edges, so `where t: Loud` resolves
// `v.show()` against `Show` — and the candidates it publishes are `Show`'s impls, which
// is why the backend needed nothing new for this. Worth an exec test anyway: "resolves
// abstractly" and "calls the right function" are different claims, and the second is
// the one a user notices.
func TestExec_BoundDispatchReachesASupertraitMethod(t *testing.T) {
	t.Parallel()
	src := showImpls + `
trait Loud: Show { volume: (Self) -> i64 }
impl Loud for i64 { volume = (self) => 11 }
impl Loud for bool { volume = (self) => 3 }
let describe<t> where t: Loud = (v: t) -> string => v.show()
let main = () -> void => {
  println(describe(7));
  println(describe(true));
}
`
	out, code := buildAndRunCapture(t, src)
	if code != 0 {
		t.Fatalf("exited %d:\n%s", code, out)
	}
	if got := strings.TrimSpace(out); got != "an int\na bool" {
		t.Errorf("got %q; want \"an int\\na bool\"", got)
	}
}

// An umbrella trait, lowered: `Arithmetic` declares nothing and bundles two operator
// traits, and `where t: Arithmetic` dispatches both `*` and `+` through the closure to
// `Vec2`'s impls. This is the shape the supertrait work exists for, and it needed the
// grammar change of the same day — a trait with no methods of its own was a syntax
// error, so the bundle could not be declared at all.
func TestExec_UmbrellaTraitBundlesOperators(t *testing.T) {
	t.Parallel()
	src := `
trait Addable { (_+_): (Self, Self) -> Self }
trait Mulable { (_*_): (Self, Self) -> Self }
trait Arithmetic: Addable + Mulable {}
struct Vec2 { x: i64 }
impl Addable for Vec2 { (_+_) = (self, o) => Vec2 { x: self.x + o.x } }
impl Mulable for Vec2 { (_*_) = (self, o) => Vec2 { x: self.x * o.x } }
impl Arithmetic for Vec2 {}
let combine<t> where t: Arithmetic = (a: t, b: t) -> t => a * b + a
let main = () -> void => {
  println(combine(Vec2 { x: 2 }, Vec2 { x: 3 }).x);
}
`
	out, code := buildAndRunCapture(t, src)
	if code != 0 {
		t.Fatalf("exited %d:\n%s", code, out)
	}
	if got := strings.TrimSpace(out); got != "8" {
		t.Errorf("got %q; want \"8\" (2*3 + 2)", got)
	}
}

// The bound travels through a second generic: `via` forwards to `describe`, and the
// specialization chain has to carry the binding all the way to the impl.
func TestExec_BoundDispatchThroughAForwardedBound(t *testing.T) {
	t.Parallel()
	src := showImpls + `
let describe<t> where t: Show = (v: t) -> string => v.show()
let via<u> where u: Show = (x: u) -> string => describe(x)
let main = () -> void => {
  println(via(7));
  println(via(false));
}
`
	out, code := buildAndRunCapture(t, src)
	if code != 0 {
		t.Fatalf("exited %d:\n%s", code, out)
	}
	if got := strings.TrimSpace(out); got != "an int\na bool" {
		t.Errorf("got %q; want \"an int\\na bool\"", got)
	}
}

// ── A generic impl is selectable as a candidate (08/14) ────────────────────
//
// The candidate tables are keyed by each impl's **written** target, which is right for a
// concrete impl (`impl Show for i64` keys `i64`, and a specialization looks up `i64`) and
// never matches for a generic one: `impl Add for Box<t>` keys the literal `Box<t>` while
// the specialization looks up `Box<i64>`. So a generic impl was unreachable through a
// bound in both forms — a method call and an operator — each dying in the backend with its
// own message for one cause.
//
// The missing keys are published where a concrete type is first known: the instantiation.

const genericBoxArith = `
struct Box<t> { v: t }
impl Add for Box<t> where t: Arithmetic { (_+_) = (self, o) => Box { v: self.v + o.v } }
impl Sub for Box<t> where t: Arithmetic { (_-_) = (self, o) => Box { v: self.v - o.v } }
impl Mul for Box<t> where t: Arithmetic { (_*_) = (self, o) => Box { v: self.v * o.v } }
impl Div for Box<t> where t: Arithmetic { (_/_) = (self, o) => Box { v: self.v / o.v } }
impl Arithmetic for Box<t> where t: Arithmetic {}
`

// An operator on a generic struct, reached through a bound. Note the bound is the
// **umbrella**: `Arithmetic` declares no methods, so `+` resolves through `Add`, and the
// candidate has to be published for the trait that declares the method rather than the one
// the bound names. Getting that wrong publishes nothing and looks exactly like no fix.
func TestExec_OperatorThroughABoundOnAGenericImpl(t *testing.T) {
	t.Parallel()
	src := genericBoxArith + `
let twice<u> where u: Arithmetic = (v: u) -> u => v + v
let main = () -> void => { println(twice(Box { v: 21 }).v); }
`
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "42" {
		t.Errorf("got %q; want \"42\"", got)
	}
}

// The same gap in the method-call form, which failed with a different message
// ("no impl of Sized2 for it") for the same reason.
func TestExec_MethodThroughABoundOnAGenericImpl(t *testing.T) {
	t.Parallel()
	src := `
trait Depth { depth: (Self) -> i64 }
struct Box<t> { v: t }
impl Depth for i64 { depth = (self) => 0 }
impl Depth<t> for Box<t> where t: Depth { depth = (self) => self.v.depth() + 1 }
let measure<u> where u: Depth = (x: u) -> i64 => x.depth()
let main = () -> void => {
  println(measure(7));
  println(measure(Box { v: Box { v: 7 } }));
}
`
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "0\n2" {
		t.Errorf("got %q; want \"0\\n2\"", got)
	}
}

// Nesting, with no bound in sight: `Box<Box<i64>> + Box<Box<i64>>` selects
// `impl Add for Box<t>` at `t = Box<i64>`, and *that body's* `self.v + o.v` must find the
// same impl one level down. The outer call site publishes for the outer type only, so the
// inner site is reached by the impl body's own publication.
func TestExec_OperatorOnANestedGenericImpl(t *testing.T) {
	t.Parallel()
	src := genericBoxArith + `
let main = () -> void => {
  let n = Box { v: Box { v: 21 } };
  println((n + n).v.v);
}
`
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "42" {
		t.Errorf("got %q; want \"42\"", got)
	}
}
