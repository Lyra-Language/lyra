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
	res := parseCollectAndCheck(t, `let x: int = 1000000000000`, false)
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
// No overflow for non-integer targets (floats, strings, etc.)
// ---------------------------------------------------------------------------

func TestTypeCheck_Overflow_F64_NoCheck(t *testing.T) {
	// Integers can be assigned to floats; no range error should be produced.
	res := parseCollectAndCheck(t, `let x: f64 = 99999`, false)
	assertNoErrors(t, res)
}
