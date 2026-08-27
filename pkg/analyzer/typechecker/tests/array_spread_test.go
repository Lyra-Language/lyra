package typechecker_test

import "testing"

// A spread contributes its operand's **element** type, and makes the literal a `[]T`.
func TestTypeCheck_ArraySpread_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
let main = () -> void => {
  let xs: []i64 = [1, 2]
  let fixed: [2]i64 = [3, 4]
  let a: []i64 = [...xs, 5]
  let b: []i64 = [...xs, ...fixed]
  let c: []i64 = [0, ...xs]
  println("${a.len()} ${b.len()} ${c.len()}")
}`, false)
	assertNoErrors(t, res)
}

// **A spread makes the result dynamic even when every operand is fixed.** The lengths
// would add up here, and the rule still refuses: a `[N]T` carries its size in its type, so
// deriving the arity from the operands' *declarations* would make this literal change type
// when `fixed`'s annotation changes from `[2]i64` to `[]i64` while reading identically.
func TestTypeCheck_ArraySpread_IsAlwaysDynamic(t *testing.T) {
	res := parseCollectAndCheck(t, `
let main = () -> void => {
  let fixed: [2]i64 = [1, 2]
  let c: [3]i64 = [...fixed, 3]
  println(c[0])
}`, false)
	assertErrorsAre(t, res, "c: cannot assign DynamicArray<i64> to StaticArray<i64, 3>")
}

// A spread has no type of its own — it stands for zero or more elements of a surrounding
// list, and only an array literal has one. The grammar admits it wherever an *expression*
// is admitted, so this is the typechecker's to refuse.
func TestTypeCheck_Spread_OutsideAnArrayLiteral(t *testing.T) {
	res := parseCollectAndCheck(t, `
let main = () -> void => {
  let xs: []i64 = [1]
  let y = ...xs
  println(y)
}`, false)
	assertErrorsAre(t, res, "a spread `...` is only valid inside an array literal")
}

// Argument spread is refused by the same rule, once the arity does not mask it. It is a
// real feature elsewhere and deliberately not one here: it would make a call's arity a
// run-time property.
func TestTypeCheck_Spread_InACallArgument(t *testing.T) {
	res := parseCollectAndCheck(t, `
let f = (a: i64, b: i64) -> i64 => a + b
let main = () -> void => {
  let xs: []i64 = [1, 2]
  println(f(...xs, 3))
}`, false)
	assertErrorsAre(t, res, "a spread `...` is only valid inside an array literal")
}

// The operand must be an array. A string is refused **by name**, because it is the one
// other thing a reader might expect to splice and `to_runes()` is the spelling that does
// it — the refuse-and-name-the-fix pattern.
func TestTypeCheck_Spread_NonArrayOperand(t *testing.T) {
	res := parseCollectAndCheck(t, `
let main = () -> void => {
  let n = 5
  let c: []i64 = [...n]
  println(c[0])
}`, false)
	assertErrorsAre(t, res, "a spread `...` needs an array, got i64")
}

func TestTypeCheck_Spread_StringOperandNamesToRunes(t *testing.T) {
	res := parseCollectAndCheck(t, `
let main = () -> void => {
  let s = "ab"
  let c: []rune = [...s]
  println(c[0])
}`, false)
	assertErrorsAre(t, res,
		"a spread `...` needs an array, got string — spread an array; `to_runes()` turns a string into one")
}
