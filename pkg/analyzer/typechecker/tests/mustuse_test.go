package typechecker_test

import (
	"testing"

	"github.com/Lyra-Language/lyra/pkg/parser"
)

// The must-use rule flags a statement that produces a Result or Maybe and then
// discards it (no binding, no match, no `?`-propagation). The wording is shared
// across cases.
func mustUseMsg(kind string) string {
	return "unused " + kind + ": the value is discarded without handling its error/absence; " +
		"match on it, propagate it with `?`, or bind it (`let _ = ...`) to discard it intentionally"
}

const mustUsePrelude = `
data Result<t, e> = Ok t | Err e
data Maybe<t> = Some t | None
let parse = (s: string) -> Result<i64, E> => { Ok(0) }
let lookup = (s: string) -> Maybe<i64> => { Some(0) }
`

// --- positive cases: a dropped Result/Maybe is flagged ----------------------

func TestMustUse_DroppedResultCall(t *testing.T) {
	src := mustUsePrelude + `
let main = () -> i64 => {
    parse("x")
    0
}`
	res := parseCollectAndCheck(t, src, false)
	assertWarningsAre(t, res, mustUseMsg("Result"))
}

func TestMustUse_DroppedMaybeCall(t *testing.T) {
	src := mustUsePrelude + `
let main = () -> i64 => {
    lookup("x")
    0
}`
	res := parseCollectAndCheck(t, src, false)
	assertWarningsAre(t, res, mustUseMsg("Maybe"))
}

func TestMustUse_MultipleDropped(t *testing.T) {
	src := mustUsePrelude + `
let main = () -> i64 => {
    parse("x")
    lookup("y")
    0
}`
	res := parseCollectAndCheck(t, src, false)
	assertWarningsAre(t, res, mustUseMsg("Result"), mustUseMsg("Maybe"))
}

// --- negative cases: the value is used, so no diagnostic --------------------

func TestMustUse_BoundResult(t *testing.T) {
	src := mustUsePrelude + `
let main = () -> i64 => {
    let r = parse("x")
    0
}`
	res := parseCollectAndCheck(t, src, false)
	assertNoMustUseWarnings(t, res)
}

// Binding to `_` discards the value deliberately, which is the opt-out the must-use
// warning itself recommends — `bind it (let _ = ...) to discard it intentionally`.
//
// That advice was impossible to follow until 08/07: a bare `_` in binding position was
// read as a *destructuring* pattern and recovered as a `data_pattern` with an empty name.
// This test asserted the opt-out worked and passed for the wrong reason — the source did
// not parse, so the truncated AST contained no call to warn about. The does-it-parse
// guard is what surfaced it.
func TestMustUse_DiscardBindingOptOut(t *testing.T) {
	src := mustUsePrelude + `
let f = () -> i64 => {
    let _ = parse("x")
    0
}`
	res := parseCollectAndCheck(t, src, false)
	assertNoMustUseWarnings(t, res)
}

func TestMustUse_NamedDiscardBindingOptOut(t *testing.T) {
	src := mustUsePrelude + `
let main = () -> i64 => {
    let _ignored = parse("x")
    0
}`
	res := parseCollectAndCheck(t, src, false)
	assertNoMustUseWarnings(t, res)
}

// Propagating with `?` is a use, not a drop: the success payload here is a
// plain i64, so nothing is flagged.
func TestMustUse_TryPropagationIsUse(t *testing.T) {
	src := mustUsePrelude + `
let run = () -> Result<i64, E> => {
    parse("x")?
    Ok(0)
}`
	res := parseCollectAndCheck(t, src, false)
	assertNoMustUseWarnings(t, res)
}

// A function returning a non-Result/Maybe type may be called for its effect.
func TestMustUse_NonResultCallNotFlagged(t *testing.T) {
	src := `
let log = (s: string) -> i64 => { 0 }
let main = () -> i64 => {
    log("hi")
    0
}`
	res := parseCollectAndCheck(t, src, false)
	assertNoMustUseWarnings(t, res)
}

// A `data Result` whose constructors aren't Ok/Err isn't must-use-eligible:
// there's no Err channel to silently discard, so dropping it is fine.
func TestMustUse_NonCanonicalResultShapeNotFlagged(t *testing.T) {
	src := `
data Result<a, b> = Foo a | Bar b
let make = (n: i64) -> Result<i64, i64> => { Foo(n) }
let main = () -> i64 => {
    make(0)
    0
}`
	res := parseCollectAndCheck(t, src, false)
	assertNoMustUseWarnings(t, res)
}

// assertNoMustUseWarnings fails if any must-use (lyra-W006) warning was emitted.
// Other diagnostics (if any) are ignored so these cases stay focused.
func assertNoMustUseWarnings(t *testing.T, res checkResult) {
	t.Helper()
	for _, e := range res.errors {
		if e.Code == "lyra-W006" {
			t.Errorf("unexpected must-use warning: %s", e.Message)
		}
	}
}

// The discard must still *evaluate* — `let _ = f()` is how you call something for its
// effect and throw the result away, so eliding the call would be a silent behaviour
// change rather than an optimization.
func TestMustUse_DiscardBindingStillEvaluates(t *testing.T) {
	res := parseCollectAndCheck(t, `
		let noisy = () -> i64 => 7
		let f = () -> i64 => {
			let _ = noisy()
			0
		}
	`, false)
	assertNoErrors(t, res)
}

// A wildcard binding is not a name: `_` is a discard, and reading it back is a *syntax*
// error rather than an undefined-identifier one — `_` is not an expression at all.
func TestMustUse_DiscardBindingIsNotAName(t *testing.T) {
	tree, err := parser.Parse(`let f = () -> i64 => { let _ = 5; _ }`)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if !tree.RootNode().HasError() {
		t.Error("`_` is a discard, not a binding — reading it back should not parse")
	}
}
