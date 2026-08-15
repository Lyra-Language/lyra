package typechecker_test

import (
	"strings"
	"testing"
)

// ── A fixed-array receiver against a `[]t` combinator names the edit (08/14) ──
//
// `["a", "b"].join("")` and `[1, 2, 3].map(f)` fail identically: an array literal infers a
// *fixed* array, every `[]t` combinator takes a dynamic one, and UFCS does not widen. That
// last part is deliberate — `[N]T` is a stack value and `[]T` a heap box, so widening at a
// call site would allocate where nothing asked it to, invisibly to `noalloc`.
//
// So the rule stays and the message carries the cost, which is the pattern lyra-E046 and
// the `++` diagnostic both follow: name the edit, not just the mismatch.
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

func TestArrayLiteralReceiver_SingleDeclarationNamesTheEdit(t *testing.T) {
	res := parseCollectAndCheck(t, dynReceivers+`
let main = () => println(["a", "b", "c"].squash(""))
`, false)
	if len(res.errors) == 0 {
		t.Fatal("a fixed-array receiver must be refused")
	}
	msg := res.errors[0].Error()
	for _, want := range []string{"squash takes a dynamic array", "`[]string`", "would allocate"} {
		if !strings.Contains(msg, want) {
			t.Errorf("want %q in: %s", want, msg)
		}
	}
}

// The overloaded shape gets the same sentence, and it is the one that matters more:
// `map` and `filter` are what a reader reaches for. Left to the overload branch it answers
// "twice is overloaded on its receiver and takes …" — true, and it still leaves the
// annotation to be worked out.
func TestArrayLiteralReceiver_OverloadedNameNamesTheEdit(t *testing.T) {
	res := parseCollectAndCheck(t, dynReceivers+`
let main = () => println([1, 2, 3].twice())
`, false)
	if len(res.errors) == 0 {
		t.Fatal("a fixed-array receiver must be refused")
	}
	msg := res.errors[0].Error()
	if !strings.Contains(msg, "twice takes a dynamic array") {
		t.Errorf("want the array-literal hint, got: %s", msg)
	}
	// **The suggested annotation has to parse.** An unannotated `[1, 2, 3]` has untyped
	// elements, which render as "integer literal" — a phrase, not a type — so the element
	// is defaulted before it is named. The rule the generated documentation follows: a
	// spelling offered to a reader must compile.
	if !strings.Contains(msg, "`[]i64`") || strings.Contains(msg, "integer literal`") {
		t.Errorf("the suggestion must name a spellable type; got: %s", msg)
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
