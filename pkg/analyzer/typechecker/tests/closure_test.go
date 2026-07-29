package typechecker_test

import "testing"

// Closures: a lambda is a first-class value, so a binding, a struct field, an
// array element, or a call result can hold one and be called through.

// The shape that makes a returned closure useful — and the one that reported
// "identifier \"add5\" is not callable" before, because the binding's value is a
// call rather than a literal lambda. Its *type* is what says it is callable.
func TestClosure_CallThroughBindingHoldingACall(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
let makeAdder = (n: i64) -> (i64) -> i64 => (x: i64) -> i64 => x + n
let main = () -> u8 => {
  let add5 = makeAdder(5)
  u8(add5(3))
}
`, false))
}

// A struct field holding a function is callable through the field.
func TestClosure_CallThroughStructField(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
struct Handler { run: (i64) -> i64 }
let main = () -> u8 => {
  let h = Handler { run: (x: i64) -> i64 => x + 1 }
  u8(h.run(5))
}
`, false))
}

// Any expression that evaluates to a function is callable — here an element of
// an array of them, which no dedicated callee case covers.
func TestClosure_CallThroughArrayElement(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
let main = () -> u8 => {
  let fs: [2](i64) -> i64 = [(x: i64) -> i64 => x + 1, (x: i64) -> i64 => x * 2]
  u8(fs[1](5))
}
`, false))
}

// A non-function value is still not callable, and says so.
func TestClosure_NonFunctionValueNotCallable(t *testing.T) {
	res := parseCollectAndCheck(t, `
let main = () -> u8 => {
  let xs: [2]i64 = [1, 2]
  u8(xs[0](3))
}
`, false)
	assertErrorsAre(t, res, "cannot call IndexExpr(xs[IntegerLiteralExpr(0, Base: 10)]) expression")
}

// A nested lambda sees the enclosing lambda's parameters — it is lexically
// inside it. Replacing the parameter scope outright made an enclosing parameter
// invisible, which stayed hidden only because an annotated nested lambda's body
// was never checked at all.
func TestClosure_NestedLambdaSeesEnclosingParameter(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
let makeAdder = (n: i64) -> (i64) -> i64 => (x: i64) -> i64 => x + n
`, false))
}

// An annotated lambda in value position has its body checked like any other, so
// a genuine error inside one is reported rather than passed over.
func TestClosure_BodyOfAnAnnotatedLambdaValueIsChecked(t *testing.T) {
	res := parseCollectAndCheck(t, `
let apply = (f: (i64) -> i64, x: i64) -> i64 => f(x)
let main = () -> u8 => u8(apply((y: i64) -> i64 => undefinedName, 1))
`, false)
	assertErrorsAre(t, res, `undefined identifier "undefinedName"`)
}

// (Assignment to a *captured* binding is rejected by lyra-E024, a checker pass —
// see pkg/analyzer/checker/captured_assignment_test.go.)
