package llvm

import (
	"strings"
	"testing"
)

// The `@builtin(Ord)` marker, end to end (08/08). The prelude's trait carries it, so
// the compiler finds the trait comparison dispatches through by **identity** rather
// than by the spelling `Ord`.

// The hole the marker closes. A program declaring its own `trait Ord` used to have that
// trait taken for the prelude's: `Ver < Ver` resolved to the user's `compare`, and since
// it returns i64 rather than the prelude's `Ordering` the *backend* caught it —
// "Ord::compare must return the prelude's Ordering, got i64", which is rule 5 catching a
// front-end mistake rather than the front end declining to dispatch.
//
// Now the user's trait is an ordinary trait: its `compare` is callable by name, and the
// comparison operators are untouched by it.
func TestExec_UserTraitNamedOrdIsOrdinary(t *testing.T) {
	t.Parallel()
	const src = `
module main
trait Ord { compare: (Self, Self) -> i64 }
struct Ver { v: i64 }
impl Ord for Ver { compare = (self, other) => 99 }
let main = () -> void => {
  println("${Ver { v: 1 }.compare(Ver { v: 2 })} ${1 < 2} ${"a" < "b"}");
}
`
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "99 true true" {
		t.Errorf("shadowing trait = %q; want \"99 true true\" — a user `trait Ord` must not hijack comparison", got)
	}
}

// The prelude's marked trait still drives comparison, both for a hand-written impl and
// for a derived one.
func TestExec_MarkedOrdStillDrivesComparison(t *testing.T) {
	t.Parallel()
	const src = `
module main
struct Ver { v: i64 }
impl Ord for Ver { compare = (self, other) => self.v <=> other.v }
@derive(Ord)
struct Pt { x: i64, y: i64 }
let main = () -> void => {
  println("${Ver { v: 1 } < Ver { v: 2 }} ${Pt { x: 1, y: 2 } < Pt { x: 1, y: 3 }}");
}
`
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "true true" {
		t.Errorf("marked Ord = %q; want \"true true\"", got)
	}
}

// `Eq` carries its own marker, and overriding `==` still works through it.
func TestExec_MarkedEqStillOverridesEquality(t *testing.T) {
	t.Parallel()
	const src = `
module main
struct Ci { s: string }
impl Eq for Ci { eq = (self, other) => true }
let main = () -> void => {
  println("${Ci { s: "a" } == Ci { s: "b" }} ${1 == 2}");
}
`
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "true false" {
		t.Errorf("marked Eq = %q; want \"true false\"", got)
	}
}
