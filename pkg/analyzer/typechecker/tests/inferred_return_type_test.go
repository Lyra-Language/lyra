package typechecker_test

import "testing"

// A function written without `-> T` gets its return type filled in from its body
// (inferLambdaReturnType), onto the AST node, the same elaboration contextual_lambda.go
// does for a lambda literal's missing annotations.
//
// Before 07/31/26 the type was simply never filled in: the body was checked, the program
// type-checked clean, and then the *build* failed with "needs a return type annotation" —
// the same front-end-accepts-what-the-backend-refuses split default params and
// multi-clause functions had.

func TestInferredReturnType_SingleExpressionBody(t *testing.T) {
	res := parseCollectAndCheck(t, `let sum = ((a, b): (i64, i64)) => a + b
let use = () -> i64 => sum((3, 4))`, false)
	assertNoErrors(t, res)
}

func TestInferredReturnType_BlockBody(t *testing.T) {
	res := parseCollectAndCheck(t, `let double = (n: i64) => {
  let d = n * 2
  d
}
let use = () -> i64 => double(4)`, false)
	assertNoErrors(t, res)
}

// A block that ends in something other than an expression has no value, which is what
// `void` means.
func TestInferredReturnType_BlockWithNoValueIsVoid(t *testing.T) {
	res := parseCollectAndCheck(t, `let f = (n: i64) => {
  let d = n * 2
}`, false)
	assertNoErrors(t, res)
}

// Recursion terminates when a non-recursive branch fixes the type — the `if`'s type comes
// from its first arm, so `fact` never needs its own signature to be inferred.
func TestInferredReturnType_RecursionThroughANonRecursiveBranch(t *testing.T) {
	res := parseCollectAndCheck(t, `let fact = (n: i64) => if n == 0 { 1 } else { n * fact(n - 1) }
let use = () -> i64 => fact(5)`, false)
	assertNoErrors(t, res)
}

// And when it does not, the diagnostic says so instead of the build failing later. This
// is the honest edge of the feature, so it is pinned rather than left to be discovered.
func TestInferredReturnType_UninferableRecursionIsReported(t *testing.T) {
	res := parseCollectAndCheck(t, `let f = (n: i64) => if n > 0 { f(n - 1) } else { 0 }`, false)
	assertHasErrorContaining(t, res, "cannot infer the return type")
}

// An explicit `return` means several candidate types, and joining them is a design
// question of its own (what if they disagree, how does a diverging arm count). Refused
// with a diagnostic naming the fix — still better than the backend error it replaces.
func TestInferredReturnType_ExplicitReturnNeedsAnAnnotation(t *testing.T) {
	res := parseCollectAndCheck(t, `let f = (n: i64) => {
  if n > 0 { return 1 }
  0
}`, false)
	assertHasErrorContaining(t, res, "explicit `return` needs a return type annotation")
}

// A `return` inside a *nested* function belongs to that function, so it must not stop the
// enclosing one from being inferred.
func TestInferredReturnType_NestedReturnDoesNotBlockTheOuterFunction(t *testing.T) {
	res := parseCollectAndCheck(t, `let outer = (n: i64) => {
  let inner = (m: i64) -> i64 => {
    return m * 2
  }
  inner(n)
}
let use = () -> i64 => outer(4)`, false)
	assertNoErrors(t, res)
}

// The inferred type is the concrete one a literal lowers as, not the provisional
// "integer literal" — a signature is compared at every call site, and an inference
// artifact leaking into one would make those comparisons read strangely.
func TestInferredReturnType_UntypedLiteralBecomesItsDefault(t *testing.T) {
	res := parseCollectAndCheck(t, `let one = () => 1
let use = (n: i64) -> i64 => n + one()`, false)
	assertNoErrors(t, res)
}
