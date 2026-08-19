package typechecker_test

import (
	"strings"
	"testing"
)

// TestDestructuring_TupleDeclBindsNames: each name in a tuple-destructuring
// declaration must resolve to its corresponding element's type, not just
// pass shape validation.
func TestDestructuring_TupleDeclBindsNames(t *testing.T) {
	source := `
let f = () -> i64 => {
    let (a, b) = (1, 2)
    a + b
}`
	res := parseCollectAndCheck(t, source, false)
	assertNoErrors(t, res)
}

// TestDestructuring_TupleDeclVarIsMutable: `var (a, b) = ...` gives each name
// the full var-level mutability (reassignable), like any other `var`.
func TestDestructuring_TupleDeclVarIsMutable(t *testing.T) {
	source := `
let f = () -> i64 => {
    var (a, b) = (1, 2)
    a = 5
    a + b
}`
	res := parseCollectAndCheck(t, source, false)
	assertNoErrors(t, res)
}

// TestDestructuring_TupleDeclLetIsImmutable: `let (a, b) = ...` gives each
// name the same immutability as a plain `let` — reassigning one is rejected.
func TestDestructuring_TupleDeclLetIsImmutable(t *testing.T) {
	source := `
let f = () -> i64 => {
    let (a, b) = (1, 2)
    a = 5
    a + b
}`
	res := parseCollectAndCheck(t, source, false)
	assertErrorsAre(t, res, "a: 'let' binding is immutable; use 'var' to allow reassignment")
}

// TestDestructuring_TupleDeclArityMismatch_NamesUnbound: when the pattern and
// tuple arity disagree, names are not bound at all (rather than guessing a
// pairing) — referencing them afterward is still "undefined identifier".
func TestDestructuring_TupleDeclArityMismatch_NamesUnbound(t *testing.T) {
	source := `
let f = () -> i64 => {
    let (a, b, c) = (1, 2)
    a + b
}`
	res := parseCollectAndCheck(t, source, false)
	assertErrorsAre(t, res,
		"tuple pattern has 3 element(s) but tuple has 2",
		`undefined identifier "a"`,
		`undefined identifier "b"`)
}

// TestDestructuring_SiblingBlocksStayIsolated: two unrelated tuple
// destructurings that happen to reuse the same names, in sibling scopes, must
// not collide — each resolves to its own element types.
func TestDestructuring_SiblingBlocksStayIsolated(t *testing.T) {
	source := `
let f = () -> i64 => {
    let g = () -> string => {
        let (a, b) = ("x", "y")
        a
    }
    let (a, b) = (1, 2)
    a + b
}`
	res := parseCollectAndCheck(t, source, false)
	assertNoErrors(t, res)
}

// TestDestructuring_TupleParamBindsNames: a destructured tuple parameter
// binds each name to its corresponding element type, for both a
// single-expression body and a block body.
func TestDestructuring_TupleParamBindsNames(t *testing.T) {
	for _, source := range []string{
		`let f = ((a, b): (i64, i64)) -> i64 => a + b`,
		`let f = ((a, b): (i64, i64)) -> i64 => {
    a + b
}`,
	} {
		res := parseCollectAndCheck(t, source, false)
		assertNoErrors(t, res)
	}
}

// TestDestructuring_StructDeclShorthandBindsNames: `let {x, y} = p` binds each
// field name to that field's type.
func TestDestructuring_StructDeclShorthandBindsNames(t *testing.T) {
	source := `
struct Point { x: i64, y: i64 }
let f = (p: Point) -> i64 => {
    let {x, y} = p
    x + y
}`
	res := parseCollectAndCheck(t, source, false)
	assertNoErrors(t, res)
}

// TestDestructuring_StructDeclRenameBindsNames: `let {x: a, y: b} = p` binds
// the *renamed* local names, each to its original field's type.
func TestDestructuring_StructDeclRenameBindsNames(t *testing.T) {
	source := `
struct Point { x: i64, y: i64 }
let f = (p: Point) -> i64 => {
    let {x: a, y: b} = p
    a + b
}`
	res := parseCollectAndCheck(t, source, false)
	assertNoErrors(t, res)
}

