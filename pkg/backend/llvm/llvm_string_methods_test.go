package llvm

import (
	"strings"
	"testing"
)

// `s.len()` and `s.slice(a, b)` (string_methods.go) and the prelude's trim family
// built on them.
//
// Every test here uses a **multi-byte** string somewhere, because that is the only
// thing that separates the rune semantics these have from the byte semantics they
// would have had: "hello".len() is 5 either way, and "héllo" is 5 or 6 depending on
// which language you built.

// The whole reason `len` counts runes: it has to agree with `s[i]` and `for c in s`,
// which counted runes before it existed. A byte-based length passes on ASCII and
// fails on the first accented character — so the assertion is the byte count being
// *wrong*, not the rune count being right.
func TestExec_StringLenCountsRunesNotBytes(t *testing.T) {
	t.Parallel()
	const src = `
module main
let main = () -> void => {
  let s = "héllo";
  let empty = "";
  let cjk = "日本語";
  println("${s.len()} ${empty.len()} ${cjk.len()}");
}
`
	// "héllo" is 6 bytes / 5 runes; "日本語" is 9 bytes / 3 runes.
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "5 0 3" {
		t.Errorf("len() = %q; want \"5 0 3\" (runes). Byte counts would be \"6 0 9\"", got)
	}
}

// The loop `len` exists to make writable, and the one that would break silently if
// `len` were bytes: indexing every position it reports must reproduce the string.
func TestExec_StringLenAgreesWithIndexing(t *testing.T) {
	t.Parallel()
	const src = `
module main
let main = () -> void => {
  let s = "héllo";
  var i = 0;
  for i < s.len() {
    print(s[i]);
    i = i + 1;
  }
  println("");
}
`
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "héllo" {
		t.Errorf("indexing 0..<len() rebuilt %q; want \"héllo\"", got)
	}
}

// `slice` is a half-open rune range, matching `..<` and array indexing. The
// boundaries are the interesting part: an empty slice in the middle, a full-width
// slice, and a slice whose end is exactly the rune count (which the walk reaches only
// as its bytes run out, not inside the loop).
func TestExec_StringSliceIsHalfOpenAndRuneIndexed(t *testing.T) {
	t.Parallel()
	const src = `
module main
let main = () -> void => {
  let s = "héllo";
  println("[${s.slice(0, 2)}]");
  println("[${s.slice(1, 5)}]");
  println("[${s.slice(2, 2)}]");
  println("[${s.slice(0, 5)}]");
  println("[${s.slice(5, 5)}]");
}
`
	want := "[hé]\n[éllo]\n[]\n[héllo]\n[]"
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != want {
		t.Errorf("slice results:\n%s\nwant:\n%s", got, want)
	}
}

// Out of range traps, and so does an *inverted* range — `start > end` is a caller
// bug, and the empty string it would otherwise produce is indistinguishable from a
// correct empty slice. A negative bound traps rather than wrapping to a huge
// unsigned value and merely failing to be found.
func TestExec_StringSliceTrapsOutOfRange(t *testing.T) {
	t.Parallel()
	for name, expr := range map[string]string{
		"end past the rune count": `s.slice(0, 6)`,
		"start past the end":      `s.slice(3, 1)`,
		"negative start":          `s.slice(-1, 2)`,
		"start past the string":   `s.slice(6, 6)`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			src := `
let main = () -> void => {
  let s = "héllo";
  println("[${` + expr + `}]");
}
`
			// buildAndRunPanic, not buildAndRunWithPrelude: a trap's observable
			// behavior is the message on fd 2 and the exit status, and the
			// stdout-capturing helper sees neither.
			stderr, code := buildAndRunPanic(t, src)
			if code != trapExitCode {
				t.Errorf("%s exited %d; want %d (the trap)", expr, code, trapExitCode)
			}
			if !strings.Contains(stderr, "string slice out of range") {
				t.Errorf("%s wrote %q; want the string-slice trap", expr, stderr)
			}
		})
	}
}

// The trap must name a *string*, not an array. Both the index and the slice reported
// "array index out of bounds" until 08/06, which sends the reader looking for an
// array that is not there.
func TestExec_StringIndexTrapNamesTheString(t *testing.T) {
	t.Parallel()
	const src = `
let main = () -> void => {
  let s = "héllo";
  println("${s[9]}");
}
`
	stderr, code := buildAndRunPanic(t, src)
	if code != trapExitCode {
		t.Errorf("an out-of-range string index exited %d; want %d (the trap)", code, trapExitCode)
	}
	if !strings.Contains(stderr, "string index out of bounds") {
		t.Errorf("an out-of-range string index wrote %q; want the string-index trap", stderr)
	}
}

