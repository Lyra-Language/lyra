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

// ── `mut` arguments at the call site ──────────────────────────────────────────
//
// A `mut` parameter mutates the caller's value (the backend passes it by
// reference), so the argument must be an lvalue rooted at a mutable binding —
// the same rule that governs writing through the path directly. Neither was
// checked before: passing a temporary silently discarded the writes, and passing
// a deeply-immutable `let` mutated it anyway, bypassing the mutability system
// through a function call.

func TestTypeCheck_MutArgument_VarBinding_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, pointStruct+`
		let poke = (p: mut Point) -> void => { p.x = 5 }
		let run = () -> void => {
			var q = Point { x: 1, y: 2 }
			poke(q)
		}
	`, false)
	assertNoErrors(t, res)
}

func TestTypeCheck_MutArgument_ImmutableBinding_Error(t *testing.T) {
	res := parseCollectAndCheck(t, pointStruct+`
		let poke = (p: mut Point) -> void => { p.x = 5 }
		let run = () -> void => {
			let q = Point { x: 1, y: 2 }
			poke(q)
		}
	`, false)
	assertErrorsAre(t, res,
		`poke: argument 1 (p): cannot pass immutable binding "q" to a `+"`mut`"+` parameter (declare it `+"`var`"+`, or take the parameter by value)`)
}

func TestTypeCheck_MutArgument_Temporary_Error(t *testing.T) {
	res := parseCollectAndCheck(t, pointStruct+`
		let poke = (p: mut Point) -> void => { p.x = 5 }
		let run = () -> void => {
			poke(Point { x: 1, y: 2 })
		}
	`, false)
	assertErrorsAre(t, res,
		"poke: argument 1 (p): a `mut` parameter mutates the caller's value, so the argument must be a variable or a field/element of one, not a temporary")
}

// An lvalue *path* (not just a bare binding) is fine — the call site takes the
// element's address the same way an assignment target would.
func TestTypeCheck_MutArgument_ElementPath_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, pointStruct+`
		let poke = (p: mut Point) -> void => { p.x = 5 }
		let run = () -> void => {
			var ps: [2]Point = [Point { x: 1, y: 2 }, Point { x: 3, y: 4 }]
			poke(ps[0])
		}
	`, false)
	assertNoErrors(t, res)
}

// Forwarding a `mut` parameter onward is allowed: it is already a mutable borrow.
func TestTypeCheck_MutArgument_ForwardedMutParam_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, pointStruct+`
		let inner = (p: mut Point) -> void => { p.x = 5 }
		let outer = (p: mut Point) -> void => { inner(p) }
	`, false)
	assertNoErrors(t, res)
}

// A `ref` parameter is an immutable borrow, so forwarding it to a `mut`
// parameter must be rejected — otherwise `mut` would launder away the `ref`.
func TestTypeCheck_MutArgument_ForwardedRefParam_Error(t *testing.T) {
	res := parseCollectAndCheck(t, pointStruct+`
		let inner = (p: mut Point) -> void => { p.x = 5 }
		let outer = (p: ref Point) -> void => { inner(p) }
	`, false)
	assertErrorsAre(t, res,
		`inner: argument 1 (p): cannot pass immutable binding "p" to a `+"`mut`"+` parameter (declare it `+"`var`"+`, or take the parameter by value)`)
}

// A copied scalar is exempt: `mut` is inert there (lyra-W010), nothing is written
// through, so an ordinary value argument — even a literal — is accepted.
func TestTypeCheck_MutArgument_ScalarLiteral_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let twice = (n: mut i64) -> i64 => n + n
		let run = () -> i64 => twice(21)
	`, false)
	assertNoErrors(t, res)
}

// ── reassigning a parameter binding ───────────────────────────────────────────
//
// A parameter is not a VarDeclStmt in scope (it lives in paramTypes), so
// checkAssignToBinding used to bail before inferring the RHS — leaving `n = …`
// on a parameter **entirely unchecked**: no assignability check, no literal-range
// check, and not even an undefined-identifier report, plus no recorded types for
// the backend (which then failed loudly on integer arithmetic and *panicked* on a
// mismatched store).

func TestTypeCheck_ParamReassign_TypeMismatch_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let f = (n: own i64) -> i64 => { n = "hello"  n }
	`, false)
	assertErrorsAre(t, res, "n: cannot assign string to i64")
}

func TestTypeCheck_ParamReassign_BoolToInt_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let f = (n: own i64) -> i64 => { n = true  n }
	`, false)
	assertErrorsAre(t, res, "n: cannot assign boolean to i64")
}

// The literal-range check now runs on a parameter target too.
func TestTypeCheck_ParamReassign_LiteralOverflow_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let f = (n: own i8) -> i8 => { n = 9999  n }
	`, false)
	assertErrorsAre(t, res, "n: literal value 9999 overflows i8")
}

