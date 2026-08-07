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
