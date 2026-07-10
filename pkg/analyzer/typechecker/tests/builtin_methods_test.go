package typechecker_test

import "testing"

// TestBuiltin_IntOverflowArithmetic_Ok verifies every registered integer
// overflow-arithmetic builtin type-checks on a concrete integer receiver and
// returns the same integer type (proven by the `: i8` annotation accepting it).
func TestBuiltin_IntOverflowArithmetic_Ok(t *testing.T) {
	ops := []string{
		"wrapping_add", "wrapping_sub", "wrapping_mul",
		"saturating_add", "saturating_sub", "saturating_mul",
	}
	for _, op := range ops {
		src := "let a: i8 = 100\nlet b: i8 = a." + op + "(1)\n"
		res := parseCollectAndCheck(t, src, false)
		assertNoErrors(t, res)
	}
}

// TestBuiltin_WorksOnAllConcreteInts checks the builtins specialize to the
// receiver's own integer type (signed and unsigned, every width).
func TestBuiltin_WorksOnAllConcreteInts(t *testing.T) {
	intTypes := []string{"i8", "i16", "i32", "i64", "u8", "u16", "u32", "u64"}
	for _, ty := range intTypes {
		src := "let a: " + ty + " = 5\nlet b: " + ty + " = a.wrapping_add(1)\n"
		res := parseCollectAndCheck(t, src, false)
		assertNoErrors(t, res)
	}
}

// TestBuiltin_ReturnTypeIsReceiverType: the result is the receiver's integer
// type, so binding it to a different type is a type error.
func TestBuiltin_ReturnTypeIsReceiverType(t *testing.T) {
	res := parseCollectAndCheck(t, "let a: i8 = 1\nlet b: string = a.wrapping_add(1)\n", false)
	assertErrorsAre(t, res, "b: cannot assign i8 to string")
}

// TestBuiltin_ArgMustMatchReceiver: the argument is checked against the
// receiver's integer type.
func TestBuiltin_ArgMustMatchReceiver(t *testing.T) {
	res := parseCollectAndCheck(t, "let a: i8 = 1\nlet b = a.wrapping_add(\"x\")\n", false)
	assertErrorsAre(t, res, `wrapping_add: argument 1: cannot assign string to i8`)
}

// TestBuiltin_WrongArgCount is reported by the shared call-arity check.
func TestBuiltin_WrongArgCount(t *testing.T) {
	res := parseCollectAndCheck(t, "let a: i8 = 1\nlet b = a.wrapping_add()\n", false)
	assertErrorsAre(t, res, "wrapping_add: expected 1 argument(s), got 0")
}

// TestBuiltin_NotOnFloat: the overflow-arithmetic builtins are integer-only.
func TestBuiltin_NotOnFloat(t *testing.T) {
	res := parseCollectAndCheck(t, "let x: f64 = 1.0\nlet y = x.wrapping_add(1.0)\n", false)
	assertErrorsAre(t, res, `f64 has no method "wrapping_add"`)
}

// TestBuiltin_UnknownMethodOnPrimitive reports a missing method, not the old
// "member access on non-struct type" (a primitive is a valid receiver now).
func TestBuiltin_UnknownMethodOnPrimitive(t *testing.T) {
	res := parseCollectAndCheck(t, "let a: i8 = 1\nlet b = a.frobnicate(1)\n", false)
	assertErrorsAre(t, res, `i8 has no method "frobnicate"`)
}
