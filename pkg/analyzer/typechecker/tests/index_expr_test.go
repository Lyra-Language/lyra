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

// A negative index counts from the end, so `[-size, -1]` is in range.
func TestIndexExpr_StaticArray_NegativeInBounds_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let xs: [3]i64 = [1, 2, 3]
		let a = xs[-1]
		let b = xs[-3]
	`, false)
	assertNoErrors(t, res)
}

// A constant index below -size is still out of range (the negation folds).
func TestIndexExpr_StaticArray_NegativeOutOfBounds_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let xs: [3]i64 = [1, 2, 3]
		let y = xs[-4]
	`, false)
	assertErrorsAre(t, res, "index -4 out of range for array of size 3 (valid indices are -3 to 2)")
}

func TestIndexExpr_StaticArray_LiteralOutOfBounds_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let xs: [3]i64 = [1, 2, 3]
		let y = xs[3]
	`, false)
	assertErrorsAre(t, res, "index 3 out of range for array of size 3 (valid indices are -3 to 2)")
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
	assertErrorsAre(t, res, "index 10 out of range for array of size 3 (valid indices are -3 to 2)")
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
