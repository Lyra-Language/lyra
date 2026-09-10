package typechecker_test

import "testing"

// ---------------------------------------------------------------------------
// Signed integer overflow
// ---------------------------------------------------------------------------

func TestTypeCheck_Overflow_I8_TooLarge(t *testing.T) {
	res := parseCollectAndCheck(t, `let x: i8 = 200`, false)
	assertErrorsAre(t, res, "x: literal value 200 overflows i8")
}

func TestTypeCheck_Overflow_I8_MaxValid(t *testing.T) {
	res := parseCollectAndCheck(t, `let x: i8 = 127`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_Overflow_I8_MinValid_Negative(t *testing.T) {
	res := parseCollectAndCheck(t, `let x: i8 = -128`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_Overflow_I8_TooNegative(t *testing.T) {
	res := parseCollectAndCheck(t, `let x: i8 = -129`, false)
	assertErrorsAre(t, res, "x: literal value -129 overflows i8")
}

func TestTypeCheck_Overflow_I16_TooLarge(t *testing.T) {
	res := parseCollectAndCheck(t, `let x: i16 = 40000`, false)
	assertErrorsAre(t, res, "x: literal value 40000 overflows i16")
}

func TestTypeCheck_Overflow_I16_MaxValid(t *testing.T) {
	res := parseCollectAndCheck(t, `let x: i16 = 32767`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_Overflow_I16_TooNegative(t *testing.T) {
	res := parseCollectAndCheck(t, `let x: i16 = -32769`, false)
	assertErrorsAre(t, res, "x: literal value -32769 overflows i16")
}

func TestTypeCheck_Overflow_I32_TooLarge(t *testing.T) {
	res := parseCollectAndCheck(t, `let x: i32 = 3000000000`, false)
	assertErrorsAre(t, res, "x: literal value 3000000000 overflows i32")
}

func TestTypeCheck_Overflow_I32_MaxValid(t *testing.T) {
	res := parseCollectAndCheck(t, `let x: i32 = 2147483647`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_Overflow_I32_TooNegative(t *testing.T) {
	res := parseCollectAndCheck(t, `let x: i32 = -2147483649`, false)
	assertErrorsAre(t, res, "x: literal value -2147483649 overflows i32")
}

func TestTypeCheck_Overflow_I64_LargeValid(t *testing.T) {
	res := parseCollectAndCheck(t, `let x: i64 = 9223372036854775807`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_Overflow_Int_LargeValid(t *testing.T) {
	res := parseCollectAndCheck(t, `let x: i64 = 1000000000000`, false)
	assertNoErrors(t, res)
}

// ---------------------------------------------------------------------------
// Unsigned integer overflow
// ---------------------------------------------------------------------------

func TestTypeCheck_Overflow_U8_TooLarge(t *testing.T) {
	res := parseCollectAndCheck(t, `let x: u8 = 256`, false)
	assertErrorsAre(t, res, "x: literal value 256 overflows u8")
}

func TestTypeCheck_Overflow_U8_MaxValid(t *testing.T) {
	res := parseCollectAndCheck(t, `let x: u8 = 255`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_Overflow_U8_Zero(t *testing.T) {
	res := parseCollectAndCheck(t, `let x: u8 = 0`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_Overflow_U16_TooLarge(t *testing.T) {
	res := parseCollectAndCheck(t, `let x: u16 = 70000`, false)
	assertErrorsAre(t, res, "x: literal value 70000 overflows u16")
}

func TestTypeCheck_Overflow_U16_MaxValid(t *testing.T) {
	res := parseCollectAndCheck(t, `let x: u16 = 65535`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_Overflow_U32_TooLarge(t *testing.T) {
	res := parseCollectAndCheck(t, `let x: u32 = 5000000000`, false)
	assertErrorsAre(t, res, "x: literal value 5000000000 overflows u32")
}

func TestTypeCheck_Overflow_U32_MaxValid(t *testing.T) {
	res := parseCollectAndCheck(t, `let x: u32 = 4294967295`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_Overflow_U64_LargeValid(t *testing.T) {
	res := parseCollectAndCheck(t, `let x: u64 = 9223372036854775807`, false)
	assertNoErrors(t, res)
}

// ---------------------------------------------------------------------------
// Reassignment overflow
// ---------------------------------------------------------------------------

func TestTypeCheck_Overflow_Reassignment_I8_TooLarge(t *testing.T) {
	res := parseCollectAndCheck(t, `
		var x: i8 = 0
		x = 200
	`, false)
	assertErrorsAre(t, res, "x: literal value 200 overflows i8")
}

func TestTypeCheck_Overflow_Reassignment_I8_Valid(t *testing.T) {
	res := parseCollectAndCheck(t, `
		var x: i8 = 0
		x = 100
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_Overflow_Reassignment_U8_TooLarge(t *testing.T) {
	res := parseCollectAndCheck(t, `
		var x: u8 = 0
		x = 300
	`, false)
	assertErrorsAre(t, res, "x: literal value 300 overflows u8")
}

// ---------------------------------------------------------------------------
// Constant-folded arithmetic overflow
// ---------------------------------------------------------------------------

func TestTypeCheck_Overflow_Folded_Add_I8(t *testing.T) {
	res := parseCollectAndCheck(t, `let x: i8 = 100 + 100`, false)
	assertErrorsAre(t, res, "x: literal value 200 overflows i8")
}

func TestTypeCheck_Overflow_Folded_Add_I8_Valid(t *testing.T) {
	res := parseCollectAndCheck(t, `let x: i8 = 100 + 27`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_Overflow_Folded_Mul_I8(t *testing.T) {
	res := parseCollectAndCheck(t, `let x: i8 = 64 * 2`, false)
	assertErrorsAre(t, res, "x: literal value 128 overflows i8")
}

func TestTypeCheck_Overflow_Folded_Nested_I8(t *testing.T) {
	res := parseCollectAndCheck(t, `let x: i8 = 10 * 10 + 50`, false)
	assertErrorsAre(t, res, "x: literal value 150 overflows i8")
}

func TestTypeCheck_Overflow_Folded_Sub_Negative_I8(t *testing.T) {
	res := parseCollectAndCheck(t, `let x: i8 = 0 - 200`, false)
	assertErrorsAre(t, res, "x: literal value -200 overflows i8")
}

func TestTypeCheck_Overflow_Folded_U8(t *testing.T) {
	res := parseCollectAndCheck(t, `let x: u8 = 200 + 100`, false)
	assertErrorsAre(t, res, "x: literal value 300 overflows u8")
}

func TestTypeCheck_Overflow_Folded_Reassignment_I8(t *testing.T) {
	res := parseCollectAndCheck(t, `
		var x: i8 = 0
		x = 100 + 100
	`, false)
	assertErrorsAre(t, res, "x: literal value 200 overflows i8")
}

func TestTypeCheck_Overflow_Folded_I64_NoFalsePositive(t *testing.T) {
	// Fits comfortably in i64; folding must not report anything.
	res := parseCollectAndCheck(t, `let x: i64 = 1000000 * 1000000`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_Overflow_Folded_NonConstant_NotReported(t *testing.T) {
	// One operand is a variable, so the expression is not a compile-time
	// constant and this static check stays silent (runtime/range analysis is
	// a separate, deferred concern).
	res := parseCollectAndCheck(t, `
		let a: i8 = 100
		let x: i8 = a + 100
	`, false)
	assertNoErrors(t, res)
}

// ---------------------------------------------------------------------------
// No overflow for non-integer targets (floats, strings, etc.)
// ---------------------------------------------------------------------------

func TestTypeCheck_Overflow_F64_NoCheck(t *testing.T) {
	// Integers can be assigned to floats; no range error should be produced.
	res := parseCollectAndCheck(t, `let x: f64 = 99999`, false)
	assertNoErrors(t, res)
}

// The workspace CLAUDE.md's claim is that "a literal that cannot hold its value is a
// compile error in **every position**". That held for integers only until 09/10: a float
// literal too large for its target was silently `inf` in an annotation, an argument, a
// return, a struct field and an array element alike.
//
// Fifteen call sites funnel into `checkLiteralRange`, so this is one branch reaching all
// of them — which is exactly why the check lives there and not at a call site.
func TestOverflow_FloatLiteralOutOfRangeInEveryPosition(t *testing.T) {
	for _, c := range []struct{ name, src string }{
		{"annotation", `let a: f32 = 1.0e40`},
		{"argument", `
			let takes = pure (v: f32) -> f32 => v
			let a = takes(1.0e40)
		`},
		{"return", `let make = pure () -> f32 => 1.0e40`},
		{"array element", `let xs: []f32 = [1.0e40]`},
		{"struct field", `
			struct Point { x: f32 }
			let p = Point { x: 1.0e40 }
		`},
		{"repeated element", `let xs: []f32 = [1.0e40; 3]`},
	} {
		t.Run(c.name, func(t *testing.T) {
			res := parseCollectAndCheck(t, c.src, false)
			assertHasErrorContaining(t, res, "overflows f32")
		})
	}
}

// The same positions with a representable value stay clean, so the check is a bound and
// not a ban on large floats.
func TestOverflow_RepresentableFloatIsAcceptedEverywhere(t *testing.T) {
	for _, c := range []struct{ name, src string }{
		{"annotation", `let a: f32 = 3.0e38`},
		{"array element", `let xs: []f32 = [3.0e38]`},
		{"struct field", `
			struct Point { x: f32 }
			let p = Point { x: 3.0e38 }
		`},
	} {
		t.Run(c.name, func(t *testing.T) {
			assertNoErrors(t, parseCollectAndCheck(t, c.src, false))
		})
	}
}
