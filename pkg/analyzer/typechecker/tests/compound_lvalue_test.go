package typechecker_test

import "testing"

// The compound form asks the same writability questions `=` does, because it names the same
// place: `xs[i].n += 1` and `xs[i].n = xs[i].n + 1` must agree about which bindings permit
// interior mutation and about `readonly` fields, or the rule depends on spelling. Both go
// through checkLValueWritable.

func TestTypeCheck_CompoundAssignment_ToAnLValueIsAccepted(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
struct Pt { x: i64, y: i64 }
let main = () -> void => {
  var p = Pt { x: 1, y: 2 }
  p.x += 1
}
`, false))
}

func TestTypeCheck_CompoundAssignment_RefusesAnImmutableBinding(t *testing.T) {
	res := parseCollectAndCheck(t, `
struct Pt { x: i64, y: i64 }
let main = () -> void => {
  let p = Pt { x: 1, y: 2 }
  p.x += 1
}
`, false)
	assertErrorContainsGeneric(t, res, "deeply immutable")
}

func TestTypeCheck_CompoundAssignment_RefusesAReadonlyField(t *testing.T) {
	res := parseCollectAndCheck(t, `
struct Pt { readonly x: i64, y: i64 }
let main = () -> void => {
  var p = Pt { x: 1, y: 2 }
  p.x += 1
}
`, false)
	assertErrorContainsGeneric(t, res, "readonly field")
}

// The non-place refusal is the collector's, so it is tested there
// (collector/tests/compound_assignment_test.go).

// The right operand is narrowed to the target's type and must fit it, exactly as the
// binding form's is — so an out-of-range literal is refused rather than truncated.
func TestTypeCheck_CompoundAssignment_ChecksTheOperandAgainstTheTarget(t *testing.T) {
	res := parseCollectAndCheck(t, `
struct Reg { bits: u8 }
let main = () -> void => {
  var r = Reg { bits: 1 }
  r.bits += 300
}
`, false)
	assertErrorContainsGeneric(t, res, "300")
}
