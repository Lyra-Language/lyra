package typechecker_test

import "testing"

// A struct pattern may name its type (`Pt { x, y }`, symmetric with construction
// `Pt { x: 1, y: 2 }`) in addition to the brace-only form (`{ x, y }`). The
// collector reclassifies the type-named form (parsed as a DataPattern) into a
// named StructPattern; the typechecker then verifies the name matches the
// scrutinee's/value's type.

func TestStructMatch_TypeNamedPattern_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
struct Pt {
  x: i64,
  y: i64,
}
let p = Pt { x: 1, y: 2 }
match p {
  Pt { x, y } => x + y,
}
`, false)
	assertNoErrors(t, res)
}

func TestStructMatch_BareForm_StillOk(t *testing.T) {
	res := parseCollectAndCheck(t, `
struct Pt {
  x: i64,
  y: i64,
}
let p = Pt { x: 1, y: 2 }
match p {
  { x, y } => x + y,
}
`, false)
	assertNoErrors(t, res)
}

// A type-named struct pattern whose name isn't the scrutinee's struct type is a
// compile error — the safety check the named form buys (the brace-only form
// couldn't express, and so couldn't catch, this mistake).
func TestStructMatch_WrongTypeName_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
struct Pt {
  x: i64,
}
struct Vec {
  x: i64,
}
let p = Pt { x: 1 }
match p {
  Vec { x } => x,
  _ => 0,
}
`, false)
	assertErrorsAre(t, res, "struct pattern names Vec but the scrutinee is Pt")
}

// The type-named form works in a destructuring `let` too, with the same name
// check against the destructured value's type.
func TestStructDestructure_TypeNamed_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
struct Pt {
  x: i64,
  y: i64,
}
let f = (p: Pt) -> i64 => {
  let Pt { x, y } = p
  x + y
}
`, false)
	assertNoErrors(t, res)
}

func TestStructDestructure_WrongTypeName_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
struct Pt {
  x: i64,
}
struct Vec {
  x: i64,
}
let f = (p: Pt) -> i64 => {
  let Vec { x } = p
  x
}
`, false)
	assertErrorsAre(t, res, "struct pattern names Vec but the value is Pt")
}
