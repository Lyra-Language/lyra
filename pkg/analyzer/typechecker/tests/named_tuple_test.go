package typechecker_test

import (
	"testing"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// Named tuples (`tuple Point(i32, i32)`) are nominal (Pit-of-Success #8,
// todo.md: "positional nominal"), matching NamedStructType — distinct from an
// anonymous tuple `(i32, i32)`, which stays structural. Construction
// (`Point(1, 2)`) is validated against the declaration the same way a struct
// literal is validated against its struct decl.

// ── nominal identity: same name assignable, different name not ─────────────

func TestNamedTuple_SameName_Assignable(t *testing.T) {
	res := parseCollectAndCheck(t, `
tuple Point(i32, i32)
let p1 = Point(1, 2)
let p2: Point = p1
`, false)
	assertNoErrors(t, res)
}

// The motivating case: two named tuples with identical element shapes but
// different names must NOT be interchangeable — this is exactly what the old
// structural (element-wise, name-ignoring) TupleType equality got wrong.
func TestNamedTuple_DifferentNamesSameShape_NotAssignable(t *testing.T) {
	res := parseCollectAndCheck(t, `
tuple Point(i32, i32)
tuple Vector(i32, i32)
let v: Vector = Point(1, 2)
`, false)
	if len(res.errors) == 0 {
		t.Fatal("expected an error assigning a Point to a Vector slot (same shape, different name); got none")
	}
}

// A named tuple type is never assignable from an anonymous tuple literal, even
// of the same shape — nominal identity, not structural.
func TestNamedTuple_AnonymousLiteralNotAssignableToNamedSlot(t *testing.T) {
	res := parseCollectAndCheck(t, `
tuple Point(i32, i32)
let p: Point = (1, 2)
`, false)
	if len(res.errors) == 0 {
		t.Fatal("expected an error assigning an anonymous tuple literal to a named-tuple slot; got none")
	}
}

// ── construction is validated against the declaration ───────────────────────

func TestNamedTuple_Undeclared_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `let p = Foo(1, 2)`, false)
	assertErrorsAre(t, res, `undefined tuple type "Foo"`)
}

// Call syntax on a name declared as something other than a named tuple (here
// a struct) reaches inferNamedTupleLiteralExpr's decl.Type type assertion and
// must fail distinctly from "undefined".
func TestNamedTuple_DeclaredAsOtherKind_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
struct Foo { x: i32 }
let f = Foo(1)
`, false)
	assertErrorsAre(t, res, "Foo: not a tuple type")
}

func TestNamedTuple_ArityMismatch_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
tuple Point(i32, i32)
let p = Point(1, 2, 3)
`, false)
	assertErrorsAre(t, res, "Point: expected 2 element(s), got 3")
}

func TestNamedTuple_ElementTypeMismatch_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
tuple Point(i32, i32)
let p = Point(1, "hello")
`, false)
	assertErrorsAre(t, res, "Point: element 2: cannot assign string to i32")
}

// ── literal-width propagation into a declared element type ──────────────────

// A regression guard for the fix this validation needed: without propagating
// the declared element type onto an untyped literal, `1`/`2` promote to i64
// (the default) *before* the assignability check runs, so `1` -> i32 would
// wrongly report "cannot assign i64 to i32" even though the literal fits.
func TestNamedTuple_LiteralElement_NoSpuriousWidthError(t *testing.T) {
	res := parseCollectAndCheck(t, `
tuple Point(i32, i32)
let p = Point(1, 2)
`, false)
	assertNoErrors(t, res)

	decl := res.program.Statements[1].(*ast.VarDeclStmt)
	tup := decl.Value.(*ast.TupleLiteralExpr)
	for i, elem := range tup.Elements {
		typ, ok := res.typeTable.Get(elem)
		if !ok {
			t.Fatalf("element %d: no TypeTable entry", i)
		}
		p, ok := typ.(types.PrimitiveType)
		if !ok || p.Name != types.Int32 {
			t.Errorf("element %d: recorded type = %s; want i32", i, typ)
		}
	}
}

// ── generics: turbofish substitutes; no turbofish leaves a bare parameter
//    position unconstrained rather than guessing (a deliberate, smaller-scope
//    choice than struct literals' per-field inference) ─────────────────────

func TestNamedTuple_GenericTurbofish_Substitutes(t *testing.T) {
	res := parseCollectAndCheck(t, `
tuple Box<t>(t)
let b = Box::<i64>(42)
`, false)
	assertNoErrors(t, res)
}

func TestNamedTuple_GenericTurbofish_WrongArgCount_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
tuple Box<t>(t)
let b = Box::<i64, i32>(42)
`, false)
	assertErrorsAre(t, res, "Box: expected 1 generic argument(s), got 2")
}

func TestNamedTuple_GenericNoTurbofish_UnboundPositionAccepted(t *testing.T) {
	res := parseCollectAndCheck(t, `
tuple Box<t>(t)
let b = Box(42)
`, false)
	assertNoErrors(t, res)
}

// ── anonymous tuples stay structural (regression guard) ──────────────────────

func TestAnonymousTuple_SameShapeViaIdentifier_StillAssignable(t *testing.T) {
	res := parseCollectAndCheck(t, `
let a = (1, 2)
let b: (i64, i64) = a
`, false)
	assertNoErrors(t, res)
}

// ── an all-uppercase type name ───────────────────────────────────────────────

