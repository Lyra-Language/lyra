package typechecker_test

import "testing"

// A binding with no initializer of its own — a `for` variable, a destructured name — is
// called through its declared type. The call asked the type of the binding's (absent)
// value and reported `for g in fs { g() }` as a definition cycle.
func TestCallingAFunctionValuedLoopVariable(t *testing.T) {
	res := parseCollectAndCheck(t, `
let one = () -> i64 => 1
let run = (fs: []() -> i64) -> i64 => {
  var t = 0
  for g in fs { t += g() }
  t
}`, false)
	assertNoErrors(t, res)
	assertIdentTypes(t, res, "g", "() -> i64")
}

func TestCallingADestructuredFunctionValue(t *testing.T) {
	res := parseCollectAndCheck(t, `
let one = () -> i64 => 1
let two = (n: i64) -> i64 => n * 2
let run = () -> i64 => {
  let (f, g) = (one, two)
  g(f())
}`, false)
	assertNoErrors(t, res)
	assertIdentTypes(t, res, "f", "() -> i64")
	assertIdentTypes(t, res, "g", "(i64) -> i64")
}
