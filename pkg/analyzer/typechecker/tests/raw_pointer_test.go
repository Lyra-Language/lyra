package typechecker_test

import "testing"

// Raw-pointer operations and `unsafe` blocks are refused — lyra-E051, 08/13.
//
// The surface exists at both ends but not in the middle: `^T` is a real type
// (types.RawPointerType unifies, substitutes and heads, and a newtype may wrap
// one), the grammar and collector build the nodes, and lyra-E011's unsafe-context
// policy checker is written and tested — but nothing infers these expressions and
// nothing lowers them. They fell to the typechecker's default arm as
// `unknown expression type "address_of_expr"`, which reads like an internal error
// rather than an unbuilt feature.
//
// What made this worse than an inert surface: **E011's advice could not be
// followed.** It told the reader a raw pointer "requires an `unsafe` block or
// function", and `unsafe { … }` was itself an unknown expression, so doing as the
// compiler said produced a different error. E011 is no longer reported (see
// driver.go) — its policy is right for the day pointers work, and until then it
// can only send a reader somewhere that does not exist.

func TestTypeCheck_AddressOf_Refused(t *testing.T) {
	res := parseCollectAndCheck(t, `
let main = () -> u8 => {
  var x: i64 = 5
  let p = &x
  0
}`, false)
	assertErrorsAre(t, res,
		"taking a raw pointer with `&` is not implemented: Lyra has no raw-pointer operations yet")
}

func TestTypeCheck_Deref_Refused(t *testing.T) {
	res := parseCollectAndCheck(t, `
let read = (p: ^i64) -> i64 => p^
let main = () -> u8 => 0`, false)
	assertErrorsAre(t, res,
		"dereferencing a raw pointer with `^` is not implemented: Lyra has no raw-pointer operations yet")
}

// The remedy E011 used to recommend is refused in its own right, which is the
// point: the advice and the thing it pointed at were both unbuilt.
func TestTypeCheck_UnsafeBlock_Refused(t *testing.T) {
	res := parseCollectAndCheck(t, `
let main = () -> u8 => {
  unsafe {
    println("hi")
  }
  0
}`, false)
	assertErrorsAre(t, res,
		"an `unsafe` block is not implemented: Lyra has no raw-pointer operations yet")
}

// An `unsafe` block's body is still checked, so the refusal does not hide every
// ordinary mistake inside it behind one error.
func TestTypeCheck_UnsafeBlock_BodyStillChecked(t *testing.T) {
	res := parseCollectAndCheck(t, `
let main = () -> u8 => {
  unsafe {
    let x: i64 = "not an int"
  }
  0
}`, false)
	assertErrorsAre(t, res,
		"an `unsafe` block is not implemented: Lyra has no raw-pointer operations yet",
		"x: cannot assign string to i64")
}

// One report per construct, not the old E011-plus-E001 pair on the same location.
func TestTypeCheck_AddressOf_SingleReport(t *testing.T) {
	res := parseCollectAndCheck(t, `
let main = () -> u8 => {
  var x: i64 = 5
  var p: ^i64 = &x
  0
}`, false)
	assertErrorsAre(t, res,
		"taking a raw pointer with `&` is not implemented: Lyra has no raw-pointer operations yet")
}

// `^T` remains a legal *type* — it resolves, and a signature mentioning one is
// not itself an error. Only the operations are missing, which is why the
// diagnostic is about them rather than about the annotation.
func TestTypeCheck_RawPointerTypeAnnotationStillResolves(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
let take = (p: ^i64) -> i64 => 0
let main = () -> u8 => 0`, false))
}
