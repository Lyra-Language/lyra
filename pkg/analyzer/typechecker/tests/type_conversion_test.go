package typechecker_test

import (
	"testing"

	"github.com/Lyra-Language/lyra/pkg/ast"
)

// --- basic numeric conversions ---

func TestTypeCheck_TypeConversion_IntToF64(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let i: int = 123
		let f: f64 = f64(i)
	`)
	assertNoErrors(t, res)
}

func TestTypeCheck_TypeConversion_F64ToU32_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let f: f64 = 3.14
		let u: u32 = u32(f)
	`)
	assertErrorCount(t, res, 1)
	assertErrorContains(t, res, "cannot convert f64 to u32: use a rounding function")
}

func TestTypeCheck_TypeConversion_IntToI32(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let x: int = 100
		let y: i32 = i32(x)
	`)
	assertNoErrors(t, res)
}

func TestTypeCheck_TypeConversion_I32ToU64(t *testing.T) {
	// explicit signed→unsigned cast
	res := parseCollectAndCheck(t, `
		let x: i32 = 5
		let y: u64 = u64(x)
	`)
	assertNoErrors(t, res)
}

func TestTypeCheck_TypeConversion_UntypedIntToF32(t *testing.T) {
	// literal int cast to float via explicit conversion
	res := parseCollectAndCheck(t, `let f: f32 = f32(42)`)
	assertNoErrors(t, res)
}

func TestTypeCheck_TypeConversion_UntypedFloatToI64_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `let i: i64 = i64(3.14)`)
	assertErrorCount(t, res, 1)
	assertErrorContains(t, res, "use a rounding function")
}

// --- result type is recorded in TypeTable ---

func TestTypeCheck_TypeConversion_TypeTable(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let i: int = 1
		let f: f64 = f64(i)
	`)
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
		let x: int = int(s)
	`)
	assertErrorCount(t, res, 1)
	assertErrorContains(t, res, "cannot convert string to int")
}

func TestTypeCheck_TypeConversion_TooManyArgs(t *testing.T) {
	res := parseCollectAndCheck(t, `let x = i32(1, 2)`)
	assertErrorCount(t, res, 1)
	assertErrorContains(t, res, "i32: type conversion requires exactly 1 argument, got 2")
}

func TestTypeCheck_TypeConversion_NoArgs(t *testing.T) {
	res := parseCollectAndCheck(t, `let x = f64()`)
	assertErrorCount(t, res, 1)
	assertErrorContains(t, res, "f64: type conversion requires exactly 1 argument, got 0")
}

// --- float narrowing (lossy, blocked) ---

func TestTypeCheck_TypeConversion_F64ToF32_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let x: f64 = 3.14
		let y: f32 = f32(x)
	`)
	assertErrorCount(t, res, 1)
	assertErrorContains(t, res, "cannot convert f64 to f32: use a rounding function")
}

func TestTypeCheck_TypeConversion_F64ToF16_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let x: f64 = 3.14
		let y: f16 = f16(x)
	`)
	assertErrorCount(t, res, 1)
	assertErrorContains(t, res, "cannot convert f64 to f16: use a rounding function")
}

func TestTypeCheck_TypeConversion_F32ToF16_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let x: f32 = 1.5
		let y: f16 = f16(x)
	`)
	assertErrorCount(t, res, 1)
	assertErrorContains(t, res, "cannot convert f32 to f16: use a rounding function")
}

// --- float widening (lossless, allowed) ---

func TestTypeCheck_TypeConversion_F32ToF64(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let x: f32 = 1.5
		let y: f64 = f64(x)
	`)
	assertNoErrors(t, res)
}

func TestTypeCheck_TypeConversion_F16ToF64(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let x: f16 = 1.5
		let y: f64 = f64(x)
	`)
	assertNoErrors(t, res)
}

func TestTypeCheck_TypeConversion_F16ToF32(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let x: f16 = 1.5
		let y: f32 = f32(x)
	`)
	assertNoErrors(t, res)
}

func TestTypeCheck_TypeConversion_UntypedFloatToF32(t *testing.T) {
	// Literal float has no concrete precision yet — widening to any float is fine.
	res := parseCollectAndCheck(t, `let x: f32 = f32(3.14)`)
	assertNoErrors(t, res)
}

// --- annotation mismatch after conversion ---

func TestTypeCheck_TypeConversion_AnnotationMismatch(t *testing.T) {
	// Converting to i32 and then assigning to string annotation should error.
	res := parseCollectAndCheck(t, `
		let x: int = 5
		let y: string = i32(x)
	`)
	assertErrorCount(t, res, 1)
	assertErrorContains(t, res, "cannot assign i32 to string")
}
