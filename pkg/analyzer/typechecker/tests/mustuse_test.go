package typechecker_test

import "testing"

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

func TestMustUse_DiscardBindingOptOut(t *testing.T) {
	src := mustUsePrelude + `
let main = () -> i64 => {
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