// `user_defined_type_name` (`[A-Z][a-zA-Z0-9]*`) and `const_identifier`
// (`[A-Z][A-Z0-9_]*`) match the *same text at the same length* for any name with no
// lowercase letter and no underscore — `S`, `AB`, `HTTP2`. Tree-sitter's lexer picks
// one, and in expression position, where a constant is also legal, it picks
// `const_identifier`. So `AB(1, 2)` fell through to the call reading and failed with
// "cannot resolve function AB" while the identical `Ab(1, 2)` worked.
//
// The grammar's `_tuple_name` now accepts either spelling and lets the following
// token decide (helpers.js `typeNameInExpr`).
func TestNamedTuple_AllUppercaseTypeName(t *testing.T) {
	res := parseCollectAndCheck(t, `
tuple AB(i64, i64)
let p = AB(1, 2)
let first = p.0
`, false)
	assertNoErrors(t, res)
}

// The control: the same declaration with one lowercase letter never had the
// collision, so this passed throughout and pins that nothing changed for it.
func TestNamedTuple_MixedCaseTypeName(t *testing.T) {
	res := parseCollectAndCheck(t, `
tuple Ab(i64, i64)
let p = Ab(1, 2)
let first = p.0
`, false)
	assertNoErrors(t, res)
}

// A constant is still a constant: the fix must not turn `MAX - 1` into an
// application, nor make a constant followed by a block a struct literal.
func TestAllUppercase_ConstantStillReadsAsAConstant(t *testing.T) {
	res := parseCollectAndCheck(t, `
const MAX: i64 = 5
let n = MAX - 1
`, false)
	assertNoErrors(t, res)
}

// The struct half of the same collision, unblocked 08/05 once the `Name • {` decision
// became a GLR conflict. `struct S` could be declared but never constructed: `S { v: 1 }`
// was a syntax error in every position.
func TestStructLiteral_AllUppercaseTypeName(t *testing.T) {
	for _, source := range []string{
		`struct S {
  v: i64,
}
let s = S { v: 1 }`,
		`struct HTTP2 {
  v: i64,
}
let s = HTTP2 { v: 1 }`,
		// Every position the original report named: call argument and return expression.
		`struct S {
  v: i64,
}
let f = (s: S) -> i64 => s.v
let n = f(S { v: 1 })`,
		`struct S {
  v: i64,
}
let mk = () -> S => S { v: 1 }`,
	} {
		res := parseCollectAndCheck(t, source, false)
		assertNoErrors(t, res)
	}
}

// A name followed by a brace that is *not* a struct body is a block, and deciding that
// needs the brace's contents — one token past where an LR parser must choose. Precedence
// used to settle it toward the struct literal, so `if Point { 1 } else { 0 }` was a syntax
// error; it is a GLR conflict now. The constant case is the one that makes this matter:
// `if MAX { … }` is ordinary code, and it is what a careless fix to the struct half breaks
// first.
func TestIfHeader_NameFollowedByPlainBlock(t *testing.T) {
	for _, source := range []string{
		`struct Point {
  x: i64,
}
let n = if Point { 1 } else { 0 }`,
		`const MAX: bool = true
let n = if MAX { 1 } else { 0 }`,
		// And the reading that must survive: a struct literal really *is* the condition's
		// head here, decided by the brace holding fields rather than statements.
		`struct Point {
  x: i64,
}
let n = if Point { x: 7 }.x > 0 { 1 } else { 0 }`,
	} {
		res := parseCollectAndCheck(t, source, false)
		assertNoErrors(t, res)
	}
}

// A call may be the first thing inside parentheses. It could not be until 08/05:
// `tuple_pattern` carried an optional leading name aliased from `$.identifier`, which
// is lowercase-leading, while a named tuple *type* is PascalCase — so the name could
// never be used by a legal program, but it did outbid the expression reading of the
// same tokens. `(f(7))` parsed as a parameter list holding the tuple pattern `f(7)`
// and then failed.
//
// `(1, f(7))` always worked, which is the tell: by the second element the pattern
// reading is already dead. Fixed in tree-sitter-lyra by making tuple_pattern
// anonymous-only; an uppercase named tuple pattern was never this rule (`Point(x, y)`
// is a data_pattern).
func TestParenthesizedCall_MayLeadATuple(t *testing.T) {
	for _, source := range []string{
		`let f = (a: i64) -> i64 => a
let p = (f(7), 1)`,
		`let f = (a: i64) -> i64 => a
let p = (f(7))`,
		`let f = (a: i64) -> i64 => a
let p = (f(7) + 1, 1)`,
		`let f = (a: i64) -> i64 => a
let p = ((f(7)), 1)`,
		// The control that always worked, kept so a regression cannot hide behind it.
		`let f = (a: i64) -> i64 => a
let p = (1, f(7))`,
	} {
		res := parseCollectAndCheck(t, source, false)
		assertNoErrors(t, res)
	}
}

// The forms that share tuple_pattern must be untouched — a lambda's tuple parameter, a
// destructuring `let`, a match arm, and the destructured *parameter* the grammar's own
// notes call this region's canary.
func TestTuplePattern_SharedFormsUnaffected(t *testing.T) {
	for _, source := range []string{
		`let g = ((a, b): (i64, i64)) -> i64 => a + b`,
		`let pair = (20, 22)
let (x, y) = pair`,
		`let p = ((1, 2), 3)
let n = match p {
  ((a, b), c) => a + b + c,
}`,
		`data Opt = None | Some(i64)
let f = (Some(x): Opt) -> i64 => x`,
	} {
		res := parseCollectAndCheck(t, source, false)
		assertNoErrors(t, res)
	}
}
