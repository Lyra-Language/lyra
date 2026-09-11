package typechecker_test

import (
	"testing"
)

func TestIndexExpr_StaticArray_ReturnsElementType(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let xs: [3]i64 = [1, 2, 3]
		let y = xs[0]
	`, false)
	assertNoErrors(t, res)
}

func TestIndexExpr_DynamicArray_ReturnsElementType(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let xs: []string = ["a", "b"]
		let y = xs[1]
	`, false)
	assertNoErrors(t, res)
}

func TestIndexExpr_String_ReturnsChar(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let s = "hello"
		let c = s[0]
	`, false)
	assertNoErrors(t, res)
}

func TestIndexExpr_NonIntegerIndex_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let xs: [3]i64 = [1, 2, 3]
		let y = xs[1.5]
	`, false)
	assertErrorsAre(t, res, "index must be an integer, got float literal")
}

func TestIndexExpr_NonIndexable_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let x = 42
		let y = x[0]
	`, false)
	assertErrorsAre(t, res, "cannot index into type i64")
}

func TestIndexExpr_StaticArray_LiteralInBounds_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let xs: [3]i64 = [1, 2, 3]
		let y = xs[2]
	`, false)
	assertNoErrors(t, res)
}

// A constant negative index is refused, naming the spelling that replaced it
// (08/12). It counted from the end until then — which handed the most common
// off-by-one, an index underflowing past zero, a valid read of the wrong element in
// the language whose thesis is trap-over-silently-wrong. Provable → compile error
// here; a runtime negative → the bounds trap.
func TestIndexExpr_StaticArray_NegativeIndexRefused(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let xs: [3]i64 = [1, 2, 3]
		let a = xs[-1]
	`, false)
	assertErrorsAre(t, res,
		"index -1 is negative — an index does not count from the end; use `.from_end(1)` for the 1st value from the end")
}

// The hint's ordinal follows the magnitude, and a folded constant (`let i = -2`)
// is refused the same way a literal is.
func TestIndexExpr_StaticArray_NegativeConstBindingRefused(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let xs: [3]i64 = [1, 2, 3]
		let i: i64 = -2
		let a = xs[i]
	`, false)
	assertErrorsAre(t, res,
		"index -2 is negative — an index does not count from the end; use `.from_end(2)` for the 2nd value from the end")
}

// A string index gets the same refusal — the rule is the indexable surface's, not
// one container's.
func TestIndexExpr_String_NegativeIndexRefused(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let s = "abc"
		let c = s[-1]
	`, false)
	assertErrorsAre(t, res,
		"index -1 is negative — an index does not count from the end; use `.from_end(1)` for the 1st value from the end")
}

// from_end type-checks on all three receivers, with the element (rune) result type.
func TestIndexExpr_FromEndTypes(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let xs: [3]i32 = [1, 2, 3]
		let a: i32 = xs.from_end(1)
		let ds: []string = ["x", "y"]
		let b: string = ds.from_end(2)
		let s = "abc"
		let c: rune = s.from_end(1)
	`, false)
	assertNoErrors(t, res)
}

func TestIndexExpr_StaticArray_LiteralOutOfBounds_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let xs: [3]i64 = [1, 2, 3]
		let y = xs[3]
	`, false)
	assertErrorsAre(t, res, "index 3 out of range for array of size 3 (valid indices are 0 to 2)")
}

func TestIndexExpr_StaticArray_ElementType_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let xs: [3]i32 = [1, 2, 3]
		let y: i32 = xs[0]
	`, false)
	assertNoErrors(t, res)
}

func TestIndexExpr_StaticArray_ElementType_Widening_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let xs: [3]i32 = [1, 2, 3]
		let y: i64 = xs[0]
	`, false)
	assertErrorsAre(t, res, "y: cannot assign i32 to i64")
}

func TestIndexExpr_StaticArray_ElementType_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let xs: [3]i32 = [1, 2, 3]
		let str: string = xs[0]
	`, false)
	assertErrorsAre(t, res, "str: cannot assign i32 to string")
}

