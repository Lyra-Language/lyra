package typechecker_test

import "testing"

// TestIfLet_ThenBranchTypeChecked: the typechecker previously had no case for
// IfDestructuringStmt at all, so it never descended into Then — a type error
// inside it passed silently. It must now be caught.
func TestIfLet_ThenBranchTypeChecked(t *testing.T) {
	source := `
let f = (arr: [3]i64) -> i64 => {
    if let [a, b, c] = arr {
        let bad: string = a + 1
    }
    0
}`
	res := parseCollectAndCheck(t, source, false)
	assertErrorsAre(t, res, "bad: cannot assign i64 to string")
}

// TestIfLet_BoundNamesTypedInThen: names bound by the if-let pattern resolve
// to their element type inside Then.
func TestIfLet_BoundNamesTypedInThen(t *testing.T) {
	source := `
let f = (arr: [3]i64) -> i64 => {
    if let [a, b, c] = arr {
        a + b + c
    } else {
        0
    }
}`
	res := parseCollectAndCheck(t, source, false)
	assertNoErrors(t, res)
}

// TestIfLet_BoundNamesNotVisibleInElse: reaching Else means the pattern failed
// to match, so it must not see the would-have-been-bound names.
func TestIfLet_BoundNamesNotVisibleInElse(t *testing.T) {
	source := `
let f = (arr: [3]i64) -> i64 => {
    if let [a, b, c] = arr {
        a
    } else {
        a
    }
}`
	res := parseCollectAndCheck(t, source, false)
	assertErrorsAre(t, res, `undefined identifier "a"`)
}

// TestIfLet_BoundNamesNotVisibleAfterStatement: if-let's bound names are
// local to Then only, unlike a plain `let` — they must not leak into code
// following the statement.
func TestIfLet_BoundNamesNotVisibleAfterStatement(t *testing.T) {
	source := `
let f = (arr: [3]i64) -> i64 => {
    if let [a, b, c] = arr {
        a
    } else {
        0
    }
    a
}`
	res := parseCollectAndCheck(t, source, false)
	assertErrorsAre(t, res, `undefined identifier "a"`)
}

// TestLetElse_BoundNamesTypedAfterStatement: let-else's bound names persist
// for code after the statement, like a plain `let`.
func TestLetElse_BoundNamesTypedAfterStatement(t *testing.T) {
	source := `
struct Point { x: i64, y: i64 }
let f = (p: Point) -> i64 => {
    let {x, y} = p else {
        return 0
    }
    x + y
}`
	res := parseCollectAndCheck(t, source, false)
	assertNoErrors(t, res)
}

// TestLetElse_TypeErrorAfterStatementCaught: like TestIfLet_ThenBranchTypeChecked,
// the absence of a checkNode case meant the whole statement (and everything
// type-dependent after it) was skipped; a type error using the bound names
// must now be caught.
func TestLetElse_TypeErrorAfterStatementCaught(t *testing.T) {
	source := `
struct Point { x: i64, y: i64 }
let f = (p: Point) -> i64 => {
    let {x, y} = p else {
        return 0
    }
    let bad: string = x + y
    0
}`
	res := parseCollectAndCheck(t, source, false)
	assertErrorsAre(t, res, "bad: cannot assign i64 to string")
}

// TestLetElse_BoundNamesNotVisibleInElse: the diverging Else branch runs only
// when the pattern failed to match, so it must not see the names.
func TestLetElse_BoundNamesNotVisibleInElse(t *testing.T) {
	source := `
struct Point { x: i64, y: i64 }
let f = (p: Point) -> i64 => {
    let {x, y} = p else {
        x
    }
    0
}`
	res := parseCollectAndCheck(t, source, false)
	assertErrorsAre(t, res, `undefined identifier "x"`)
}

// TestIfLet_ArityMismatchLeavesNamesUnbound: an if-let pattern that doesn't
// match its scrutinee's static shape (a tuple's arity, checked exactly, unlike
// arrays) is a compile-time error, mirroring plain destructuring — the
// mismatched names stay unbound rather than guessed.
func TestIfLet_ArityMismatchLeavesNamesUnbound(t *testing.T) {
	source := `
let f = (pair: (i64, i64)) -> i64 => {
    if let (a, b, c) = pair {
        a
    } else {
        0
    }
}`
	res := parseCollectAndCheck(t, source, false)
	assertErrorsAre(t, res,
		"tuple pattern has 3 element(s) but tuple has 2",
		`undefined identifier "a"`)
}

// An if-let is a *statement*, so neither branch is in value position: an ordinary
// one-armed `if` as the last thing in a branch is a conditional side effect, not a
// valueless expression. It was rejected until both branches started being checked
// for effect (the same fix a loop body needed).
func TestIfLet_OneArmedIfAsLastStatementInBranch(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
data Maybe = Some(i64) | None
let main = () -> u8 => {
  let m = Some(5)
  var out = 0
  if let Some(v) = m {
    if v > 1 { out = v }
  } else {
    if out == 0 { out = 1 }
  }
  u8(out)
}
`, false))
}

// The same for a `let … else` branch.
func TestLetElse_OneArmedIfAsLastStatementInBranch(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
data Maybe = Some(i64) | None
let f = (m: Maybe, flag: bool) -> i64 => {
  let Some(v) = m else {
    if flag { return 0 }
    return 1
  }
  v
}
`, false))
}

// Checking a branch for effect must not lose diagnostics: an expression statement
// the statement-position switch doesn't name is still inferred, so a bare
// undefined identifier is reported. That hole was masked in a *value* block, whose
// trailing value inference checked the final statement as a side effect.
func TestIfLet_UndefinedIdentifierInBranchIsReported(t *testing.T) {
	res := parseCollectAndCheck(t, `
data Maybe = Some(i64) | None
let main = () -> u8 => {
  let m = Some(5)
  if let Some(v) = m {
    undefinedThing
  }
  0
}
`, false)
	assertErrorsAre(t, res, `undefined identifier "undefinedThing"`)
}
