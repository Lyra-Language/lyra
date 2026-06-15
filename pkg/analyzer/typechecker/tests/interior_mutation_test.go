package typechecker_test

import "testing"

// The three-level binding model:
//   let p      — frozen name, deeply immutable interior
//   let mut p  — frozen name, mutable interior (JS `const`)
//   var p      — mutable name + mutable interior
//
// These tests cover interior mutation through a member/index lvalue
// (`p.x = v`, `arr[i] = v`, `grid[i].y = v`).

const pointStruct = `
	struct Point {
		x: i64,
		y: i64,
	}
`

// --- member assignment ---

func TestTypeCheck_MemberAssign_Var_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, pointStruct+`
		var p = Point { x: 1, y: 2 }
		p.x = 5
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_MemberAssign_LetMut_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, pointStruct+`
		let mut p = Point { x: 1, y: 2 }
		p.x = 5
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_MemberAssign_Let_DeeplyImmutable_Error(t *testing.T) {
	res := parseCollectAndCheck(t, pointStruct+`
		let p = Point { x: 1, y: 2 }
		p.x = 5
	`, false)
	assertErrorsAre(t, res,
		"p: `let` binding is deeply immutable; its interior cannot be mutated (use `let mut` to allow interior mutation, or `var` to also allow reassignment)")
}

func TestTypeCheck_MemberAssign_TypeMismatch(t *testing.T) {
	res := parseCollectAndCheck(t, pointStruct+`
		var p = Point { x: 1, y: 2 }
		p.x = "nope"
	`, false)
	assertErrorsAre(t, res, "cannot assign string to i64")
}

// --- deep paths: immutability of the root reaches through every hop ---

func TestTypeCheck_NestedPathAssign_Let_Error(t *testing.T) {
	// Mutation through a two-hop path (`ps[0].x`) must still be rejected for a
	// `let`: the root-binding walk reaches `ps` through both the index and the
	// member hop.
	res := parseCollectAndCheck(t, pointStruct+`
		let ps = [Point { x: 0, y: 0 }]
		ps[0].x = 9
	`, false)
	assertErrorsAre(t, res,
		"ps: `let` binding is deeply immutable; its interior cannot be mutated (use `let mut` to allow interior mutation, or `var` to also allow reassignment)")
}

func TestTypeCheck_NestedPathAssign_LetMut_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, pointStruct+`
		let mut ps = [Point { x: 0, y: 0 }]
		ps[0].x = 9
	`, false)
	assertNoErrors(t, res)
}

// --- nested structs: a field whose type is itself a named struct ---

const lineStruct = `
	struct Point {
		x: i64,
		y: i64,
	}
	struct Line {
		start: Point,
		end: Point,
	}
`

func TestTypeCheck_NestedStructMemberAssign_LetMut_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, lineStruct+`
		let mut l = Line { start: Point { x: 0, y: 0 }, end: Point { x: 1, y: 1 } }
		l.start.x = 9
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_NestedStructMemberAssign_Let_Error(t *testing.T) {
	res := parseCollectAndCheck(t, lineStruct+`
		let l = Line { start: Point { x: 0, y: 0 }, end: Point { x: 1, y: 1 } }
		l.start.x = 9
	`, false)
	assertErrorsAre(t, res,
		"l: `let` binding is deeply immutable; its interior cannot be mutated (use `let mut` to allow interior mutation, or `var` to also allow reassignment)")
}

// A struct field whose declared type is another named struct must resolve so a
// nested struct literal type-checks (previously mis-reported "cannot assign
// Point to Point"), while a genuine mismatch is still caught.
func TestTypeCheck_NestedStructLiteral_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, lineStruct+`
		let l = Line { start: Point { x: 0, y: 0 }, end: Point { x: 1, y: 1 } }
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_NestedStructLiteral_FieldMismatch_Error(t *testing.T) {
	res := parseCollectAndCheck(t, lineStruct+`
		let l = Line { start: 5, end: Point { x: 1, y: 1 } }
	`, false)
	assertErrorsAre(t, res, "Line.start: cannot assign integer literal to Point")
}

// --- readonly fields: immutable even on a mutable instance ---

const entityStruct = `
	struct Entity {
		readonly id: u64,
		pos: i64,
	}
`

