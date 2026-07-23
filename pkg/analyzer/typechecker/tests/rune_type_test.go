package typechecker_test

import "testing"

// The `rune` primitive type (a Unicode code point, i32 — Go/Odin's `rune`, the
// former `char`) flows through the typechecker like any other scalar. A `'a'`
// character literal has type `rune`.

func TestRune_AnnotatedBinding(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `let c: rune = 'a'`, false))
}

func TestRune_ParamAndReturn(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `let id = (c: rune) -> rune => c`, false))
}

func TestRune_StructField(t *testing.T) {
	src := `struct Cell { ch: rune, }
let first = () -> rune => {
  let c: Cell = Cell { ch: 'x' }
  c.ch
}`
	assertNoErrors(t, parseCollectAndCheck(t, src, false))
}

// Note: a `match` on a rune *literal pattern* (`'a' => …`) is intentionally not
// covered here — character literal patterns don't parse yet (a separate grammar
// gap, unrelated to `rune` as a type), so such a test would pass vacuously on a
// partial tree (parseCollectAndCheck doesn't fail on CST ERROR nodes).

// A real primitive rejects a mismatched initializer — the generic-variable
// representation used to mask this.
func TestRune_RejectsIntegerInitializer(t *testing.T) {
	res := parseCollectAndCheck(t, `let c: rune = 5`, false)
	if len(res.errors) == 0 {
		t.Error("assigning an integer to a `rune` binding should be a type error")
	}
}