// TestDestructuring_StructDeclMissingField_Errors: a pattern field that
// doesn't exist on the struct is reported, by name.
func TestDestructuring_StructDeclMissingField_Errors(t *testing.T) {
	source := `
struct Point { x: i64, y: i64 }
let f = (p: Point) -> i64 => {
    let {x, z} = p
    x
}`
	res := parseCollectAndCheck(t, source, false)
	assertErrorsAre(t, res, `Point has no field "z"`)
}

// TestDestructuring_StructDeclVarIsMutable / LetIsImmutable: the same
// let/var mutability distinction as tuple destructuring applies to struct
// destructuring.
func TestDestructuring_StructDeclVarIsMutable(t *testing.T) {
	source := `
struct Point { x: i64, y: i64 }
let f = (p: Point) -> i64 => {
    var {x, y} = p
    x = 5
    x + y
}`
	res := parseCollectAndCheck(t, source, false)
	assertNoErrors(t, res)
}

func TestDestructuring_StructDeclLetIsImmutable(t *testing.T) {
	source := `
struct Point { x: i64, y: i64 }
let f = (p: Point) -> i64 => {
    let {x, y} = p
    x = 5
    x + y
}`
	res := parseCollectAndCheck(t, source, false)
	assertErrorsAre(t, res, "x: 'let' binding is immutable; use 'var' to allow reassignment")
}

// TestDestructuring_StructParamBindsNames: a destructured struct parameter
// binds each field name directly, like the declaration form.
func TestDestructuring_StructParamBindsNames(t *testing.T) {
	source := `
struct Point { x: i64, y: i64 }
let f = ({x, y}: Point) -> i64 => x + y`
	res := parseCollectAndCheck(t, source, false)
	assertNoErrors(t, res)
}

// TestDestructuring_StructDeclNonStruct_Errors: destructuring a non-struct
// value with a struct pattern is rejected, and the named fields stay unbound.
func TestDestructuring_StructDeclNonStruct_Errors(t *testing.T) {
	source := `
let f = (n: i64) -> i64 => {
    let {x, y} = n
    x
}`
	res := parseCollectAndCheck(t, source, false)
	assertErrorsAre(t, res,
		"cannot destructure i64 with a struct pattern",
		`undefined identifier "x"`)
}

// TestDestructuring_StructDeclNestedPattern: a struct field's own pattern
// (here a nested tuple) is walked recursively against that field's type.
func TestDestructuring_StructDeclNestedPattern(t *testing.T) {
	source := `
struct Pair { t: (i64, i64) }
let f = (p: Pair) -> i64 => {
    let {t: (a, b)} = p
    a + b
}`
	res := parseCollectAndCheck(t, source, false)
	assertNoErrors(t, res)
}

// TestDestructuring_ArrayDeclBindsNames: each name in an array-destructuring
// declaration binds to the array's uniform element type, for both a static
// and a dynamic array.
func TestDestructuring_ArrayDeclBindsNames(t *testing.T) {
	for _, source := range []string{
		`let f = (arr: [3]i64) -> i64 => {
    let [a, b, c] = arr
    a + b + c
}`,
		`let f = (arr: []i64) -> i64 => {
    let [a, b] = arr
    a + b
}`,
	} {
		res := parseCollectAndCheck(t, source, false)
		assertNoErrors(t, res)
	}
}

// TestDestructuring_ArrayDeclNamedRestBindsArrayType: a named rest element
// (`...rest`) binds the whole array type, not an element type.
func TestDestructuring_ArrayDeclNamedRestBindsArrayType(t *testing.T) {
	source := `
let f = (arr: [3]i64) -> i64 => {
    let [a, ...rest] = arr
    a
}`
	res := parseCollectAndCheck(t, source, false)
	assertNoErrors(t, res)
}

// TestDestructuring_ArrayParamBindsNames: a destructured array parameter
// binds each name to the element type.
func TestDestructuring_ArrayParamBindsNames(t *testing.T) {
	source := `let f = ([a, b]: [2]i64) -> i64 => a + b`
	res := parseCollectAndCheck(t, source, false)
	assertNoErrors(t, res)
}

