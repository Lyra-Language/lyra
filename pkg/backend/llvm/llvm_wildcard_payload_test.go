package llvm

import (
	"strings"
	"testing"
)

// A bare `_` standing for a whole multi-field payload — `Rect _` where
// `Rect(i64, i64)`. It parsed and type-checked and then failed to lower
// (`payload pattern for "Rect" not implemented yet`) while the arity-matched
// `Rect(_, _)` worked, so the two spellings of the same set of values disagreed.
//
// Found 08/07 writing the data-type `@derive(Ord)` by hand, which wants exactly this
// shape for its "self is the earlier variant" arms; the synthesis generates
// arity-matched wildcards and stepped around it, but a hand-written match still hit it.
//
// A wildcard binds nothing and tests nothing, so expanding it to one per field is exact
// rather than an approximation.
func TestExec_BareWildcardStandsForAMultiFieldPayload(t *testing.T) {
	t.Parallel()
	const src = `
module main
data Shape = Circle(i64) | Rect(i64, i64) | Tri(i64, i64, i64) | Dot
let name = (s: Shape) -> string => match s {
  Circle _ => "circle",
  Rect _ => "rect",
  Tri _ => "tri",
  Dot => "dot",
}
let main = () -> void => {
  println("${name(Circle(1))} ${name(Rect(1, 2))} ${name(Tri(1, 2, 3))} ${name(Dot)}");
}
`
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "circle rect tri dot" {
		t.Errorf("got %q; want \"circle rect tri dot\"", got)
	}
}

// The same through a *tuple* scrutinee, which is where the derive needs it and where the
// failure was originally hit.
func TestExec_BareWildcardPayloadUnderATupleScrutinee(t *testing.T) {
	t.Parallel()
	const src = `
module main
data Shape = Circle(i64) | Rect(i64, i64) | Tri(i64, i64, i64) | Dot
let both = (a: Shape, b: Shape) -> string => match (a, b) {
  (Tri _, Tri _) => "both tri",
  (Rect _, _) => "rect first",
  (_, Rect _) => "rect second",
  (_, _) => "other",
}
let main = () -> void => {
  println("${both(Tri(1,2,3), Tri(4,5,6))} ${both(Rect(1,2), Dot)} ${both(Dot, Rect(1,2))} ${both(Dot, Dot)}");
}
`
	want := "both tri rect first rect second other"
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != want {
		t.Errorf("got %q; want %q", got, want)
	}
}

// What is *not* fixed, and says so: a single **binding** for a multi-field payload
// (`Rect pair`) would bind the payload tuple as one value. That is a real feature and a
// different one, so it keeps an honest error naming the spelling that works — rather
// than the old message, which said "not implemented" about a form that was.
func TestExec_BindingAWholeMultiFieldPayloadStillErrors(t *testing.T) {
	t.Parallel()
	const src = `
module main
data Shape = Rect(i64, i64) | Dot
let f = (s: Shape) -> i64 => match s { Rect pair => 1, Dot => 0 }
let main = () -> void => { println("${f(Dot)}") }
`
	_, err := emitSource(t, src)
	if err == nil {
		t.Fatal("binding a whole multi-field payload should still be refused")
	}
	if !strings.Contains(err.Error(), "name the fields instead") {
		t.Errorf("the message should name the working spelling, got: %v", err)
	}
}