// The RHS is inferred, so ordinary diagnostics inside it surface — this one went
// completely unreported before.
func TestTypeCheck_ParamReassign_UndefinedInRHS_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let f = (n: own i64) -> i64 => { n = undefinedVar  n }
	`, false)
	assertErrorsAre(t, res, `undefined identifier "undefinedVar"`)
}

// A well-typed reassignment is accepted for the modes where the write means
// something: `own` (the value was transferred, so the copy is the callee's) and `mut`
// (a reference to the caller's storage).
func TestTypeCheck_ParamReassign_WellTyped_Ok(t *testing.T) {
	for _, src := range []string{
		`let f = (n: own i64) -> i64 => { n = n + 1  n }`,
		`let f = (n: i64, k: own i64) -> i64 => { k = n * 2; k }`,
		`let f = (n: own i64) -> i64 => { n = 5  n }`,
		`let f = (n: mut i64) -> i64 => { n = 5  n }`,
		`let f = (x: own f64) -> f64 => { x = x + 1.5  x }`,
	} {
		res := parseCollectAndCheck(t, src, false)
		assertNoErrors(t, res)
	}
}

// Reassigning a *borrowed* parameter — no modifier, or `ref` — is rejected
// (lyra-E025). The caller still owns the value, so the write could only reach the
// callee's own copy: the same lost-write class as assigning to a captured binding
// (E024) and the by-value `mut` parameter that silently dropped its writes.
//
// It also restores consistency with the binding model. `let x = 5; x = 6` is an
// error, yet a bare parameter used to accept exactly that — making a parameter the
// most permissive rung, with no syntax for the immutable one.
func TestTypeCheck_ParamReassign_Borrowed_Error(t *testing.T) {
	for _, src := range []string{
		`let f = (n: i64) -> i64 => { n = 5  n }`,
		`let f = (n: ref i64) -> i64 => { n = 5  n }`,
		`let f = (s: string) -> string => { s = "a"  s }`,
		`let f = (n: i64, k: i64) -> i64 => { k = n * 2; k }`,
	} {
		res := parseCollectAndCheck(t, src, false)
		assertErrorContainsGeneric(t, res, "cannot reassign a borrowed parameter")
	}
}

// Shadowing is the replacement, including the form that derives from the parameter —
// which required teaching the use-before-declaration checker that a parameter is in
// scope for the whole body.
func TestTypeCheck_ParamShadowing_Ok(t *testing.T) {
	for _, src := range []string{
		`let f = (s: string) -> string => { let s = s ++ "!"  s }`,
		`let f = (n: i64) -> i64 => { let n = n + 1  n }`,
		`let f = (n: i64) -> i64 => { let n = 5  n }`,
	} {
		res := parseCollectAndCheck(t, src, false)
		assertNoErrors(t, res)
	}
}

// ── exclusive mutable borrow ──────────────────────────────────────────────────
//
// `mut` and `ref` arguments are pointers to the caller's storage, so two of them
// naming one binding are two views of the same memory. If either is `mut`, what
// the other observes depends on statement order inside the callee — so a `mut`
// borrow is exclusive, as in Rust. Both shapes below compiled silently before.

func TestTypeCheck_ExclusiveMut_RefAndMutSameBinding_Error(t *testing.T) {
	res := parseCollectAndCheck(t, pointStruct+`
		let both = (a: ref Point, b: mut Point) -> i64 => { b.x = 99  a.x }
		let run = () -> i64 => {
			var p = Point { x: 1, y: 2 }
			both(p, p)
		}
	`, false)
	assertErrorsAre(t, res,
		"both: \"p\" is passed to argument 2 as `mut` and also to argument 1 — a `mut` borrow is exclusive, so no other argument of the same call may name it")
}

func TestTypeCheck_ExclusiveMut_TwoMutSameBinding_Error(t *testing.T) {
	res := parseCollectAndCheck(t, pointStruct+`
		let two = (a: mut Point, b: mut Point) -> i64 => { a.x = 10  b.x = 20  a.x }
		let run = () -> i64 => {
			var p = Point { x: 1, y: 2 }
			two(p, p)
		}
	`, false)
	assertErrorsAre(t, res,
		"two: \"p\" is passed to argument 1 as `mut` and also to argument 2 — a `mut` borrow is exclusive, so no other argument of the same call may name it")
}

// Two `ref` arguments may name one binding — neither can write, so there is
// nothing to observe.
func TestTypeCheck_ExclusiveMut_TwoRefSameBinding_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, pointStruct+`
		let both = (a: ref Point, b: ref Point) -> i64 => a.x + b.y
		let run = () -> i64 => {
			let p = Point { x: 1, y: 2 }
			both(p, p)
		}
	`, false)
	assertNoErrors(t, res)
}

// Distinct bindings are unaffected.
func TestTypeCheck_ExclusiveMut_DistinctBindings_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, pointStruct+`
		let both = (a: ref Point, b: mut Point) -> i64 => { b.x = 9  a.x }
		let run = () -> i64 => {
			let p = Point { x: 1, y: 2 }
			var q = Point { x: 3, y: 4 }
			both(p, q)
		}
	`, false)
	assertNoErrors(t, res)
}

// Scalars are exempt: they are passed by value, so there is no shared storage.
func TestTypeCheck_ExclusiveMut_ScalarSameBinding_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let both = (a: ref i64, b: mut i64) -> i64 => a + b
		let run = () -> i64 => {
			var n = 5
			both(n, n)
		}
	`, false)
	assertNoErrors(t, res)
}
