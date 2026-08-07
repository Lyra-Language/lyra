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

// `@derive(Ord)` on a data type is refused with the fix, rather than going quiet:
// the derived ordering there is by constructor order and then payload, and the
// language has no way to read a tag, so the synthesis would be an N-squared match over
// both scrutinees. Worth building, not worth guessing at.
//
// A derive naming a trait that does not exist yet (`@derive(Show)`) is a *warning*
// instead — it is a no-op, not a mistake, and refusing the program over a feature that
// has not landed would be worse. Both beat the silence `@derive` had for its whole
// life, which is the phantom-builtin shape this compiler keeps digging out.
func TestCheck_DeriveOrdOnADataTypeIsRefused(t *testing.T) {
	t.Parallel()
	const src = `
module main
@derive(Ord)
data Color = Red | Green
let main = () -> void => { println("x") }
`
	diags := checkWithPrelude(t, src)
	if len(diags) == 0 {
		t.Fatal("`@derive(Ord)` on a data type must be reported")
	}
	if !strings.Contains(diags[0], "only implemented for structs") {
		t.Errorf("the message should name the limit and the fix, got: %s", diags[0])
	}
}
