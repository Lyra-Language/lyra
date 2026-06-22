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
