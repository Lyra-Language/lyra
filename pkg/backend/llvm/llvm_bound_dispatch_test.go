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
