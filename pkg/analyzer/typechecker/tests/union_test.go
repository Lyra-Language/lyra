package typechecker_test

import "testing"

// A C union: one block of storage its members read several ways, declared for the FFI.
//
// It is **not** a `data` type, and the whole feature follows from the difference. A
// `data` type carries a tag, which is what makes `match` on it safe; a union carries
// nothing, so every rule below is the same fact restated — *nothing records which member
// is live*:
//
//   - a literal names exactly one member;
//   - reading a member is `unsafe`;
//   - a union has no equality, because there is no member to compare;
//   - every member must have a C layout, which is also what makes the ownership walk's
//     "a union owns nothing" sound.

func TestUnion_DeclaresAndReadsUnderUnsafe(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
union Ev { kind: u32, code: i64 }
let main = () -> void => {
  let e = Ev { kind: 7 }
  println("${unsafe { e.kind }}")
}`, false))
}

// The rule the type exists to enforce. Without it a union member read would be spelled
// exactly like a safe struct field access — which is the trade this language already
// refused once, keeping pointer arithmetic a named method rather than `p[i]`.
func TestUnion_MemberReadNeedsUnsafe(t *testing.T) {
	res := parseCollectAndCheck(t, `
union Ev { kind: u32, code: i64 }
let main = () -> void => {
  let e = Ev { kind: 7 }
  println("${e.kind}")
}`, false)
	assertHasErrorContaining(t, res, "requires an `unsafe` block")
}

// Exactly one member: naming two would write both to the same bytes and keep the last,
// so the literal would quietly discard whichever the reader wrote first.
func TestUnion_LiteralNamesExactlyOneMember(t *testing.T) {
	res := parseCollectAndCheck(t, `
union Ev { kind: u32, code: i64 }
let main = () -> void => {
  let e = Ev { kind: 7, code: 9 }
  println("${unsafe { e.kind }}")
}`, false)
	assertHasErrorContaining(t, res, "names exactly one member")
}

func TestUnion_UnknownMemberIsRefused(t *testing.T) {
	res := parseCollectAndCheck(t, `
union Ev { kind: u32 }
let main = () -> void => {
  let e = Ev { nope: 7 }
  println("${unsafe { e.kind }}")
}`, false)
	assertHasErrorContaining(t, res, `has no member "nope"`)
}

// A member with no C layout. The message names the rule rather than the symptom: a union
// is a C type, so its members are held to what C can represent.
func TestUnion_MemberMustHaveACLayout(t *testing.T) {
	res := parseCollectAndCheck(t, `
union Bad { s: string, n: u32 }
let main = () -> void => println("x")`, false)
	assertHasErrorContaining(t, res, "no C representation")
}

// **An array or struct member is admitted**, and that is the line between this rule and
// lyra-E063's. E063 asks whether a type can *cross a function boundary by value* and
// refuses every aggregate; this asks whether it has a *layout*, and structs and arrays do
// — which they must, since a C union is made of them.
func TestUnion_ArrayAndStructMembersAreAdmitted(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
struct Payload { a: u32, b: f64 }
union Ev { kind: u32, user: Payload, padding: [64]u8 }
let main = () -> void => {
  let e = Ev { kind: 7 }
  println("${unsafe { e.user.a }}")
}`, false))
}

// A union has no equality: C does not give one either, and there is no member to compare.
func TestUnion_HasNoEquality(t *testing.T) {
	res := parseCollectAndCheck(t, `
union Ev { kind: u32 }
let main = () -> void => {
  let a = Ev { kind: 1 }
  let b = Ev { kind: 1 }
  println("${a == b}")
}`, false)
	assertHasErrorContaining(t, res, "a union has no equality")
}

// A union crosses the boundary by pointer, exactly as a struct does — by *value* is still
// lyra-E063, since that needs the per-target classifier the struct case is waiting on too.
func TestUnion_CrossesTheBoundaryByPointer(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
union Ev { kind: u32, code: i64 }
unsafe extern fill: (out: ^mut Ev) -> void
let main = () -> void => {
  var e = Ev { kind: 0 }
  unsafe { fill(&mut e) }
  println("${unsafe { e.kind }}")
}`, false))
}
