package typechecker_test

import "testing"

// TestBuiltin_Rounding_Ok verifies floor/ceil/round type-check on every
// concrete float receiver and return i64.
func TestBuiltin_Rounding_Ok(t *testing.T) {
	ops := []string{"floor", "ceil", "round"}
	floatTypes := []string{"f16", "f32", "f64"}
	for _, op := range ops {
		for _, ty := range floatTypes {
			src := "let a: " + ty + " = 1.5\nlet b: i64 = a." + op + "()\n"
			res := parseCollectAndCheck(t, src, false)
			assertNoErrors(t, res)
		}
	}
}

// TestBuiltin_Rounding_NotOnInt: the rounding builtins are float-only.
func TestBuiltin_Rounding_NotOnInt(t *testing.T) {
	res := parseCollectAndCheck(t, "let a: i64 = 1\nlet b = a.floor()\n", false)
	assertErrorsAre(t, res, `i64 has no method "floor"`)
}

// TestBuiltin_Rounding_WrongArgCount is reported by the shared call-arity check.
func TestBuiltin_Rounding_WrongArgCount(t *testing.T) {
	res := parseCollectAndCheck(t, "let a: f64 = 1.5\nlet b = a.floor(1.0)\n", false)
	assertErrorsAre(t, res, "floor: expected 0 argument(s), got 1")
}

// TestBuiltin_Rounding_ReturnTypeIsI64: the result is always i64, so binding
// it to a different type is a type error.
func TestBuiltin_Rounding_ReturnTypeIsI64(t *testing.T) {
	res := parseCollectAndCheck(t, "let a: f64 = 1.5\nlet b: string = a.floor()\n", false)
	assertErrorsAre(t, res, "b: cannot assign i64 to string")
}
