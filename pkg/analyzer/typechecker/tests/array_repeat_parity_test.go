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
// **The two arms consult different things, deliberately.** The literal rule looks only at the
// *value*, because provenance is about where the elements came from and a runtime length is
// still a perfectly good `[]T`. The const rule looks at the value *and* the count, because a
// const's whole value has to be known at compile time.
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

// The parity runs both ways: a repeat whose **value** is not a literal must be refused
// exactly as the comma form is, or the fix would have bought permissiveness rather than
// consistency.
func TestTypeCheck_ArrayRepeatIsNotALiteralWhenItsValueIsNot(t *testing.T) {
	// Both spellings produce the *identical* message, which is the parity this pins.
	const want = "cannot use StaticArray<i64, 3> as Nums implicitly: Nums is a distinct type " +
		"over DynamicArray<i64>, so the conversion must be written — `Nums(...)`"
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
			assertErrorsAre(t, parseCollectAndCheck(t, c.src, false), want)
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
