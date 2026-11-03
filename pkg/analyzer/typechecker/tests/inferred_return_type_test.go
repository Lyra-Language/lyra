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

// These tests check without the prelude.
const genericMaybe = "data Maybe<t> = Some(t) | None\n"

// A trailing `if let … else …` is the body's value, as a trailing `if` or `match` is. Until
// 11/02 it was void and its branches' value dropped in silence, so `{ 1 } else { "s" }`
// compiled and `let x: i64 = f()` reported "cannot assign void".
func TestInferredReturnType_TrailingIfLetIsTheValue(t *testing.T) {
	cases := []struct{ name, f string }{
		{"a block", `let f = (m: Maybe<i64>) => { if let Some(v) = m { v + 1 } else { 0 } }`},
		{"an else-if", `let f = (m: Maybe<i64>) => { if let Some(v) = m { v } else if m == None { 1 } else { 0 } }`},
		{"an else-if-let", `let f = (m: Maybe<i64>) => { if let Some(v) = m { v } else if let Some(w) = m { w } else { 0 } }`},
		{"a diverging else", `let f = (m: Maybe<i64>) => { if let Some(v) = m { v } else { panic("no") } }`},
		{"after other statements", `let f = (m: Maybe<i64>) => {
  let k = 2
  if let Some(v) = m { v * k } else { k }
}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := parseCollectAndCheck(t, genericMaybe+tc.f+"\nlet use = () -> i64 => f(None)", false)
			assertNoErrors(t, res)
		})
	}
}

// Branches that disagree are refused, as an `if`'s are, in the words of what was written.
func TestInferredReturnType_TrailingIfLetBranchesMustAgree(t *testing.T) {
	cases := []struct{ name, f, want string }{
		{"two values", `let f = (m: Maybe<i64>) => { if let Some(v) = m { v } else { "s" } }`,
			"`if let` branches have incompatible types: i64 vs string"},
		{"a value beside void", `let add = (x: i64) -> void => { x; }
let f = (m: Maybe<i64>) => { if let Some(v) = m { v } else { add(1) } }`,
			"`if let` branches have incompatible types: i64 vs void"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertHasErrorContaining(t, parseCollectAndCheck(t, genericMaybe+tc.f, false), tc.want)
		})
	}
}

// With a `void` written, by the function or by its context, there is no value to agree on:
// each branch is a statement, whatever it ends in.
func TestInferredReturnType_DeclaredVoidKeepsTrailingIfLetAStatement(t *testing.T) {
	const common = `let add = (x: i64) -> void => { x; }
let each = (xs: []i64, f: (i64) -> void) -> void => { for x in xs { f(x) } }
let opt = (x: i64) -> Maybe<i64> => Some(x)
`
	cases := []struct{ name, src string }{
		{"written", `let f = (m: Maybe<i64>) -> void => { if let Some(v) = m { v } else { "s" } }`},
		{"from a callback's parameter", `let use = () -> void => {
  each([1], (x: i64) => { if let Some(v) = opt(x) { v } else { add(1) } })
}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertNoErrors(t, parseCollectAndCheck(t, genericMaybe+common+tc.src, false))
		})
	}
}

// What has no value stays void: no `else`, a pattern a match arm cannot hold, branches of
// nothing or of statements.
func TestInferredReturnType_TrailingIfLetWithNoValueIsVoid(t *testing.T) {
	const common = `var n: i64 = 0
let add = (x: i64) -> void => { n += x }
`
	cases := []struct{ name, f string }{
		{"no else", `let f = (m: Maybe<i64>) => { if let Some(v) = m { v } }`},
		{"an annotated pattern", `let f = (m: Maybe<i64>) => { if let v: Maybe<i64> = m { v } else { m } }`},
		{"two void calls", `let f = (m: Maybe<i64>) => { if let Some(v) = m { add(v) } else { add(0) } }`},
		{"statements", `let f = (m: Maybe<i64>) => { if let Some(v) = m { n = v; } else { let k = 1 } }`},
		{"recursion through a void branch", `let f = (m: Maybe<i64>) => { if let Some(v) = m { f(None) } else { add(1) } }`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := parseCollectAndCheck(t, genericMaybe+common+tc.f+"\nlet use = () -> void => f(Some(1))", false)
			assertNoErrors(t, res)
			assertHasNoValue(t, common+tc.f+"\nlet use = () -> i64 => f(Some(1))")
		})
	}
}

// Every branch empty is void, for all three spellings. It was "cannot infer the return type
// — annotate it", naming a recursion that was not there.
func TestInferredReturnType_EmptyBranchesAreVoid(t *testing.T) {
	cases := []struct{ name, f string }{
		{"if", `let f = (c: Maybe<i64>) => { if c == None { } else { } }`},
		{"a chain", `let f = (c: Maybe<i64>) => { if c == None { } else if c == Some(1) { } else { } }`},
		{"match", `let f = (c: Maybe<i64>) => { match c { Some(_) => { }, None => { } } }`},
		{"if let", `let f = (c: Maybe<i64>) => { if let Some(_) = c { } else { } }`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := parseCollectAndCheck(t, genericMaybe+tc.f+"\nlet use = () -> void => f(None)", false)
			assertNoErrors(t, res)
			assertHasNoValue(t, tc.f+"\nlet use = () -> i64 => f(None)")
		})
	}
}

// assertHasNoValue checks that src, whose `use` wants an i64 of a function inferred void, is
// refused for that, so the case above it did not pass by inferring something a void `use`
// merely discarded.
func assertHasNoValue(t *testing.T, src string) {
	t.Helper()
	assertHasErrorContaining(t, parseCollectAndCheck(t, genericMaybe+src, false), "void")
}
