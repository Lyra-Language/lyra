package typechecker_test

import "testing"

// **A struct literal is a valid `const` initializer**, which it was not until 09/09.
//
// It computes nothing — it *is* its fields — so it is constant exactly when they are, the
// same rule the array and tuple arms already applied. Refusing it made `Color { … }` less
// constant than `[245, 245, 245, 255]`, which nothing about either justified, and forced
// `bindings/raylib`'s named colours to be nullary `pure` functions.
//
// **This is not compile-time evaluation.** Nothing runs a constructor or folds a call: the
// walk is structural, and a field holding a call is still refused with that call named.

const colorDecl = "struct Color { r: u8, g: u8, b: u8, a: u8 }\n"

func TestConstStruct_LiteralFields(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, colorDecl+`
const WHITE: Color = Color { r: 245, g: 245, b: 245, a: 255 }
let main = () -> void => println("${WHITE.r}")`, false))
}

// Derived from another const, including arithmetic — the walk recurses, so anything the
// existing rules call constant is constant in a field too.
func TestConstStruct_DerivedFromAnotherConst(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, colorDecl+`
const N: u8 = 200
const HALF: Color = Color { r: N, g: N / 2, b: 0, a: 255 }
let main = () -> void => println("${HALF.g}")`, false))
}

// Nested, and anonymous — the same arm, since neither adds a way to compute something.
func TestConstStruct_NestedAndAnonymous(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, colorDecl+`
struct Pair { a: Color, n: i64 }
const P: Pair = Pair { a: Color { r: 1, g: 2, b: 3, a: 4 }, n: 7 }
const A: { x: i64, y: i64 } = { x: 3, y: 4 }
let main = () -> void => println("${P.a.b} ${A.x}")`, false))
}

// **A non-constant field is still refused, and the offender is the field** — not the
// struct. Recursing rather than accepting outright is what keeps the message pointed at
// the thing that is not constant.
func TestConstStruct_ANonConstantFieldIsRefused(t *testing.T) {
	res := parseCollectAndCheck(t, colorDecl+`
let f = pure () -> u8 => 1
const BAD: Color = Color { r: f(), g: 0, b: 0, a: 0 }
let main = () -> void => println("${BAD.r}")`, false)
	assertHasErrorContaining(t, res, "a function call is not constant")
}

func TestConstStruct_ANonConstantVariableFieldIsRefused(t *testing.T) {
	res := parseCollectAndCheck(t, colorDecl+`
var v: u8 = 3
const BAD: Color = Color { r: v, g: 0, b: 0, a: 0 }
let main = () -> void => println("${BAD.r}")`, false)
	assertHasErrorContaining(t, res, "variable `v` is not constant")
}

// **The update form is refused, and names itself rather than its base.** The base is very
// often a `const`, so reporting it produced "variable `BASE` is not constant" about a thing
// that plainly is — a message that sends the reader to check the wrong declaration.
func TestConstStruct_TheUpdateFormIsRefusedByName(t *testing.T) {
	res := parseCollectAndCheck(t, colorDecl+`
const BASE: Color = Color { r: 1, g: 2, b: 3, a: 4 }
const BAD: Color = Color { BASE | r: 9 }
let main = () -> void => println("${BAD.r}")`, false)
	assertHasErrorContaining(t, res, "a struct update is not constant")
}
