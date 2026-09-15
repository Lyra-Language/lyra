package typechecker_test

import (
	"strings"
	"testing"
)

// ── A fixed array against a `[]t` combinator names the edit (08/14) ──
//
// `["a", "b"].join("")` and `[1, 2, 3].map(f)` are ordinary calls: `[…]` is a `[]T` by
// spelling (09/14). What stays refused, and what these pin, is a **fixed** array — a
// `#[…]` value — reaching a `[]t` combinator, which would widen it, allocating where
// nothing asked, invisibly to `noalloc`. The message names the edits, which is the pattern
// lyra-E046 and the `++` diagnostic both follow.
//
// Declared locally rather than leaning on the prelude's `join`/`map`, so these test the
// mechanism rather than what the standard library happens to ship — and because
// parseCollectAndCheck has no prelude, which is why the first draft of them passed
// vacuously with a bare "member access on non-struct type".

const dynReceivers = `
let squash = (self: []string, sep: string) -> string => sep
let twice<t> = (self: []t) -> i64 => 2
let twice<t> = (self: Maybe<t>) -> i64 => 1
`

func TestArrayBindingReceiver_SingleDeclarationNamesTheEdit(t *testing.T) {
	res := parseCollectAndCheck(t, dynReceivers+`
let main = () => {
  let parts = #["a", "b", "c"]
  println(parts.squash(""))
}
`, false)
	if len(res.errors) == 0 {
		t.Fatal("a fixed-array receiver must be refused")
	}
	msg := res.errors[0].Error()
	for _, want := range []string{"squash takes a dynamic array", "build it with `[…]` rather than `#[…]`", ".slice(0, 3)", "would allocate"} {
		if !strings.Contains(msg, want) {
			t.Errorf("want %q in: %s", want, msg)
		}
	}
}

// The overloaded shape gets the same sentence, and it is the one that matters more:
// `map` and `filter` are what a reader reaches for. Left to the overload branch it answers
// "twice is overloaded on its receiver and takes …" — true, and it still leaves the
// edit to be worked out.
func TestArrayBindingReceiver_OverloadedNameNamesTheEdit(t *testing.T) {
	res := parseCollectAndCheck(t, dynReceivers+`
let main = () => {
  let nums = #[1, 2, 3]
  println(nums.twice())
}
`, false)
	if len(res.errors) == 0 {
		t.Fatal("a fixed-array receiver must be refused")
	}
	msg := res.errors[0].Error()
	if !strings.Contains(msg, "twice takes a dynamic array") {
		t.Errorf("want the array-literal hint, got: %s", msg)
	}
}

// Taking the advice compiles, which is the only test that proves the hint is *correct*
// rather than merely well-worded.
func TestArrayLiteralReceiver_TheSuggestedFixWorks(t *testing.T) {
	res := parseCollectAndCheck(t, dynReceivers+`
let main = () => {
  let parts: []string = ["a", "b", "c"]
  let nums: []i64 = [1, 2, 3]
  println(parts.squash(""))
  println(nums.twice())
}
`, false)
	assertNoErrors(t, res)
}

// A receiver that is not an array is unaffected — the hint must not fire on every
// "has no method" in the language.
func TestArrayLiteralReceiver_OtherReceiversAreUnaffected(t *testing.T) {
	res := parseCollectAndCheck(t, dynReceivers+`
let main = () => println("abc".squash(""))
`, false)
	if len(res.errors) == 0 {
		t.Fatal("a string receiver must still be refused")
	}
	if msg := res.errors[0].Error(); strings.Contains(msg, "dynamic array") {
		t.Errorf("the array hint should not fire on a string receiver: %s", msg)
	}
}

// The literal receiver, which is what this file used to assert was refused: `[…]` is a
// `[]T`, in receiver position as in argument position (`squash(["a"], "")`).
func TestArrayLiteralReceiver_LiteralIsBuiltAsTheReceiverAsks(t *testing.T) {
	res := parseCollectAndCheck(t, dynReceivers+`
let main = () => {
  println(["a", "b", "c"].squash(""))
  println([1, 2, 3].twice())
}
`, false)
	assertNoErrors(t, res)
}

// The **repeat** form is the comma form's variant and is a `[]T` the same way. Reaching it
// needed a grammar change too (08/28): `array_repeat_init` was not a postfix head, so
// `["x"; 3].squash("")` was a *parse* error while the comma form checked fine, which is
// hazard 8's "when adding an expression kind, grep for the kind it is a variant of" seen
// from the grammar side.
func TestArrayLiteralReceiver_RepeatFormToo(t *testing.T) {
	res := parseCollectAndCheck(t, dynReceivers+`
let main = () => println(["x"; 3].squash(""))
`, false)
	assertNoErrors(t, res)
}