// TestDestructuring_ArrayDeclNonArray_Errors: destructuring a non-array value
// with an array pattern is rejected, and the named elements stay unbound.
func TestDestructuring_ArrayDeclNonArray_Errors(t *testing.T) {
	source := `
let f = (n: i64) -> i64 => {
    let [a, b] = n
    a
}`
	res := parseCollectAndCheck(t, source, false)
	assertErrorsAre(t, res,
		"cannot destructure i64 with an array pattern",
		`undefined identifier "a"`)
}

// TestDestructuring_DataDeclParenPayloadBindsName: `let Some(x) = m` binds `x`
// to the constructor's payload type, substituted for the concrete type
// argument of the generic data type (Maybe<i64> -> x: i64, not the
// unsubstituted type variable `t`).
func TestDestructuring_DataDeclParenPayloadBindsName(t *testing.T) {
	source := `
data Maybe<t> = Some t | None
let f = (m: Maybe<i64>) -> i64 => {
    let Some(x) = m
    x
}`
	res := parseCollectAndCheck(t, source, false)
	assertNoErrors(t, res)
}

// TestDestructuring_DataDeclBarePayloadBindsName: the same as above but with
// the bare (unparenthesized) single-argument payload form, `let Some x = m`.
func TestDestructuring_DataDeclBarePayloadBindsName(t *testing.T) {
	source := `
data Maybe<t> = Some t | None
let f = (m: Maybe<i64>) -> i64 => {
    let Some x = m
    x
}`
	res := parseCollectAndCheck(t, source, false)
	assertNoErrors(t, res)
}

// TestDestructuring_DataDeclVarIsMutable / LetIsImmutable: the same let/var
// mutability distinction applies to a data-pattern payload binding.
func TestDestructuring_DataDeclVarIsMutable(t *testing.T) {
	source := `
data Maybe<t> = Some t | None
let f = (m: Maybe<i64>) -> i64 => {
    var Some(x) = m
    x = 5
    x
}`
	res := parseCollectAndCheck(t, source, false)
	assertNoErrors(t, res)
}

func TestDestructuring_DataDeclLetIsImmutable(t *testing.T) {
	source := `
data Maybe<t> = Some t | None
let f = (m: Maybe<i64>) -> i64 => {
    let Some(x) = m
    x = 5
    x
}`
	res := parseCollectAndCheck(t, source, false)
	assertErrorsAre(t, res, "x: 'let' binding is immutable; use 'var' to allow reassignment")
}

// TestDestructuring_DataDeclUnknownConstructor_Errors: a constructor name not
// declared on the data type is rejected, and its payload stays unbound.
func TestDestructuring_DataDeclUnknownConstructor_Errors(t *testing.T) {
	source := `
data Maybe<t> = Some t | None
let f = (m: Maybe<i64>) -> i64 => {
    let Other(x) = m
    x
}`
	res := parseCollectAndCheck(t, source, false)
	assertErrorsAre(t, res,
		"Other is not a constructor of Maybe",
		`undefined identifier "x"`)
}

// TestDestructuring_DataDeclZeroArgConstructor_NoBinding: a zero-payload
// constructor (`None`) used with no pattern binds nothing and is not an error.
func TestDestructuring_DataDeclZeroArgConstructor_NoBinding(t *testing.T) {
	source := `
data Maybe<t> = Some t | None
let f = (m: Maybe<i64>) -> i64 => {
    let None = m
    0
}`
	res := parseCollectAndCheck(t, source, false)
	assertNoErrors(t, res)
}

// TestDestructuring_DataDeclNonData_Errors: destructuring a non-data value
// with a data pattern is rejected, and the payload name stays unbound.
func TestDestructuring_DataDeclNonData_Errors(t *testing.T) {
	source := `
let f = (n: i64) -> i64 => {
    let Some(x) = n
    x
}`
	res := parseCollectAndCheck(t, source, false)
	assertErrorsAre(t, res,
		"cannot destructure i64 with a data pattern",
		`undefined identifier "x"`)
}

