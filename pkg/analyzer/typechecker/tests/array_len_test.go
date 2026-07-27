package typechecker_test

import "testing"

// `xs.len()` is a compiler-provided method on any array (fixed-size or dynamic)
// returning i64 — usable wherever an i64 is expected.
func TestTypeCheck_ArrayLen(t *testing.T) {
	cases := []string{
		`let xs: []i64 = [1, 2, 3]
let n: i64 = xs.len()`,
		`let xs: [4]u8 = [1, 2, 3, 4]
let n: i64 = xs.len()`,
		`let xs: shared [3]i64 = [1, 2, 3]
let n: i64 = xs.len()`,
		// len() composes in arithmetic and comparisons.
		`let xs: []i64 = [1, 2, 3]
let last: i64 = xs.len() - 1`,
	}
	for _, src := range cases {
		res := parseCollectAndCheck(t, src, false)
		assertNoErrors(t, res)
	}
}

// A user type with no `len` method still errors — the builtin is arrays-only.
func TestTypeCheck_Len_NotOnArbitraryType(t *testing.T) {
	res := parseCollectAndCheck(t, `let n: i64 = 5
let m: i64 = n.len()`, false)
	if len(res.errors) == 0 {
		t.Fatal("expected an error for .len() on a non-array receiver, got none")
	}
}