func TestTypeCheck_FrozenField_Construction_Ok(t *testing.T) {
	// A frozen field is set once, at construction.
	res := parseCollectAndCheck(t, entityStruct+`
		var e = Entity { id: 1, pos: 0 }
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_FrozenField_MutableField_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, entityStruct+`
		var e = Entity { id: 1, pos: 0 }
		e.pos = 5
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_FrozenField_Var_Error(t *testing.T) {
	// The field is frozen even though the binding is a `var`.
	res := parseCollectAndCheck(t, entityStruct+`
		var e = Entity { id: 1, pos: 0 }
		e.id = 2
	`, false)
	assertErrorsAre(t, res,
		"cannot mutate readonly field \"id\": it is immutable after construction")
}

func TestTypeCheck_FrozenField_LetMut_Error(t *testing.T) {
	// `let mut` permits interior mutation, but a frozen field still wins.
	res := parseCollectAndCheck(t, entityStruct+`
		let mut e = Entity { id: 1, pos: 0 }
		e.id = 2
	`, false)
	assertErrorsAre(t, res,
		"cannot mutate readonly field \"id\": it is immutable after construction")
}

// A frozen struct-typed field is deeply immutable: you cannot mutate through it.
const bodyStruct = `
	struct Point {
		x: i64,
		y: i64,
	}
	struct Body {
		readonly origin: Point,
		pos: Point,
	}
`

func TestTypeCheck_FrozenField_DeepThrough_Error(t *testing.T) {
	res := parseCollectAndCheck(t, bodyStruct+`
		var b = Body { origin: Point { x: 0, y: 0 }, pos: Point { x: 1, y: 1 } }
		b.origin.x = 9
	`, false)
	assertErrorsAre(t, res,
		"cannot mutate readonly field \"origin\": it is immutable after construction")
}

func TestTypeCheck_FrozenField_DeepThroughMutable_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, bodyStruct+`
		var b = Body { origin: Point { x: 0, y: 0 }, pos: Point { x: 1, y: 1 } }
		b.pos.x = 9
	`, false)
	assertNoErrors(t, res)
}

// --- index assignment ---

func TestTypeCheck_IndexAssign_Var_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
		var xs = [1, 2, 3]
		xs[0] = 9
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_IndexAssign_Let_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let xs = [1, 2, 3]
		xs[0] = 9
	`, false)
	assertErrorsAre(t, res,
		"xs: `let` binding is deeply immutable; its interior cannot be mutated (use `let mut` to allow interior mutation, or `var` to also allow reassignment)")
}

func TestTypeCheck_IndexAssign_LetMut_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let mut xs = [1, 2, 3]
		xs[0] = 9
	`, false)
	assertNoErrors(t, res)
}

// --- `let mut` still forbids reassigning the name itself ---

func TestTypeCheck_LetMut_Reassign_Name_Error(t *testing.T) {
	res := parseCollectAndCheck(t, pointStruct+`
		let mut p = Point { x: 1, y: 2 }
		p = Point { x: 3, y: 4 }
	`, false)
	assertErrorsAre(t, res, "p: 'let' binding is immutable; use 'var' to allow reassignment")
}

// --- parameter `ref`/`mut`/`own` modifiers govern interior mutation ---
//
// A bare or `ref` parameter is an immutable borrow (no interior mutation); `mut`
// (mutable borrow) and `own` (owned local) both permit it.

func TestTypeCheck_ParamAssign_Bare_Immutable_Error(t *testing.T) {
	res := parseCollectAndCheck(t, pointStruct+`
		let f = (p: Point) -> void => {
			p.x = 5
		}
	`, false)
	assertErrorsAre(t, res,
		"p: parameter is an immutable borrow by default; its interior cannot be mutated (declare it `mut <type>` to mutate the caller's value, or `own <type>` for an owned local copy)")
}

func TestTypeCheck_ParamAssign_Ref_Immutable_Error(t *testing.T) {
	res := parseCollectAndCheck(t, pointStruct+`
		let f = (p: ref Point) -> void => {
			p.x = 5
		}
	`, false)
	assertErrorsAre(t, res,
		"p: parameter is a `ref` (immutable borrow); its interior cannot be mutated (declare it `mut <type>` to mutate the caller's value, or `own <type>` for an owned local copy)")
}

func TestTypeCheck_ParamAssign_Mut_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, pointStruct+`
		let f = (p: mut Point) -> void => {
			p.x = 5
		}
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_ParamAssign_Own_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, pointStruct+`
		let f = (p: own Point) -> void => {
			p.x = 5
		}
	`, false)
	assertNoErrors(t, res)
}
