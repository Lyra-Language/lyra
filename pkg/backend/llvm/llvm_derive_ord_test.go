package llvm

import (
	"strings"
	"testing"
)

// `@derive(Ord)` synthesizes the structural ordering as an **ordinary impl**, appended
// to the program by the collector. Everything downstream then treats it exactly as a
// hand-written one — which is why deriving needs no support in the typechecker,
// dispatch or the backend, and why the error cases below are the diagnostics those
// passes already had.
//
// `@derive(...)` parsed and was collected onto `TypeDeclStmt.Derives` from the start
// and read by nobody, so before 08/07 an unsupported derive compiled and did nothing.

// Lexicographic, in declaration order. The third field is what proves it is not just
// comparing the first: a and b differ only in `patch`.
func TestExec_DeriveOrdIsLexicographic(t *testing.T) {
	t.Parallel()
	const src = `
module main
@derive(Ord)
struct Ver { major: i64, minor: i64, patch: i64 }
let main = () -> void => {
  let a = Ver { major: 1, minor: 2, patch: 3 };
  let b = Ver { major: 1, minor: 2, patch: 4 };
  let c = Ver { major: 1, minor: 3, patch: 0 };
  println("${a < b} ${b < c} ${c < a} ${a < a} ${a <= a}");
}
`
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "true true false false true" {
		t.Errorf("got %q; want \"true true false false true\"", got)
	}
}

// A derived impl is a real impl, so a hand-written one beside it is the ordinary
// duplicate-impl error rather than a special case — the coherence check from earlier
// today doing its job on code nobody wrote.
func TestCheck_DeriveBesideAHandWrittenImplIsADuplicate(t *testing.T) {
	t.Parallel()
	const src = `
module main
@derive(Ord)
struct Ver { v: i64 }
impl Ord for Ver { compare = (self, other) => Equal }
let main = () -> void => { println("x") }
`
	diags := checkWithPrelude(t, src)
	if len(diags) == 0 {
		t.Fatal("a derived impl beside a hand-written one must be a duplicate")
	}
	if !strings.Contains(diags[0], "already implemented") {
		t.Errorf("expected the duplicate-impl diagnostic, got: %s", diags[0])
	}
}

// Deriving on a field that is not orderable is checked in the synthesized body, so it
// reports as an ordinary comparison error naming the field's type. No diagnostic of
// its own — the body is type-checked like any other.
func TestCheck_DeriveOrdOnAnUnorderableFieldIsRefused(t *testing.T) {
	t.Parallel()
	const src = `
module main
struct Inner { z: i64 }
@derive(Ord)
struct Outer { a: Inner }
let main = () -> void => { println("x") }
`
	diags := checkWithPrelude(t, src)
	if len(diags) == 0 {
		t.Fatal("deriving over a field with no ordering must be refused")
	}
	if !strings.Contains(diags[0], "Inner") {
		t.Errorf("the message should name the offending field type, got: %s", diags[0])
	}
}

// `@derive(Ord)` on a `data` type: by **constructor declaration order first, then by
// payload**. The language cannot read a variant's tag, so the comparison is a match over
// the pair — 3n arms rather than n-squared, which is what made it worth synthesizing.
func TestExec_DeriveOrdOnADataType(t *testing.T) {
	t.Parallel()
	const src = `
module main
@derive(Ord)
data Shape = Circle(i64) | Rect(i64, i64) | Dot
let main = () -> void => {
  println("${Circle(1) < Circle(2)} ${Circle(9) < Rect(0, 0)} ${Rect(1, 2) < Rect(1, 3)} ${Dot > Rect(9, 9)}");
  println("${Circle(2) < Circle(1)} ${Rect(0, 0) < Circle(9)} ${Dot == Dot}");
}
`
	want := "true true true true\nfalse false true"
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

// The shapes around the edges: an all-nullary enum (no payload comparison at all), a
// single constructor (the "last constructor needs only one arm" case, which is then the
// *only* arm), and mixed arities in one type.
func TestExec_DeriveOrdOnDataTypeEdgeShapes(t *testing.T) {
	t.Parallel()
	const src = `
module main
@derive(Ord)
data Color = Red | Green | Blue
@derive(Ord)
data One = Only(i64)
@derive(Ord)
data Mixed = A | B(i64) | C(i64, i64)
let main = () -> void => {
  println("${Red < Green} ${Green < Blue} ${Blue < Red} ${Red <= Red}");
  println("${Only(1) < Only(2)} ${Only(2) < Only(1)}");
  println("${A < B(1)} ${B(1) < B(2)} ${B(9) < C(0, 0)} ${C(1, 1) < C(1, 2)}");
}
`
	want := "true true false true\ntrue false\ntrue true true true"
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

// A derive naming a trait that does not exist yet is a *warning* — it is a no-op, not a
// mistake, and refusing the program over a feature that has not landed would be worse.
// Both beat the silence `@derive` had for its whole life.
func TestCheck_UnknownDeriveIsANoOpNotAnError(t *testing.T) {
	t.Parallel()
	const src = `
module main
@derive(Show)
struct Ver { v: i64 }
let main = () -> void => { println("x") }
`
	if diags := checkWithPrelude(t, src); len(diags) != 0 {
		t.Errorf("an unimplemented derive must not be an error: %v", diags)
	}
}
