package typechecker_test

import "testing"

// `[v; n]` must be treated as the literal it is, wherever `[v, v, v]` is.
//
// Two places consulted `*ast.ArrayLiteralExpr` and had no arm for `*ast.ArrayRepeatExpr`, so
// the two spellings of one thing diverged:
//
//   - `isSyntacticLiteral` (assignable.go) decides whether a value has "provenance to lose"
//     for the implicit-newtype rule, so `let n: Nums = [7; 3]` over `newtype Nums = []i64`
//     was lyra-E046 demanding `Nums(...)` while `[7, 7, 7]` went through.
//   - `firstNonConstant` (typechecker_const.go) decides what a `const` may hold, so
//     `const XS = [7; 3]` was "not a compile-time constant" while `const XS = [1, 2, 3]` was
//     fine.
//
// Hazard 8's "when adding an expression kind, grep for the kind it is a variant of":
// ArrayRepeatExpr had already been found missing in five places ArrayLiteralExpr appears.
// These were the sixth and seventh, and the second was found by sweeping for the first.
//
// **The two rules consult different things, deliberately.** The provenance rule (since
// 08/28) looks only at the *form* — an aggregate construction is provenance-free whatever
// its elements or count, see isSyntacticLiteral. The const rule looks at the elements *and*
// the count, because a const's whole value has to be known at compile time.
func TestTypeCheck_ArrayRepeatIsALiteralWhereArrayLiteralIs(t *testing.T) {
	for _, c := range []struct{ name, src string }{
		{"newtype over a dynamic array", `
newtype Nums = []i64
let n: Nums = [7; 3]`},
		{"newtype over a fixed array", `
newtype Trio = [3]i64
let t: Trio = [7; 3]`},
		{"newtype over an array of strings", `
newtype Names = []string
let n: Names = ["ab"; 2]`},
		{"a const of repeated elements", `
const XS = [7; 3]
let first = XS[0]`},
		// The comma forms, as the parity baseline.
		{"comma form (baseline)", `
newtype Nums = []i64
let n: Nums = [7, 7, 7]`},
		{"const comma form (baseline)", `
const XS = [1, 2, 3]
let first = XS[0]`},
	} {
		t.Run(c.name, func(t *testing.T) {
			assertNoErrors(t, parseCollectAndCheck(t, c.src, false))
		})
	}
}

// The parity still runs both ways — both spellings of an array of computed values are
// now *admitted* (08/28): an aggregate construction is provenance-free by form, since
// the container the newtype names is built in place, aimed at the annotation, and
// `Nums([x; 3])` would assert nothing the annotation does not already say. The earlier
// form of this test pinned the refusal of both; what it guarded — that the two
// spellings agree — is what this still guards.
func TestTypeCheck_ArrayOfComputedValuesIsAConstructionInBothSpellings(t *testing.T) {
	for _, c := range []struct{ name, src string }{
		{"repeat of a variable", `
newtype Nums = []i64
let x = 7
let n: Nums = [x; 3]`},
		{"comma form of a variable (baseline)", `
newtype Nums = []i64
let x = 7
let n: Nums = [x, x, x]`},
	} {
		t.Run(c.name, func(t *testing.T) {
			assertNoErrors(t, parseCollectAndCheck(t, c.src, false))
		})
	}
}

// A `const` needs its **count** constant too — the one place the two rules differ. The
// message names the *variable* rather than the expression, because recursing into the count
// is what lets firstNonConstant find the offender, which is the behaviour its own doc calls
// for ("the offender is now the variable, which is the thing that is not constant").
func TestTypeCheck_ConstArrayRepeatNeedsAConstantCount(t *testing.T) {
	assertErrorsAre(t, parseCollectAndCheck(t, `
let k = 3
const XS = [7; k]
`, false), "`const` initializer must be a compile-time constant: variable `k` is not constant")
}

// The case that motivated the form-based rule: refusal used to flip on element *form* —
// `[1, 2, 3]` admitted while `["a" ++ "1"]` demanded the constructor — which read as
// arbitrary at the use site. A construction's elements may be computed however they like;
// the container is still built in place.
func TestTypeCheck_ComputedElementsStayAConstruction(t *testing.T) {
	for _, c := range []struct{ name, src string }{
		{"concat elements", `
newtype Bag = []string
let b: Bag = ["a" ++ "1", "b" ++ "2"]`},
		{"mixed literal and variable", `
newtype Nums = []i64
let x = 7
let n: Nums = [1, x, 3]`},
	} {
		t.Run(c.name, func(t *testing.T) {
			assertNoErrors(t, parseCollectAndCheck(t, c.src, false))
		})
	}
}

// Element-level newtype rules are untouched by the form exemption: the array flows into
// `Row` freely, and the typed element inside it is still refused against the element
// newtype — the check lands where the discard actually is.
func TestTypeCheck_FormExemptionDoesNotBypassElementNewtypes(t *testing.T) {
	res := parseCollectAndCheck(t, `
newtype Cents = i64
newtype Row = []Cents
let x = 7
let r: Row = [x]
`, false)
	assertErrorsAre(t, res,
		"cannot use i64 as Cents implicitly: Cents is a distinct type over i64, so the conversion must be written — `Cents(...)`")
}

// A typed array *binding* keeps its provenance — the form rule is about a construction
// written in place, and an identifier is not one, whatever it holds.
func TestTypeCheck_ATypedArrayBindingStillNeedsTheConstructor(t *testing.T) {
	res := parseCollectAndCheck(t, `
newtype Nums = []i64
let xs: []i64 = [1, 2, 3]
let n: Nums = xs
`, false)
	assertErrorsAre(t, res,
		"cannot use DynamicArray<i64> as Nums implicitly: Nums is a distinct type over DynamicArray<i64>, so the conversion must be written — `Nums(...)`")
}

// A lambda literal is deliberately NOT form-exempt, though the container argument would
// seem to apply to a function-type base. The reason is a pre-existing gap this change
// must not paper over: a lambda-valued binding is a *function declaration* to the
// typechecker, so `let h: Handler = (n: i64) -> i64 => n + 1` never reaches the
// conversion check at all — it is silently accepted — and `h` then infers as the
// lambda's own `(i64) -> i64`, not as Handler, so nothing downstream treats it as one
// (`base(h)` reports "operand must be a newtype"). Until the binding-type question is
// settled (todo.md, Newtypes), the constructor is the spelling that works end to end,
// which this pins.
func TestTypeCheck_LambdaIntoFunctionTypeBase_ConstructorWorks(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
newtype Handler = (i64) -> i64
let h: Handler = Handler((n: i64) -> i64 => n + 1)
let out: i64 = base(h)(41)
`, false))
}
