package typechecker_test

import "testing"

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
