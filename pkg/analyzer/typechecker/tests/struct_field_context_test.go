package typechecker_test

import "testing"

// A struct literal's field is a context for the value written in it, so a generic call
// whose type variables **no argument mentions** is solved from the field's declared type.
//
// This is the position that was missing: an annotated binding, a declared return and an
// argument slot all reached seedFromExpectedReturn, and a field reached none of them —
// so a generic constructor, whose whole shape is "takes nothing, returns the thing", was
// uncallable in the one place a record of them is assembled.
func TestStructFieldIsAContextForACallsTypeArguments(t *testing.T) {
	res := parseCollectAndCheck(t, `
struct Bag<t> { items: []t }
let bag_new<t> = pure () -> Bag<t> => Bag { items: [] }
struct Holder { b: Bag<string> }
let main = () -> void => {
  let h = Holder { b: bag_new() }
}
`, false)
	assertNoErrors(t, res)
}

// The binding form has always worked and must keep working — it is the control the bug
// report was written against, and the two spellings should agree.
func TestAnnotatedBindingAndStructFieldAgreeOnAGenericConstructor(t *testing.T) {
	res := parseCollectAndCheck(t, `
struct Bag<t> { items: []t }
let bag_new<t> = pure () -> Bag<t> => Bag { items: [] }
struct Holder { b: Bag<string> }
let main = () -> void => {
  let viaBinding: Bag<string> = bag_new()
  let viaField = Holder { b: bag_new() }
}
`, false)
	assertNoErrors(t, res)
}

// A nested field reaches it too: the context pushed for the outer field is the inner
// literal's own declaration, so the recursion needs nothing of its own.
func TestANestedStructFieldIsAContextToo(t *testing.T) {
	res := parseCollectAndCheck(t, `
struct Bag<t> { items: []t }
let bag_new<t> = pure () -> Bag<t> => Bag { items: [] }
struct Inner { b: Bag<string> }
struct Outer { inner: Inner }
let main = () -> void => {
  let o = Outer { inner: Inner { b: bag_new() } }
}
`, false)
	assertNoErrors(t, res)
}

// The sibling positions, which had the identical gap. A struct field was the reported
// one; a named tuple's element and a data constructor's payload are the same aggregate
// position spelled differently, and fixing only the reported one is the "list of
// aggregate forms with one missing" shape this compiler keeps paying for (hazard 8).
//
// Both are written **non-generic** on purpose: a non-generic declaration returns early
// from solveDataTypeVars with no parameters to solve, so its elements are first inferred
// somewhere else entirely — which is why one push could not cover both.
func TestANamedTupleElementIsAContextForACallsTypeArguments(t *testing.T) {
	res := parseCollectAndCheck(t, `
struct Bag<t> { items: []t }
let bag_new<t> = pure () -> Bag<t> => Bag { items: [] }
tuple Pair(Bag<string>, i64)
let main = () -> void => {
  let p = Pair(bag_new(), 1)
}
`, false)
	assertNoErrors(t, res)
}

func TestADataConstructorPayloadIsAContextForACallsTypeArguments(t *testing.T) {
	res := parseCollectAndCheck(t, `
struct Bag<t> { items: []t }
let bag_new<t> = pure () -> Bag<t> => Bag { items: [] }
data Wrapper = Empty | Wrap(Bag<string>)
let main = () -> void => {
  let w = Wrap(bag_new())
}
`, false)
	assertNoErrors(t, res)
}

// The generic spelling of each, which goes through solveDataTypeVars instead. A payload
// declared as the type's *own* parameter (`Some(t)`) has no context to give — that
// variable is what the solve is for — so this checks the guard holds and the ordinary
// solve still works.
func TestAGenericConstructorPayloadStillSolvesFromItsArgument(t *testing.T) {
	res := parseCollectAndCheck(t, `
data Maybe<t> = None | Some(t)
struct Bag<t> { items: []t }
let bag_new<t> = pure () -> Bag<t> => Bag { items: [] }
data Holder<t> = Hold(Bag<string>, t)
let main = () -> void => {
  let s = Some(5)
  let h = Hold(bag_new(), 7)
}
`, false)
	assertNoErrors(t, res)
}

// The fourth member of the family, and the one whose fix is different in kind: an
// anonymous struct has no declaration to read a field type from, so the context is the
// ambient annotation narrowed by field name. Applying it afterwards — which
// propagateExpectedType's anonymous-struct arm already did — is too late, because the
// generic call has already reported.
func TestAnAnonymousStructFieldIsAContextForACallsTypeArguments(t *testing.T) {
	res := parseCollectAndCheck(t, `
struct Bag<t> { items: []t }
let bag_new<t> = pure () -> Bag<t> => Bag { items: [] }
let main = () -> void => {
  let a: { b: Bag<string> } = { b: bag_new() }
}
`, false)
	assertNoErrors(t, res)
}

// An anonymous struct with no annotation at all still infers from its own leaves — the
// push is skipped, not guessed at, and nothing that worked before needs a context now.
func TestAnUnannotatedAnonymousStructStillInfersFromItsLeaves(t *testing.T) {
	res := parseCollectAndCheck(t, `
let main = () -> void => {
  let a = { x: 1, y: "s" }
  let n: i64 = a.x
}
`, false)
	assertNoErrors(t, res)
}

// The seed must not paper over a genuine disagreement. The context says `Bag<string>`
// and the call is pinned to `Bag<i64>` by its turbofish, so this is still an error —
// seeding is a seed, not an override.
func TestAStructFieldContextDoesNotOverrideATurbofish(t *testing.T) {
	res := parseCollectAndCheck(t, `
struct Bag<t> { items: []t }
let bag_new<t> = pure () -> Bag<t> => Bag { items: [] }
struct Holder { b: Bag<string> }
let main = () -> void => {
  let h = Holder { b: bag_new::<i64>() }
}
`, false)
	assertHasErrorContaining(t, res, "cannot assign")
}