// TestDestructuring_DataDeclNestedPattern: a constructor's tuple payload is
// destructured further (Maybe<(i64,i64)>'s Some carries a real tuple, unlike
// the parens around a single-argument payload, which are pattern syntax
// only — see TestDestructuring_DataDeclTuplePayloadConstructor for that case).
func TestDestructuring_DataDeclNestedPattern(t *testing.T) {
	source := `
data Maybe<t> = Some t | None
let f = (m: Maybe<(i64, i64)>) -> i64 => {
    let Some((a, b)) = m
    a + b
}`
	res := parseCollectAndCheck(t, source, false)
	assertNoErrors(t, res)
}

// TestDestructuring_DataDeclTuplePayloadConstructor: a constructor declared
// with a tuple payload (`MkPair (a, b)`, the project's recommended form for a
// positional multi-value payload) is destructured with a matching tuple
// sub-pattern.
func TestDestructuring_DataDeclTuplePayloadConstructor(t *testing.T) {
	source := `
data Pair<a, b> = MkPair (a, b)
let f = (p: Pair<i64, string>) -> i64 => {
    let MkPair((x, y)) = p
    x
}`
	res := parseCollectAndCheck(t, source, false)
	assertNoErrors(t, res)
}

// TestDestructuring_DataParamBindsName: a data pattern used directly as a
// parameter (`(Some(x): Maybe<i64>) -> ...`) binds its payload name, for both
// a single-expression and a block body. This previously mis-parsed at the
// grammar level — `Some(x)` in parameter-pattern position lost to the
// constructor-call *expression* reading (tuple_literal) because data_pattern
// had no explicit precedence to win that tie; fixed via PREC.DATA_PATTERN
// (tree-sitter-lyra/include/prec.js) plus a declared 3-way GLR conflict
// (`_tuple_name`, `_primary_expr`, `data_pattern`) in grammar.js.
func TestDestructuring_DataParamBindsName(t *testing.T) {
	for _, source := range []string{
		`data Maybe<t> = Some t | None
let f = (Some(x): Maybe<i64>) -> i64 => x`,
		`data Maybe<t> = Some t | None
let f = (Some(x): Maybe<i64>) -> i64 => {
    x
}`,
	} {
		res := parseCollectAndCheck(t, source, false)
		assertNoErrors(t, res)
	}
}

// TestDestructuring_DataParamTuplePayloadConstructor: a data parameter whose
// constructor carries a tuple payload is destructured further, same as the
// declaration form.
func TestDestructuring_DataParamTuplePayloadConstructor(t *testing.T) {
	source := `
data Pair<a, b> = MkPair (a, b)
let f = (MkPair((x, y)): Pair<i64, string>) -> i64 => x`
	res := parseCollectAndCheck(t, source, false)
	assertNoErrors(t, res)
}

// TestDestructuring_TraitImplMethodParamBindsNames: a trait-impl method clause may
// destructure, and it is the one place a destructured parameter needs no annotation
// — the trait's signature supplies the type the pattern is walked against. Before
// 07/31 checkTraitImplMethodBody bound identifier patterns only, so every name in a
// destructured clause parameter was reported undefined.
func TestDestructuring_TraitImplMethodParamBindsNames(t *testing.T) {
	for _, source := range []string{
		// The receiver itself.
		`struct Pt { x: i64, y: i64 }
trait Summable { total: (Self) -> i64 }
impl Summable for Pt { total = ({ x, y }) => x + y }`,
		// A non-receiver parameter, whose type comes from the signature too.
		`struct Pt { x: i64, y: i64 }
trait Shift { by: (Self, (i64, i64)) -> i64 }
impl Shift for Pt { by = (self, (dx, dy)) => self.x + dx + self.y + dy }`,
	} {
		res := parseCollectAndCheck(t, source, false)
		assertNoErrors(t, res)
	}
}

