package typechecker_test

import "testing"

// The float-math builtins are `floatUnaryMathOps` and `floatBinaryMathOps` in
// typechecker/builtins.go. Unlike the rounding family beside them (builtin_rounding_test.go)
// they answer the **receiver's own width** rather than a fixed i64 — a log or a sine is a
// float operation whose answer is a float, and narrowing here would throw away precision
// the caller may still want.

// Every unary float-math builtin type-checks on every concrete float receiver and answers
// that same width. The loop is the point: a name registered in one of the two backend
// tables and not in the front end's (or the reverse) shows up here as a missing method.
func TestBuiltin_FloatMath_AnswersTheReceiversWidth(t *testing.T) {
	ops := []string{"log", "log2", "log10", "sqrt", "exp", "exp2", "sin", "cos", "tan", "asin", "acos", "atan"}
	for _, op := range ops {
		for _, ty := range []string{"f16", "f32", "f64"} {
			src := "let a: " + ty + " = 0.5\nlet b: " + ty + " = a." + op + "()\n"
			res := parseCollectAndCheck(t, src, false)
			assertNoErrors(t, res)
		}
	}
}

// The binary pair takes one argument of the receiver's width and answers the same.
func TestBuiltin_FloatMath_BinaryTakesTheReceiversWidth(t *testing.T) {
	for _, op := range []string{"pow", "atan2"} {
		for _, ty := range []string{"f16", "f32", "f64"} {
			src := "let a: " + ty + " = 0.5\nlet c: " + ty + " = 2.0\nlet b: " + ty + " = a." + op + "(c)\n"
			res := parseCollectAndCheck(t, src, false)
			assertNoErrors(t, res)
		}
	}
}

// Float-only, like the rounding builtins: an integer receiver has no such method at all,
// rather than one that converts.
func TestBuiltin_FloatMath_NotOnInt(t *testing.T) {
	res := parseCollectAndCheck(t, "let a: i64 = 1\nlet b = a.sin()\n", false)
	assertErrorsAre(t, res, `i64 has no method "sin"`)
}

// The arity is the front end's to enforce. Without it the backend would be the first pass
// to notice, which is rule 5 backwards — a well-typed program is what it is allowed to
// refuse to lower, not an arity it could have checked.
func TestBuiltin_FloatMath_WrongArgCount(t *testing.T) {
	res := parseCollectAndCheck(t, "let a: f64 = 1.5\nlet b = a.sin(1.0)\n", false)
	assertErrorsAre(t, res, "sin: expected 0 argument(s), got 1")

	res = parseCollectAndCheck(t, "let a: f64 = 1.5\nlet b = a.pow()\n", false)
	assertErrorsAre(t, res, "pow: expected 1 argument(s), got 0")
}

// The result is the receiver's width and not i64 — the line that separates this family
// from the rounding one, stated as a type error so a drift in either direction is caught.
func TestBuiltin_FloatMath_ResultIsNotAnInteger(t *testing.T) {
	res := parseCollectAndCheck(t, "let a: f64 = 1.5\nlet b: i64 = a.cos()\n", false)
	assertErrorsAre(t, res, "b: cannot assign f64 to i64")
}

// A narrower receiver keeps its width through the call, so the result does not flow into a
// wider binding without a conversion.
func TestBuiltin_FloatMath_DoesNotWidenTheReceiver(t *testing.T) {
	res := parseCollectAndCheck(t, "let a: f32 = 1.5\nlet b: f64 = a.exp()\n", false)
	assertErrorsAre(t, res, "b: cannot assign f32 to f64")
}
