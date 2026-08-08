package typechecker_test

import "testing"

// An anonymous struct is assignable to itself (08/08).
//
// It was not, which made the type unusable as a value: a literal's field types come from
// its own leaves, so `{ x: 1 }` is `{ x: untyped_int }`, and `isAssignable` had no
// anonymous-struct arm — it fell through to `TypesEqual`, which compares field types
// *exactly*. The result was **"cannot assign struct to struct"**, a type refused against
// itself with the message naming the same thing twice.
//
// The anonymous *tuple* arm directly above it in `assignable.go` is the same rule and had
// been there all along: hazard 8, in a list of aggregate forms with one missing.

func TestAnonymousStruct_AssignableToItself(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
let a: { x: i64 } = { x: 1 }
let b: { x: u8, y: string } = { x: 200, y: "s" }
`, false))
}

// Fields match by **name**, not by position, so writing them in another order is the
// same type — which is what distinguishes this from the tuple rule.
func TestAnonymousStruct_FieldsMatchByName(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
let c: { y: string, x: u8 } = { x: 1, y: "s" }
`, false))
}

// An untyped literal field narrows to the annotation, exactly as a tuple element does —
// and having narrowed, it must still fit.
func TestAnonymousStruct_FieldLiteralMustFitTheAnnotation(t *testing.T) {
	res := parseCollectAndCheck(t, `
let a: { x: u8 } = { x: 300 }
`, false)
	assertHasErrorContaining(t, res, "field x: literal value 300 overflows u8")
}

// The genuine mismatches are still refused — and the message now renders the fields,
// since an anonymous struct *is* its fields and printing every one of them as the bare
// word `struct` is indistinguishable from the self-rejection this fixed.
func TestAnonymousStruct_MismatchesAreRefusedAndReadable(t *testing.T) {
	for _, tc := range []struct{ src, want string }{
		{`let a: { x: i64 } = { x: "s" }`, "cannot assign { x: string } to { x: i64 }"},
		{`let a: { x: i64 } = { y: 1 }`, "cannot assign { y: integer literal } to { x: i64 }"},
		{`let a: { x: i64 } = { x: 1, y: 2 }`, "to { x: i64 }"},
	} {
		res := parseCollectAndCheck(t, "\n"+tc.src+"\n", false)
		assertHasErrorContaining(t, res, tc.want)
	}
}