// Capitalization is syntax, not convention: a SCREAMING_CASE name is the grammar's
// `const_identifier` and a capitalized one a constructor, so either in binding position
// parses as a pattern to match. lyra-E057 says so and names the fix — which differs
// between the two spellings, since `const` accepts only the first.

// TestDestructuring_ConstCaseBindingName_NamesConst: `let RAMP = …` is a constant
// written with the wrong keyword, and the message says which keyword.
func TestDestructuring_ConstCaseBindingName_NamesConst(t *testing.T) {
	source := `let RAMP = [" ", "."]`
	res := parseCollectAndCheck(t, source, false)
	assertErrorsAre(t, res,
		"\"RAMP\" is spelled as a constant, so it is matched here rather than bound; "+
			"write `const RAMP = ...` to declare a constant")
}

// TestDestructuring_CapitalizedBindingName_NamesLowercase: `Foo` is not a
// `const_identifier`, so `const Foo = …` would be a *syntax* error — the fix offered is
// the initial letter instead.
func TestDestructuring_CapitalizedBindingName_NamesLowercase(t *testing.T) {
	source := `let Foo = 10`
	res := parseCollectAndCheck(t, source, false)
	assertErrorsAre(t, res,
		"\"Foo\" is spelled as a constructor, so it is matched here rather than bound; "+
			"a binding's name must start with a lowercase letter")
}

// TestDestructuring_CapitalizedBindingName_VarToo: the mistake is about the name, so the
// keyword it follows does not change it.
func TestDestructuring_CapitalizedBindingName_VarToo(t *testing.T) {
	source := `var LIMIT = 10`
	res := parseCollectAndCheck(t, source, false)
	assertErrorsAre(t, res,
		"\"LIMIT\" is spelled as a constant, so it is matched here rather than bound; "+
			"write `const LIMIT = ...` to declare a constant")
}

// TestDestructuring_CapitalizedBindingName_DoesNotCascade: the name is bound anyway, so
// one mistake draws one diagnostic. The exact-and-ordered assertion is the point — before
// this, every *use* added an `undefined identifier` of its own.
func TestDestructuring_CapitalizedBindingName_DoesNotCascade(t *testing.T) {
	source := `
let RAMP = [" ", "."]
let f = () -> string => RAMP[0]
let g = () -> string => RAMP[1]`
	res := parseCollectAndCheck(t, source, false)
	assertErrorsAre(t, res,
		"\"RAMP\" is spelled as a constant, so it is matched here rather than bound; "+
			"write `const RAMP = ...` to declare a constant")
}

// TestDestructuring_BareRealConstructor_KeepsShapeMismatch: a bare name that *is* a
// constructor is a genuine attempt to destructure, and "cannot destructure" is what is
// wrong with it — E057 must not swallow that.
func TestDestructuring_BareRealConstructor_KeepsShapeMismatch(t *testing.T) {
	source := `
data Maybe<t> = Some t | None
let f = (n: i64) -> i64 => {
    let None = n
    0
}`
	res := parseCollectAndCheck(t, source, false)
	assertErrorsAre(t, res, "cannot destructure i64 with a data pattern")
}

// Destructuring the result of a function whose return type is **inferred** and which is
// declared **later** in the file.
//
// A destructure needs the element types where the pattern is walked — each name's type
// comes from decomposing the value there and then, and nothing later revisits it — so a
// helper declared below its caller had not been inferred yet, the value's type was nil,
// and the pattern bound nothing. It was lyra-E058 from 08/17, asking for an annotation;
// since 08/18 the callee is checked **on demand** instead and the program compiles.
//
// The top-down house style (main first, helpers below) is exactly the arrangement that
// puts an un-annotated helper after its caller, which is what made this worth fixing
// rather than diagnosing.
func TestDestructuring_InferredLaterReturn_Ok(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
		let f = () -> void => {
			let (a, b) = pair(1, 2)
			println("${a} ${b}")
		}
		let pair = (x: i64, y: i64) => (x, y)
	`, false))
}

// On demand is *transitive*: the hoisted helper's own body may destructure a third
// function declared below it.
func TestDestructuring_InferredLaterReturn_Chained(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
		let f = () -> void => {
			let (a, b) = first()
			println("${a} ${b}")
		}
		let first = () => {
			let (x, y) = second()
			(x + 1, y + 1)
		}
		let second = () => (1, 2)
	`, false))
}