// The trim family is ordinary Lyra in std/prelude.lyra, so this exercises the
// prelude's real source rather than a pasted copy.
func TestExec_TrimFamily(t *testing.T) {
	t.Parallel()
	const src = `
module main
let main = () -> void => {
  let a = "  héllo  ";
  println("[${a.trim()}][${a.trim_start()}][${a.trim_end()}]");
  let blank = "   ";
  let empty = "";
  let solid = "no-space";
  let mixed = "\t\n x \r\n";
  println("[${blank.trim()}][${empty.trim()}][${solid.trim()}][${mixed.trim()}]");
}
`
	want := "[héllo][héllo  ][  héllo]\n[][][no-space][x]"
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != want {
		t.Errorf("trim results:\n%s\nwant:\n%s", got, want)
	}
}

// The effect boundary, and the bug writing `trim` exposed. A builtin method is
// charged **no** effect by all three copies of the purity pass's dispatch ladder,
// which is what makes `x.wrapping_mul(y)` usable from `pure noalloc` code — and
// `slice` is the first builtin method that genuinely allocates, so it was invisible
// to every one of them at once and `noalloc` accepted a function that allocates on
// every call. The typechecker now records whether the resolved builtin allocates.
//
// `len` and `s[i]` must stay allocation-free, or the fix would have been a blanket
// "builtin methods allocate", which is wrong in the other direction.
func TestCheck_StringSliceAllocatesButLenDoesNot(t *testing.T) {
	t.Parallel()
	t.Run("len and index are noalloc", func(t *testing.T) {
		t.Parallel()
		const src = `
module main
let count = pure noalloc (s: string) -> i64 => s.len()
let first = pure noalloc (s: string) -> rune => s[0]
let main = () -> void => { println("${count("ab")} ${first("ab")}") }
`
		if diags := checkWithPrelude(t, src); len(diags) != 0 {
			t.Errorf("len/index allocate nothing and should be `noalloc`, got: %v", diags)
		}
	})
	t.Run("slice is refused by noalloc", func(t *testing.T) {
		t.Parallel()
		const src = `
module main
let head = pure noalloc (s: string) -> string => s.slice(0, 1)
let main = () -> void => { println(head("ab")) }
`
		diags := checkWithPrelude(t, src)
		if len(diags) == 0 {
			t.Fatal("`slice` builds a fresh string, so `noalloc` must refuse it")
		}
		if !strings.Contains(diags[0], "noalloc") {
			t.Errorf("expected the noalloc diagnostic, got: %s", diags[0])
		}
	})
	t.Run("trim reaches slice, so noalloc refuses it too", func(t *testing.T) {
		t.Parallel()
		const src = `
module main
let clean = pure noalloc (s: string) -> string => s.trim()
let main = () -> void => { println(clean(" a ")) }
`
		if diags := checkWithPrelude(t, src); len(diags) == 0 {
			t.Fatal("`trim` allocates through `slice`; `noalloc` must refuse it")
		}
	})
	t.Run("trim is still pure", func(t *testing.T) {
		t.Parallel()
		const src = `
module main
let clean = pure (s: string) -> string => s.trim()
let main = () -> void => { println(clean(" a ")) }
`
		if diags := checkWithPrelude(t, src); len(diags) != 0 {
			t.Errorf("allocation is orthogonal to purity, so `trim` is pure; got: %v", diags)
		}
	})
}

// Two `slice` results in one expression, which is the shape that broke: `slice`
// branches (a bounds trap and a decode loop), so it ends in a *continuation* block,
// and flushStmtTemps used to release any temp not produced in the statement's start
// block right there — i.e. before the rest of the statement ran. The first result was
// freed before the second allocated, and the second allocation landed on the freed
// bytes, so both halves printed the second slice: `cd cd`. Fixed 08/07 by asking
// dominance rather than block identity (llvm.go).
//
// Three forms, because the corruption showed up differently in each: interpolation
// dropped the first value, `++` also lost the literal between them, and mixing in a
// `trim` (prelude Lyra over the same builtin) inherited it.
func TestExec_TwoSlicesInOneExpression(t *testing.T) {
	t.Parallel()
	const src = `
module main
let main = () -> void => {
  let s = "abcdef";
  println("${s.slice(0,2)} ${s.slice(2,4)}");
  println(s.slice(0,2) ++ "|" ++ s.slice(2,4));
  println("${s.slice(0,2)} ${s.slice(2,4)} ${s.slice(4,6)}");
}
`
	want := "ab cd\nab|cd\nab cd ef"
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != want {
		t.Errorf("two slices in one expression =\n%q\nwant\n%q", got, want)
	}
}

// The same defect reached through the prelude: `trim` is ordinary Lyra built on
// `slice`, so a `trim` beside a `slice` in one interpolation was corrupted too — and
// this is the form a user would actually write.
func TestExec_TrimBesideSliceInOneInterpolation(t *testing.T) {
	t.Parallel()
	const src = `
module main
let main = () -> void => {
  let s = "  héllo  ";
  println("${s.len()} ${s.trim()} ${s.slice(2, 4)}");
}
`
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "9 héllo hé" {
		t.Errorf("trim beside slice = %q; want \"9 héllo hé\"", got)
	}
}
