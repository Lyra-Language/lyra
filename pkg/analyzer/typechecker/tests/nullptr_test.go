package typechecker_test

import "testing"

// `nullptr` — the null raw pointer, and the reason the FFI can now tell a C function's
// failure from its success. Three rules carry the whole feature:
//
//   - it is an *untyped* literal, so a context supplies the pointee (lyra-E069 when none
//     does — it has no default, unlike every other untyped literal);
//   - it fills a `^T` slot and a `^mut T` slot alike, since a null address names no
//     storage and there is nothing for a mutability rule to protect;
//   - writing it and comparing pointers are **safe**. Only the deref that follows is
//     `unsafe`, which is what lets the guard sit outside the block it guards.

func TestNullPtr_AnnotatedBinding(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
let main = () -> void => {
  let p: ^i64 = nullptr
  println(p == nullptr)
}`, false))
}

// A mutable slot takes it too. The pointee is what a pointer context has to agree on;
// mutability is a permission to write, and there is nothing at a null address to write.
func TestNullPtr_FillsAMutableSlot(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
let main = () -> void => {
  let p: ^mut u8 = nullptr
  println(p == nullptr)
}`, false))
}

// **Neither the literal nor the comparison needs `unsafe`.** This is the shape the
// feature exists for: the check is safe, and the deref it guards is not. If this test
// starts requiring a block, the guard has become more ceremonious than the thing it
// protects and the rule has inverted.
func TestNullPtr_TheGuardNeedsNoUnsafeBlock(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
let main = () -> void => {
  var n: i64 = 5
  let p: ^i64 = unsafe { &n }
  if p != nullptr {
    println("${unsafe { p^ }}")
  }
}`, false))
}

// A context reaches the literal through a *call*, not only an annotation — the position
// every real FFI use is written in.
func TestNullPtr_AsAnArgument(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
let take = (p: ^u8) -> bool => p == nullptr
let main = () -> void => println(take(nullptr))`, false))
}

// A generic pointee resolves, which is what makes `std.ffi`'s `is_null` one function
// rather than one per width.
func TestNullPtr_GenericPointee(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
let is_null = pure noalloc (self: ^t) -> bool => self == nullptr
let main = () -> void => {
  var n: i64 = 5
  let p: ^i64 = unsafe { &n }
  println(is_null(p))
}`, false))
}

// `^mut T` and `^T` compare — mutability is not part of an address's identity.
func TestNullPtr_ComparesAcrossMutability(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
let main = () -> void => {
  var n: i64 = 5
  let a: ^mut i64 = unsafe { &mut n }
  let b: ^i64 = a
  println(a == b)
}`, false))
}

// lyra-E069: nothing said what it points at. The message names the two spellings that
// supply a type rather than treating this as an unbuilt feature.
func TestNullPtr_UnpinnedIsRefused(t *testing.T) {
	res := parseCollectAndCheck(t, `
let main = () -> void => {
  let p = nullptr
  println(p == nullptr)
}`, false)
	assertHasErrorContaining(t, res, "cannot tell what this points at")
}

// Two unpinned literals do not pin each other. Reported at each of them, since each is
// equally missing a type — inventing a pointee here would make the *next* use the error.
func TestNullPtr_ComparedWithItselfIsRefused(t *testing.T) {
	res := parseCollectAndCheck(t, `
let main = () -> void => println(nullptr == nullptr)`, false)
	assertHasErrorContaining(t, res, "cannot tell what this points at")
}

// A pointer is not an integer, and the untyped ladders do not meet. Without this the
// literal would fill an `i64` slot on the strength of both being "untyped".
func TestNullPtr_IsNotAnInteger(t *testing.T) {
	res := parseCollectAndCheck(t, `
let main = () -> void => {
  let n: i64 = nullptr
  println(n)
}`, false)
	assertHasErrorContaining(t, res, "null pointer literal")
}