// **Where it still cannot answer, and must not hang.** Two un-annotated functions that
// destructure each other's results have no fixed point — computing either return type
// requires the other — so the cycle guard breaks the recursion and lyra-E058 asks for the
// annotation that resolves it. The same honest answer self-recursion already gets.
func TestDestructuring_MutuallyRecursiveInferredReturns_ReportsAndTerminates(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let ping = () => {
			let (x, y) = pong()
			(x, y)
		}
		let pong = () => {
			let (p, q) = ping()
			(p, q)
		}
	`, false)
	assertHasErrorContaining(t, res, "its return type is inferred and could not be worked out")
	assertHasErrorContaining(t, res, "give it a return type annotation")
}

// **A hoisted body is checked as if the pass had reached it in order.** withParamScope
// copies an enclosing lambda's parameters into a nested one's — a nested lambda really is
// lexically inside its enclosing one — so a hoisted *top-level* function would otherwise
// resolve a name belonging to the caller's parameters. A false accept, which is the
// direction that does not announce itself: checked in declaration order this is undefined.
func TestDestructuring_HoistedBodyDoesNotSeeTheCallersParameters(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let outer = (secret: i64) -> i64 => {
			let (a, b) = helper()
			a + b + secret
		}
		let helper = () => (secret, 2)
	`, false)
	assertHasErrorContaining(t, res, `undefined identifier "secret"`)
}

// **A body checked early must not be checked twice.** The main pass reaches the same
// declaration later, and a second walk reports everything in it a second time — so the
// error inside the hoisted helper must appear exactly once.
func TestDestructuring_HoistedBodyReportsItsErrorsOnce(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let f = () -> void => {
			let (a, b) = helper()
			println("${a} ${b}")
		}
		let helper = () => {
			let bad = undefined_name + 1
			(bad, 2)
		}
	`, false)
	n := 0
	for _, e := range res.errors {
		if strings.Contains(e.Message, "undefined_name") {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("want the hoisted body's error reported once, got %d: %v", n, res.errors)
	}
}

// An annotation on the later helper works too, and always did — the on-demand path is
// only reached when there is none.
func TestDestructuring_AnnotatedLaterReturn_Ok(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
		let f = () -> void => {
			let (a, b) = pair(1, 2)
			println("${a} ${b}")
		}
		let pair = (x: i64, y: i64) -> (i64, i64) => (x, y)
	`, false))
}

// Declared *before* its caller, inference has already run, so an annotation is not needed
// — which is what makes this an ordering gap rather than a rule about destructuring.
func TestDestructuring_InferredEarlierReturn_Ok(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
		let pair = (x: i64, y: i64) => (x, y)
		let f = () -> void => {
			let (a, b) = pair(1, 2)
			println("${a} ${b}")
		}
	`, false))
}

// Binding the whole tuple defers the element types, so it is unaffected. Guards against
// the diagnostic being widened into "you must annotate any later function".
func TestDestructuring_WholeTupleFromInferredLaterReturn_Ok(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
		let f = () -> void => {
			let p = pair(1, 2)
			println("${p.0}")
		}
		let pair = (x: i64, y: i64) => (x, y)
	`, false))
}

// A nil type from any *other* cause keeps its own diagnostic: an undefined callee is
// reported as undefined, not as a missing annotation. The narrowness is the point — a
// confident message about the wrong thing is worse than the silence this replaced.
func TestDestructuring_UndefinedCallee_KeepsItsOwnError(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let f = () -> void => {
			let (a, b) = nonexistent(1)
			println("${a} ${b}")
		}
	`, false)
	assertHasErrorContaining(t, res, `undefined function "nonexistent"`)
	for _, e := range res.errors {
		if strings.Contains(e.Message, "return type is inferred") {
			t.Errorf("E058 fired for an undefined callee: %q", e.Message)
		}
	}
}
