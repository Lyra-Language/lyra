package typechecker_test

import "testing"

// A field's value is judged as itself, before the field's type is pushed into it (10/19).
// The push records the target's type on an array literal or a comprehension whatever its
// elements are, and the check read the value's type back after it — so a `[]bool` into a
// `[]Item` field checked as `[]Item` into `[]Item`, passed, and the backend failed at
// "cannot store i1". The same into a local was refused. Found by Sulafat, filtering its
// saved themes with a comprehension whose result was the filter's test.

func TestTypeCheck_FieldAssignment_RefusesAnArrayLiteralOfTheWrongElement(t *testing.T) {
	res := parseCollectAndCheck(t, `
struct Item { name: string }
struct Holder { items: []Item }
let main = () -> void => {
  var h = Holder { items: [] }
  h.items = [true, false]
}
`, false)
	assertErrorContainsGeneric(t, res, "cannot assign []bool to []Item")
}

func TestTypeCheck_FieldAssignment_RefusesAComprehensionOfTheWrongElement(t *testing.T) {
	res := parseCollectAndCheck(t, `
struct Item { name: string }
struct Holder { items: []Item }
let main = () -> void => {
  var h = Holder { items: [Item { name: "a" }] }
  h.items = [it in h.items | it.name != "a"]
}
`, false)
	assertErrorContainsGeneric(t, res, "cannot assign []bool to []Item")
}

func TestTypeCheck_FieldAssignment_ThroughAnElementToo(t *testing.T) {
	res := parseCollectAndCheck(t, `
struct Item { name: string }
struct Holder { items: []Item }
let main = () -> void => {
  var hs = [Holder { items: [] }]
  hs[0].items = [true]
}
`, false)
	assertErrorContainsGeneric(t, res, "cannot assign []bool to []Item")
}

// What the push is for still passes: a literal takes the field's flavour, an empty `[]`
// its element type, a comprehension of the right element is the field's array.
func TestTypeCheck_FieldAssignment_TakesWhatTheFieldMakesOfIt(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
struct Item { name: string }
struct Holder { items: []Item, names: []string, pair: (i64, []string) }
let main = () -> void => {
  var h = Holder { items: [Item { name: "a" }], names: [], pair: (0, []) }
  h.names = ["x"]
  h.names = []
  h.items = [it in h.items | it]
  h.names = [it in h.items | it.name]
  h.pair = (1, [])
}
`, false))
}
