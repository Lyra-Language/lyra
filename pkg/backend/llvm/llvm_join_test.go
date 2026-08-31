package llvm

import (
	"strings"
	"testing"
)

// `join` — `split`'s inverse (`std/prelude/strings.lyra`, 08/14).
//
// Generic over the element rather than taking `[]string`, so a list of anything printable
// joins without being converted first. A `[]string` pays nothing for that, since
// `impl Show for string` returns the value rather than interpolating it.
func TestExec_Join(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, expr, want string }{
		{"with a separator", `parts.join(", ")`, "a, b, c"},
		{"with none", `parts.join()`, "abc"},
		// `sep` goes *between* parts, never around them — so one element uses none.
		{"a single element", `one.join(", ")`, "solo"},
		{"an empty array", `none.join(", ")`, ""},
		// The Show bound is what makes this work on something that is not a string.
		{"a list of integers", `nums.join(" - ")`, "1 - 2 - 3"},
		{"roundtrips with split", `"a::b::c".split("::").join("::")`, "a::b::c"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			src := `
let main = () -> void => {
  let parts: []string = ["a", "b", "c"];
  let one: []string = ["solo"];
  let none: []string = [];
  let nums: []i64 = [1, 2, 3];
  println("[" ++ ` + c.expr + ` ++ "]");
}
`
			want := "[" + c.want + "]"
			if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != want {
				t.Errorf("%s = %q; want %q", c.expr, got, want)
			}
		})
	}
}

// The frame-buffer shape it was added for: rows accumulated with `push`, then joined once.
func TestExec_JoinBuildsAFrame(t *testing.T) {
	t.Parallel()
	src := `
let main = () -> void => {
  var rows: []string = []
  for r in 0..<3 { rows.push("row${r}") }
  println(rows.join("\n"));
}
`
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "row0\nrow1\nrow2" {
		t.Errorf("got %q; want three rows", got)
	}
}

// A literal receiver reaching a `[]t` combinator, against the **real** prelude — which is
// what a reader actually meets: `join` is declared once and `map` is overloaded on three
// receivers, and both must work, since a literal is built in the shape its receiver asks
// for. This asserted the *refusal* and its hint until 08/28.
//
// Checked here rather than in the typechecker's own tests because those run without a
// prelude — `join` and `map` are not declared there, so an earlier draft of this passed
// vacuously against a bare "member access on non-struct type".
func TestExec_ArrayLiteralReceiverReachesAPreludeCombinator(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct{ src, want string }{
		"join, declared once": {
			`let main = () -> void => { println(["a", "b"].join("-")); }`,
			"a-b",
		},
		"map, overloaded on three receivers": {
			`let main = () -> void => { println("${[1, 2].map((x: i64) -> i64 => x * 10).len()}"); }`,
			"2",
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if errs := checkWithPrelude(t, tc.src+"\n"); len(errs) != 0 {
				t.Fatalf("expected a clean program; got: %s", strings.Join(errs, "\n"))
			}
			if got := strings.TrimSpace(buildAndRunWithPrelude(t, tc.src, "")); got != tc.want {
				t.Errorf("got %q; want %q", got, tc.want)
			}
		})
	}
}

// The hint still exists and still names the edit — for a fixed-array **binding**, where
// the value already exists as a stack `[N]T` and reaching a `[]t` combinator would widen
// it. That is the case the rule is actually about.
func TestCheck_ArrayBindingReceiverNamesTheEditAgainstThePrelude(t *testing.T) {
	t.Parallel()
	src := `let main = () -> void => {
  let parts = ["a", "b"]
  println(parts.join(""))
}`
	errs := strings.Join(checkWithPrelude(t, src+"\n"), "\n")
	if !strings.Contains(errs, "join takes a dynamic array") {
		t.Errorf("want the array hint; got: %s", errs)
	}
	if !strings.Contains(errs, "annotate the value as") {
		t.Errorf("the hint should name the annotation; got: %s", errs)
	}
}

// The repeat form reaching a prelude combinator, end to end. It needed both halves: the
// typechecker's literal allowance (which covered both array forms from the start) and a
// grammar change making `array_repeat_init` a postfix head, without which this was a
// parse error while `["x", "x", "x"].join("-")` compiled.
func TestExec_ArrayRepeatReceiverReachesAPreludeCombinator(t *testing.T) {
	t.Parallel()
	src := `let main = () -> void => { println(["x"; 3].join("-")); }`
	if errs := checkWithPrelude(t, src+"\n"); len(errs) != 0 {
		t.Fatalf("expected a clean program; got: %s", strings.Join(errs, "\n"))
	}
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "x-x-x" {
		t.Errorf("got %q; want %q", got, "x-x-x")
	}
}

// --- The byte-buffer accumulation ------------------------------------------------------
//
// `join` builds its result as a `[]u8` and decodes once at the end, rather than `++`-ing a
// string per part — linear instead of quadratic (the doc comment carries the measurement).
// That moves the risk from arithmetic to encoding: the bytes are now assembled by hand, so
// what has to be pinned is that they come back out as the same *text*, and that the rune
// count survives, since `decode_utf8` derives it from the bytes rather than being told.

// A multi-byte separator and multi-byte parts. Concatenating encoded UTF-8 is valid UTF-8,
// but only if every part's bytes go in whole and in order — a truncated or interleaved
// push shows up here as mojibake rather than as a length that happens to match.
func TestExec_JoinMultiByte(t *testing.T) {
	t.Parallel()
	src := `
let main = () -> void => {
  let parts: []string = ["héllo", "wörld", "日本語"];
  println(parts.join(" — "));
}
`
	want := "héllo — wörld — 日本語"
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != want {
		t.Errorf("got %q; want %q", got, want)
	}
}

