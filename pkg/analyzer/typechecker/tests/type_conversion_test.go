package typechecker_test

import (
	"testing"

	"github.com/Lyra-Language/lyra/pkg/ast"
)

// --- basic numeric conversions ---

func TestTypeCheck_TypeConversion_IntToF64(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let i: i64 = 123
		let f: f64 = f64(i)
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_TypeConversion_F64ToU32_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let f: f64 = 3.14
		let u: u32 = u32(f)
	`, false)
	assertErrorsAre(t, res, "cannot convert f64 to u32: use floor(), ceil(), or round() to convert explicitly")
}

func TestTypeCheck_TypeConversion_IntToI32(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let x: i64 = 100
		let y: i32 = i32(x)
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_TypeConversion_I32ToU64(t *testing.T) {
	// explicit signed→unsigned cast
	res := parseCollectAndCheck(t, `
		let x: i32 = 5
		let y: u64 = u64(x)
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_TypeConversion_UntypedIntToF32(t *testing.T) {
	// literal i64 cast to float via explicit conversion
	res := parseCollectAndCheck(t, `let f: f32 = f32(42)`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_TypeConversion_UntypedFloatToI64_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `let i: i64 = i64(3.14)`, false)
	assertErrorsAre(t, res, "cannot convert float literal to i64: use floor(), ceil(), or round() to convert explicitly")
}

// --- result type is recorded in TypeTable ---

func TestTypeCheck_TypeConversion_TypeTable(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let i: i64 = 1
		let f: f64 = f64(i)
	`, false)
	decl := res.program.Statements[1].(*ast.VarDeclStmt)
	typ, ok := res.typeTable.Get(decl.Value)
	if !ok {
		t.Fatal("expected type table entry for conversion expr")
	}
	if got := typ.String(); got != "f64" {
		t.Errorf("expected f64, got %s", got)
	}
}

// --- error cases ---

func TestTypeCheck_TypeConversion_NonNumericArg(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let s: string = "hello"
		let x: i64 = i64(s)
	`, false)
	assertErrorsAre(t, res, "cannot convert string to i64")
}

func TestTypeCheck_TypeConversion_TooManyArgs(t *testing.T) {
	res := parseCollectAndCheck(t, `let x = i32(1, 2)`, false)
	assertErrorsAre(t, res, "i32: type conversion requires exactly 1 argument, got 2")
}

func TestTypeCheck_TypeConversion_NoArgs(t *testing.T) {
	res := parseCollectAndCheck(t, `let x = f64()`, false)
	assertErrorsAre(t, res, "f64: type conversion requires exactly 1 argument, got 0")
}

// --- float narrowing (lossy, blocked) ---

func TestTypeCheck_TypeConversion_F64ToF32_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let x: f64 = 3.14
		let y: f32 = f32(x)
	`, false)
	assertErrorsAre(t, res, "cannot convert f64 to f32: narrowing conversion may lose precision")
}

func TestTypeCheck_TypeConversion_F64ToF16_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let x: f64 = 3.14
		let y: f16 = f16(x)
	`, false)
	assertErrorsAre(t, res, "cannot convert f64 to f16: narrowing conversion may lose precision")
}

func TestTypeCheck_TypeConversion_F32ToF16_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let x: f32 = 1.5
		let y: f16 = f16(x)
	`, false)
	assertErrorsAre(t, res, "cannot convert f32 to f16: narrowing conversion may lose precision")
}

// --- float widening (lossless, allowed) ---

func TestTypeCheck_TypeConversion_F32ToF64(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let x: f32 = 1.5
		let y: f64 = f64(x)
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_TypeConversion_F16ToF64(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let x: f16 = 1.5
		let y: f64 = f64(x)
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_TypeConversion_F16ToF32(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let x: f16 = 1.5
		let y: f32 = f32(x)
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_TypeConversion_UntypedFloatToF32(t *testing.T) {
	// Literal float has no concrete precision yet — widening to any float is fine.
	res := parseCollectAndCheck(t, `let x: f32 = f32(3.14)`, false)
	assertNoErrors(t, res)
}

// --- integer narrowing of compile-time constants (lossy, blocked) ---

func TestTypeCheck_TypeConversion_U8Overflow_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `let x = u8(256)`, false)
	assertErrorsAre(t, res, "cannot convert 256 to u8: literal value is out of range")
}

func TestTypeCheck_TypeConversion_I8Overflow_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `let x = i8(300)`, false)
	assertErrorsAre(t, res, "cannot convert 300 to i8: literal value is out of range")
}

func TestTypeCheck_TypeConversion_UnsignedNegative_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `let x = u8(-1)`, false)
	assertErrorsAre(t, res, "cannot convert -1 to u8: literal value is out of range")
}

func TestTypeCheck_TypeConversion_FoldedConstantOverflow_Error(t *testing.T) {
	// The argument is folded (200 + 100 = 300) before the range check.
	res := parseCollectAndCheck(t, `let x = i8(200 + 100)`, false)
	assertErrorsAre(t, res, "cannot convert 300 to i8: literal value is out of range")
}

func TestTypeCheck_TypeConversion_IntInRange_OK(t *testing.T) {
	res := parseCollectAndCheck(t, `let x = u8(255)`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_TypeConversion_IntWidening_OK(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let a: i8 = 5
		let b = i64(a)
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_TypeConversion_NonConstantNarrowing_OK(t *testing.T) {
	// Non-constant int narrowing is deferred to a future value-range pass, so it
	// is intentionally NOT reported here even though it may overflow at runtime.
	res := parseCollectAndCheck(t, `
		let a: i64 = 5
		let b = i8(a)
	`, false)
	assertNoErrors(t, res)
}

// --- annotation mismatch after conversion ---

func TestTypeCheck_TypeConversion_AnnotationMismatch(t *testing.T) {
	// Converting to i32 and then assigning to string annotation should error.
	res := parseCollectAndCheck(t, `
		let x: i64 = 5
		let y: string = i32(x)
	`, false)
	assertErrorsAre(t, res, "y: cannot assign i32 to string")
}