func TestIndexExpr_StaticArray_ConstLetIndex_OutOfBounds_Error(t *testing.T) {
	// A let binding with a literal initializer is a compile-time constant —
	// its value is knowable and out-of-bounds access must be caught.
	res := parseCollectAndCheck(t, `
		let xs: [3]i64 = [1, 2, 3]
		let i: i64 = 10
		let y = xs[i]
	`, false)
	assertErrorsAre(t, res, "index 10 out of range for array of size 3 (valid indices are 0 to 2)")
}

func TestIndexExpr_StaticArray_ConstLetIndex_InBounds_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let xs: [3]i64 = [1, 2, 3]
		let i: i64 = 2
		let y = xs[i]
	`, false)
	assertNoErrors(t, res)
}

func TestIndexExpr_Tuple_FirstElement_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let t: (i64, string) = (42, "hello")
		let x = t[0]
	`, false)
	assertNoErrors(t, res)
}

func TestIndexExpr_Tuple_SecondElement_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let t: (i64, string) = (42, "hello")
		let s = t[1]
	`, false)
	assertNoErrors(t, res)
}

func TestIndexExpr_Tuple_OutOfBounds_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let t: (i64, string) = (42, "hello")
		let x = t[2]
	`, false)
	assertErrorsAre(t, res, "tuple index 2 out of range for tuple with 2 elements")
}

func TestIndexExpr_Tuple_ConstLetIndex_Ok(t *testing.T) {
	// A let binding with a literal initializer resolves as a compile-time constant,
	// so tuple indexing through it is allowed.
	res := parseCollectAndCheck(t, `
		let t: (i64, string) = (42, "hello")
		let i: i64 = 1
		let x = t[i]
	`, false)
	assertNoErrors(t, res)
}

func TestIndexExpr_Tuple_RuntimeVariableIndex_Error(t *testing.T) {
	// A runtime value has no compile-time constant — tuple index must be a constant.
	res := parseCollectAndCheck(t, `
		let runtime = () => 0
		let t: (i64, string) = (42, "hello")
		var i: i64 = runtime()
		let x = t[i]
	`, false)
	assertErrorsAre(t, res, "tuple index must be an integer literal")
}

// ── a binding that can change is not a constant ─────────────────────────────
//
// `resolveConstantInt` folded **any** binding to its initializer, so a `var` reassigned into
// range was still reported as out of range by its initial value — a hard error on a correct
// program. Only a plain `let` or a `const` folds now: a `var` can be reassigned and a
// `let mut` can be written through `&mut`. The `let` case above
// (TestIndexExpr_StaticArray_ConstLetIndex_OutOfBounds_Error) is what must still fire.

func TestIndexExpr_ReassignedVarIndexIsNotAConstant(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let xs: [3]i64 = [1, 2, 3]
		var i: i64 = 5
		i = 0
		let y = xs[i]
	`, false)
	assertNoErrors(t, res)
}

func TestIndexExpr_LetMutIndexIsNotAConstant(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let set = (p: ^mut i64, v: i64) -> void => unsafe { p^ = v }
		let xs: [3]i64 = [1, 2, 3]
		let mut i: i64 = 5
		unsafe { set(&mut i, 0) }
		let y = xs[i]
	`, false)
	assertNoErrors(t, res)
}

// The negative-index check shares the folding, and so shared the bug.
func TestIndexExpr_ReassignedNegativeVarIsNotRefused(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let xs: [3]i64 = [1, 2, 3]
		var i: i64 = -1
		i = 2
		let y = xs[i]
	`, false)
	assertNoErrors(t, res)
}

// **A tuple index must be known at compile time, so a `var` is refused** — which is the
// fix, not a regression: it used to type `t[k]` by `k`'s *initializer*, whatever `k` had
// become by then.
func TestIndexExpr_TupleIndexThroughAVarIsRefused(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let t: (i64, string) = (1, "one")
		var k: i64 = 0
		k = 1
		let v = t[k]
	`, false)
	assertHasErrorContaining(t, res, "tuple index must be an integer literal")
}