// **The rune count, not the byte count.** `len()` is rune-indexed, and the joined string's
// count comes from `decode_utf8` counting non-continuation bytes rather than from anything
// that tracked it. Ten runes across three multi-byte parts and two separators; the byte
// length is larger, and a `len()` reporting it would break every caller that indexes.
func TestExec_JoinMultiByteLength(t *testing.T) {
	t.Parallel()
	src := `
let main = () -> void => {
  let parts: []string = ["é", "ö", "日"];
  let joined = parts.join("--");
  println("${joined.len()} ${joined[0]} ${joined[3]} ${joined[6]}");
}
`
	// "é--ö--日" is 7 runes; bytes would be 11.
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "7 é ö 日" {
		t.Errorf("got %q; want %q", got, "7 é ö 日")
	}
}

// The default empty separator through the byte path: the separator's encoding is hoisted
// out of the loop, and an empty one has to contribute nothing rather than a stray byte.
func TestExec_JoinEmptySeparatorMultiByte(t *testing.T) {
	t.Parallel()
	src := `
let main = () -> void => {
  let parts: []string = ["日", "本", "語"];
  println(parts.join());
}
`
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "日本語" {
		t.Errorf("got %q; want %q", got, "日本語")
	}
}

// Past the buffer's first few doublings, which is where a growth bug stops being invisible:
// 300 parts of 4 runes plus separators is ~1500 bytes, so the `[]u8` reallocates about nine
// times. Checked by length and by both ends, since a lost realloc shows up as a truncation
// and a mis-stored one as garbage at the seam.
func TestExec_JoinAcrossManyReallocations(t *testing.T) {
	t.Parallel()
	src := `
let main = () -> void => {
  var parts: []string = []
  for i in 0..<300 { parts.push("ab${i % 10}") }
  let joined = parts.join(",")
  println("${joined.len()} ${joined.slice(0, 3)} ${joined.slice(joined.len() - 3, joined.len())}")
}
`
	// 300 parts of 3 runes ("ab" + one digit) + 299 separators = 900 + 299 = 1199.
	// The first part is "ab0"; the last is "ab9" (299 % 10 == 9).
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "1199 ab0 ab9" {
		t.Errorf("got %q; want %q", got, "1199 ab0 ab9")
	}
}

// One part returns without touching the buffer at all — the early exit that keeps a
// one-row frame from paying an encode and a decode to arrive back at the string it was
// handed. Multi-byte, so an accidental round trip through the bytes would still be visible
// as a wrong `len()` if it also lost the rune count.
func TestExec_JoinSingleElementIsReturnedWhole(t *testing.T) {
	t.Parallel()
	src := `
let main = () -> void => {
  let one: []string = ["héllo"];
  let joined = one.join(", ");
  println("${joined} ${joined.len()}");
}
`
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "héllo 5" {
		t.Errorf("got %q; want %q", got, "héllo 5")
	}
}

// The Show bound through the byte path: a non-string element is rendered and *then*
// encoded, so `show` has to run before the bytes are taken rather than the element being
// encoded as whatever it is.
func TestExec_JoinNonStringElementsThroughBytes(t *testing.T) {
	t.Parallel()
	src := `
let main = () -> void => {
  let nums: []i64 = [10, 20, 30, 40];
  println(nums.join(" · "));
}
`
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "10 · 20 · 30 · 40" {
		t.Errorf("got %q; want %q", got, "10 · 20 · 30 · 40")
	}
}

// `join` reserves the separators' bytes up front (08/30) — the one part of the result whose
// size is known before any element is evaluated. It is a floor and not a guess, so the only
// way it can be wrong is arithmetically: counting the separator in **runes** would
// under-reserve on a multi-byte one (harmless, just growth) and, more to the point, would
// mean the two lengths had been confused somewhere that matters more.
//
// This joins enough parts to cross several reallocations with a three-byte separator, and
// asserts the rune and byte lengths separately.
func TestExec_JoinReservesSeparatorBytes(t *testing.T) {
	t.Parallel()
	src := `
module main
let main = () -> void => {
  var parts: []string = []
  for i in 0..<400 { parts.push("ab") }
  let joined = parts.join("—")          // U+2014, three bytes and one rune
  print("${joined.len()} ${joined.byte_len()} ${joined.slice(0, 3)}")
}
`
	// 400 parts of 2 runes plus 399 separators of 1 rune = 1199 runes.
	// In bytes: 800 + 399*3 = 1997.
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "1199 1997 ab—" {
		t.Errorf("join with a multi-byte separator = %q; want %q", got, "1199 1997 ab—")
	}
}

// The floor is computed as `len - 1`, so it must sit *after* the early returns: on an empty
// or single-element array that would be a negative reserve, which traps. The cases are
// already covered by TestExec_Join, and this states why they matter now.
func TestExec_JoinShortArraysDoNotReserveNegatively(t *testing.T) {
	t.Parallel()
	src := `
module main
let main = () -> void => {
  let none: []string = []
  let one: []string = ["solo"]
  print("[${none.join(", ")}][${one.join(", ")}][${["a", "b"].join("")}]")
}
`
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "[][solo][ab]" {
		t.Errorf("short joins = %q; want %q", got, "[][solo][ab]")
	}
}
