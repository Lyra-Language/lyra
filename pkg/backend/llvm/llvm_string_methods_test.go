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

// starts_with / ends_with, ordinary Lyra in the prelude over the same rune indexing
// `trim` uses, so this exercises the shipped source rather than a pasted copy.
//
// The cases are chosen around the one that was wrong: `ends_with` was written as
// `starts_with` with the loop running backwards, indexing `self[i]` against
// `other[i]`, which is a *prefix* test whichever direction it runs. So
// `"hello".ends_with("lo")` was false and `"hello".ends_with("he")` was true — each
// answering the question the other one asked, which is why a test comparing only
// against one of them would have passed. Every row below pairs a prefix with a
// suffix of the same string for that reason.
func TestExec_StartsWithEndsWith(t *testing.T) {
	t.Parallel()
	const src = `
module main
let show = (b: bool) -> string => if b { "T" } else { "F" }
let main = () -> void => {
  let s = "hello";
  // prefix: he hello lo x ""     suffix: lo hello he x ""
  println("${show(s.starts_with("he"))}${show(s.starts_with("hello"))}${show(s.starts_with("lo"))}${show(s.starts_with("x"))}${show(s.starts_with(""))}");
  println("${show(s.ends_with("lo"))}${show(s.ends_with("hello"))}${show(s.ends_with("he"))}${show(s.ends_with("x"))}${show(s.ends_with(""))}");
  // an empty receiver, and a needle longer than the haystack
  println("${show("".starts_with(""))}${show("".ends_with(""))}${show("".starts_with("x"))}${show("".ends_with("x"))}${show("hi".starts_with("hiya"))}${show("hi".ends_with("hiya"))}");
  // overlapping suffixes, where an off-by-one in the offset shows up
  println("${show("banana".ends_with("na"))}${show("banana".ends_with("ana"))}${show("banana".ends_with("nan"))}");
}
`
	want := "TTFFT\nTTFFT\nTTFFFF\nTTF"
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != want {
		t.Errorf("starts_with/ends_with results:\n%s\nwant:\n%s", got, want)
	}
}

// Multi-byte input, which is the case rune indexing exists for. A byte-offset
// implementation would answer these wrongly rather than crashing: `"日本語"` is nine
// bytes and three runes, so a suffix of length 2 starts at rune 1 and byte 3.
func TestExec_StartsWithEndsWithNonASCII(t *testing.T) {
	t.Parallel()
	const src = `
module main
let show = (b: bool) -> string => if b { "T" } else { "F" }
let main = () -> void => {
  println("${show("héllo".starts_with("hé"))}${show("héllo".ends_with("llo"))}${show("héllo".ends_with("é"))}");
  println("${show("naïve".ends_with("ïve"))}${show("日本語".starts_with("日本"))}${show("日本語".ends_with("本語"))}${show("日本語".ends_with("日語"))}");
}
`
	want := "TTF\nTTTF"
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != want {
		t.Errorf("non-ASCII prefix/suffix results:\n%s\nwant:\n%s", got, want)
	}
}

// Both are `pure noalloc`, unlike the trim family beside them. That is a real
// difference rather than a spare annotation: they compare runes in place and never
// reach `slice`, so a `noalloc` caller — the code that most wants a cheap prefix
// test — can use them. Pinned because the bound is invisible until something
// silently starts allocating.
func TestCheck_StartsWithIsNoalloc(t *testing.T) {
	t.Parallel()
	const src = `
module main
let is_flag = pure noalloc (s: string) -> bool => s.starts_with("--") || s.ends_with("!")
let main = () -> void => { println("${is_flag("--x")}") }
`
	if diags := checkWithPrelude(t, src); len(diags) != 0 {
		t.Errorf("starts_with/ends_with allocate nothing, so noalloc must accept them; got: %v", diags)
	}
}

// `byte_len()` — the representation's length, against `len()`'s rune count. The two
// agree only on ASCII, which is exactly why the unit is in the name.
func TestExec_StringByteLen(t *testing.T) {
	t.Parallel()
	const src = `
module main
let main = () -> void => {
  println("${"hello".byte_len()} ${"hello".len()}");
  println("${"héllo".byte_len()} ${"héllo".len()}");
  println("${"日本語".byte_len()} ${"日本語".len()}");
  println("${"".byte_len()} ${"".len()}");
}
`
	want := "5 5\n6 5\n9 3\n0 0"
	out, _ := buildAndRunCapture(t, src)
	if got := strings.TrimSpace(out); got != want {
		t.Errorf("byte_len/len:\n%s\nwant:\n%s", got, want)
	}
}

// `compare_bytes_at(offset, other)` is **total**: every out-of-range offset and every
// short range folds into a select rather than a trap or a branch, so there is no input
// the prelude has to guard against. The cases below are the ones that decide whether
// the clamping is right, printed as signs since only the sign is specified.
//
// Two of them are the rule that is easy to guess backwards, and both were written
// wrongly the first time they were tested by hand: a byte **mismatch decides before a
// shortfall** (memcmp's order), so a short range whose first byte is greater answers
// positive, and short-sorts-first settles only a range that matched as far as it went.
func TestExec_StringCompareBytesAtIsTotal(t *testing.T) {
	t.Parallel()
	const src = `
module main
let sign = (n: i64) -> string => if n < 0 { "-" } else { if n > 0 { "+" } else { "0" } }
let main = () -> void => {
  let s = "hello";
  // in range: prefix, whole, empty needle, suffix at an offset, the empty tail
  println("${sign(s.compare_bytes_at(0, "he"))}${sign(s.compare_bytes_at(0, "hello"))}${sign(s.compare_bytes_at(0, ""))}${sign(s.compare_bytes_at(3, "lo"))}${sign(s.compare_bytes_at(5, ""))}");
  // mismatch decides first ("el" vs "he"; "o" vs "lo"), then shortfall ("o" vs "ox")
  println("${sign(s.compare_bytes_at(1, "he"))}${sign(s.compare_bytes_at(4, "lo"))}${sign(s.compare_bytes_at(4, "ox"))}");
  // out of range every way: needle too long, offset past the end, negative offset
  println("${sign(s.compare_bytes_at(0, "helloo"))}${sign(s.compare_bytes_at(6, ""))}${sign(s.compare_bytes_at(99, "x"))}${sign(s.compare_bytes_at(-1, "h"))}${sign(s.compare_bytes_at(-99, "h"))}");
  // an empty receiver, and multi-byte offsets ("日本語" is 9 bytes; "本語" starts at 3)
  println("${sign("".compare_bytes_at(0, ""))}${sign("".compare_bytes_at(0, "x"))}${sign("日本語".compare_bytes_at(3, "本語"))}${sign("日本語".compare_bytes_at(0, "日本"))}");
}
`
	want := "00000\n-+-\n-----\n0-00"
	out, _ := buildAndRunCapture(t, src)
	if got := strings.TrimSpace(out); got != want {
		t.Errorf("compare_bytes_at:\n%s\nwant:\n%s", got, want)
	}
}

// Both new builtins allocate nothing and decode nothing, so `pure noalloc` accepts
// them — which is what lets the prelude's prefix tests keep that bound while being
// one memcmp each. `slice` is the string builtin that does allocate, and the test
// above pins that it is still refused.
func TestCheck_ByteBuiltinsAreNoalloc(t *testing.T) {
	t.Parallel()
	const src = `
module main
let at_end = pure noalloc (s: string, p: string) -> bool =>
  s.compare_bytes_at(s.byte_len() - p.byte_len(), p) == 0
let main = () -> void => { println("${at_end("ab", "b")}") }
`
	if diags := checkWithPrelude(t, src); len(diags) != 0 {
		t.Errorf("byte_len/compare_bytes_at allocate nothing; noalloc must accept them; got: %v", diags)
	}
}

// `index` / `contains`, ordinary Lyra in the prelude over `compare_bytes_at`.
//
// **The offsets it returns are rune indices**, so the answer can be handed straight to
// `slice`; the scan underneath is byte-level, reconciled by carrying a byte cursor
// alongside the rune counter rather than converting afterwards. The multi-byte rows are
// what separate the two: in "日本語" the last rune is at index 2 and byte 6, so a
// byte-leaking implementation answers 6.
//
// The empty-needle rows matter more than they look. `index("")` is the position itself,
// including at the very end (`s.index("", s.len())` is `Some(len)`, checked after the
// loop because a `for-in` does not visit that position) — which is what will make a
// `split` on an empty separator terminate rather than run off the end.
func TestExec_StringIndexAndContains(t *testing.T) {
	t.Parallel()
	const src = `
module main
let at = (s: string, n: string, o: i64) -> i64 => s.index(n, o).unwrap_or(-1)
let main = () -> void => {
  // found at 2 / 0 / 4, absent, needle longer than haystack
  println("${at("hello", "ll", 0)} ${at("hello", "h", 0)} ${at("hello", "o", 0)} ${at("hello", "z", 0)} ${at("hello", "hellooo", 0)}");
  // the offset, and overlapping occurrences: "banana" has "na" at 2 and 4
  println("${at("banana", "na", 0)} ${at("banana", "na", 3)} ${at("banana", "na", 5)} ${at("banana", "ana", 2)}");
  // empty needle at a position, at the end, past the end; empty haystack; negative offset
  println("${at("hello", "", 0)} ${at("hello", "", 5)} ${at("hello", "", 6)} ${at("", "", 0)} ${at("", "x", 0)} ${at("hello", "l", -1)}");
  // rune indices, not byte offsets
  println("${at("héllo", "llo", 0)} ${at("héllo", "é", 0)} ${at("héllo", "o", 0)} ${at("日本語", "語", 0)} ${at("日本語", "本語", 0)} ${at("日本語", "日語", 0)}");
  // the answer feeds straight into slice, which is the reason it is a rune index
  let s = "héllo wörld";
  println("[${s.slice(s.index("wörld").unwrap_or(0), s.len())}]");
  println("${"hello".contains("ell")} ${"hello".contains("xyz")} ${"日本語".contains("本")}");
}
`
	want := "2 0 4 -1 -1\n2 4 -1 3\n0 5 -1 0 -1 -1\n2 1 4 2 1 -1\n[wörld]\ntrue false true"
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != want {
		t.Errorf("index/contains:\n%s\nwant:\n%s", got, want)
	}
}

// The prelude's `index` is written with guard-clause `return`s inside an `if` and a
// `for-in` — the idiom that did not lower until the nested-return fix (08/08), and the
// reason `parse_i64` beside it is one long tail if/else instead. Pinned here as well as
// in the typechecker tests because this is the path that actually failed: the front end
// passed and the backend died with `no type recorded for data constructor "None"`.
func TestExec_EarlyReturnOfAConstructorLowers(t *testing.T) {
	t.Parallel()
	const src = `
module main
let first_over = (xs: [4]i64, limit: i64) -> Maybe<i64> => {
  if limit < 0 { return None }
  for var i = 0; i < 4; i+=1 {
    if xs[i] > limit { return Some(xs[i]) }
  }
  None
}
let main = () -> void => {
  let xs = [3, 9, 4, 12];
  println("${first_over(xs, 5).unwrap_or(-1)} ${first_over(xs, 20).unwrap_or(-1)} ${first_over(xs, -1).unwrap_or(-1)}");
}
`
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "9 -1 -1" {
		t.Errorf("early return of a constructor: got %q, want \"9 -1 -1\"", got)
	}
}

// A negative string index counts from the end, `s[-1]` being the last rune — the rule
// arrays already had, arriving for strings on 08/08.
//
// The multi-byte rows are the ones that matter: a byte-offset implementation would
// answer with a continuation byte rather than a rune, so "日本語"[-1] is the test that
// the backward walk lands on a lead byte. It skips continuation bytes without decoding,
// which is well-defined only because UTF-8 is self-synchronizing.
func TestExec_NegativeStringIndex(t *testing.T) {
	t.Parallel()
	const src = `
module main
let main = () -> void => {
  let s = "héllo";
  println("${s[0]}${s[1]}${s[4]}|${s[-1]}${s[-2]}${s[-5]}");
  let j = "日本語";
  println("${j[-1]}${j[-2]}${j[-3]}|${j[0]}${j[2]}");
}
`
	want := "héo|olh\n語本日|日語"
	out, _ := buildAndRunCapture(t, src)
	if got := strings.TrimSpace(out); got != want {
		t.Errorf("negative string index:\n%s\nwant:\n%s", got, want)
	}
}

// Out of range in *either* direction still traps, and with the string trap rather than
// the array one. `s[-n]` is the first rune and `s[-n-1]` is past the front; the end
// position `s[n]` is not an index, which is what allowEnd=false buys (slice, which does
// admit it, is the other caller).
func TestExec_NegativeStringIndexOutOfRangeTraps(t *testing.T) {
	t.Parallel()
	for _, expr := range []string{"s[5]", "s[-6]", "s[99]", "s[-99]"} {
		t.Run(expr, func(t *testing.T) {
			t.Parallel()
			src := "\nmodule main\nlet main = () -> void => {\n  let s = \"héllo\";\n  println(\"${" + expr + "}\");\n}\n"
			stderr, code := buildAndRunPanic(t, src)
			if code != trapExitCode {
				t.Errorf("%s exited %d; want %d (the trap)", expr, code, trapExitCode)
			}
			if !strings.Contains(stderr, "string index out of bounds") {
				t.Errorf("%s wrote %q; want the string-index trap", expr, stderr)
			}
		})
	}
}

// Negative `slice` bounds, including a **mixed** range like `slice(1, -1)` — which is
// the case that pins the ordering test being applied to the resolved byte offsets
// rather than to the written bounds. Compared as written, `1 > -1` would trap on a
// perfectly ordinary interval.
//
// `slice(-n, -n)` is the empty string, not a trap: an empty range is what every loop
// terminates on, and it is only `start > end` that is a caller bug.
func TestExec_NegativeStringSliceBounds(t *testing.T) {
	t.Parallel()
	const src = `
module main
let main = () -> void => {
  let s = "héllo";
  println("[${s.slice(0, 5)}][${s.slice(1, 3)}][${s.slice(0, 0)}][${s.slice(5, 5)}]");
  println("[${s.slice(1, -1)}][${s.slice(-3, -1)}][${s.slice(-5, 2)}][${s.slice(-1, 5)}][${s.slice(-5, -5)}]");
  let j = "日本語です";
  println("[${j.slice(-2, 5)}][${j.slice(0, -2)}][${j.slice(-3, -1)}]");
}
`
	want := "[héllo][él][][]\n[éll][ll][hé][o][]\n[です][日本語][語で]"
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != want {
		t.Errorf("negative slice bounds:\n%s\nwant:\n%s", got, want)
	}
}

// An inverted range traps whichever way it is spelled, and so does a bound past either
// end. `slice(-1, -3)` is the negative spelling of `slice(4, 2)`, and must not quietly
// yield "" — the empty string it would produce is indistinguishable from a correct
// empty slice.
func TestExec_NegativeStringSliceOutOfRangeTraps(t *testing.T) {
	t.Parallel()
	for _, expr := range []string{"s.slice(3, 1)", "s.slice(-1, -3)", "s.slice(0, 6)", "s.slice(-6, 2)", "s.slice(6, 6)"} {
		t.Run(expr, func(t *testing.T) {
			t.Parallel()
			src := "\nmodule main\nlet main = () -> void => {\n  let s = \"héllo\";\n  println(\"[${" + expr + "}]\");\n}\n"
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

// `s.byte_offset(i)` — the rune→byte conversion, which the language has no other way to
// perform. The multi-byte rows are the whole point: in "héllo" rune 2 is byte 3, so an
// implementation that confused the two would pass on ASCII and fail here.
//
// **The end position is `Some`, not `None`** (rune count → byte length), which is
// slice's rule rather than indexing's: this converts *bounds*, and `s.slice(a, n)` is an
// ordinary slice, so `n` must have an answer. The asymmetry with `s[n]`, which traps, is
// deliberate. A negative index counts from the end like everywhere else.
func TestExec_StringByteOffset(t *testing.T) {
	t.Parallel()
	const src = `
module main
let off = (s: string, i: i64) -> i64 => s.byte_offset(i).unwrap_or(-1)
let main = () -> void => {
  let s = "héllo";
  // runes h é l l o start at bytes 0 1 3 4 5; byte_len is 6, so rune 5 is the end
  // position and rune 6 does not exist. Note the row skips rune 3.
  println("${off(s, 0)} ${off(s, 1)} ${off(s, 2)} ${off(s, 4)} ${off(s, 5)} ${off(s, 6)}");
  println("${off(s, -1)} ${off(s, -5)} ${off(s, -6)}");
  let j = "日本語";
  println("${off(j, 1)} ${off(j, 3)} ${off(j, -1)} ${off("", 0)} ${off("", 1)}");
}
`
	want := "0 1 3 5 6 -1\n5 0 -1\n3 9 6 0 -1"
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != want {
		t.Errorf("byte_offset:\n%s\nwant:\n%s", got, want)
	}
}

// The composition byte_offset exists for: "does `sep` occur at rune i", allocation-free.
//
// It reads without a `match` only because `compare_bytes_at` is **total** — `unwrap_or(-1)`
// hands it an offset it already answers negative for. That is the payoff for having made
// it total rather than trapping, and this test is what pins the two staying compatible:
// if either grew a trap or a different out-of-range answer, the idiom would break here
// rather than in whatever prelude function next reaches for it.
//
// `pure noalloc` on the helper is part of the assertion. A prefix test that allocated
// could not be used from the code that most wants one.
func TestExec_ByteOffsetComposesWithCompareBytesAt(t *testing.T) {
	t.Parallel()
	const src = `
module main
let occurs_at = pure noalloc (self: string, sep: string, i: i64) -> bool =>
  self.compare_bytes_at(self.byte_offset(i).unwrap_or(-1), sep) == 0
let show = (b: bool) -> string => if b { "T" } else { "F" }
let main = () -> void => {
  let t = "a,bb,c";
  // separators sit at runes 1 and 4; 0 and 5 are not, and 99 is off the end
  println("${show(occurs_at(t, ",", 0))}${show(occurs_at(t, ",", 1))}${show(occurs_at(t, ",", 4))}${show(occurs_at(t, ",", 5))}${show(occurs_at(t, ",", 99))}");
  // a multi-rune needle at a multi-byte offset, and the same needle one rune early
  println("${show(occurs_at("héllo", "llo", 2))}${show(occurs_at("héllo", "llo", 1))}${show(occurs_at("héllo", "é", 1))}");
}
`
	want := "FTTFF\nTFT"
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != want {
		t.Errorf("byte_offset + compare_bytes_at:\n%s\nwant:\n%s", got, want)
	}
}

// `index_rune` / `contains_rune` — the single-code-point search, a separate name because
// `index(self: string, needle: string)` and a rune-needle version share the receiver head
// `string`, and receiver-keyed overloading requires the heads to differ (Lyra has no
// argument-type overloading, decided 08/04).
//
// It is not a special case of `index`: `for c in self` already decodes one rune per step,
// so the needle is compared against what the walk produces — no memcmp, no byte cursor.
// Routing it through `index` as `self.index("${needle}")` would allocate a string per call
// to ask about one code point.
//
// The multi-byte rows are the ones that matter: 'é' is at rune 1 of "héllo" and byte 1,
// but '語' is at rune 2 of "日本語" and byte 6, so a byte-leaking implementation passes the
// first and fails the second.
func TestExec_StringIndexRune(t *testing.T) {
	t.Parallel()
	const src = `
module main
let at = (s: string, c: rune, o: i64) -> i64 => s.index_rune(c, o).unwrap_or(-1)
let main = () -> void => {
  // found at 2 / 0 / 4, absent
  println("${at("hello", 'l', 0)} ${at("hello", 'h', 0)} ${at("hello", 'o', 0)} ${at("hello", 'z', 0)}");
  // the offset resumes the scan; past the end, and a negative offset, are None
  println("${at("hello", 'l', 3)} ${at("hello", 'l', 4)} ${at("hello", 'h', 5)} ${at("", 'x', 0)} ${at("hello", 'l', -1)}");
  // rune indices, not byte offsets
  println("${at("héllo", 'é', 0)} ${at("héllo", 'o', 0)} ${at("日本語", '語', 0)} ${at("日本語", '日', 0)}");
}
`
	want := "2 0 4 -1\n3 -1 -1 -1 -1\n1 4 2 0"
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != want {
		t.Errorf("index_rune:\n%s\nwant:\n%s", got, want)
	}
}

// `contains_rune` — the containment half, `index_rune(...).is_some()`.
//
// The rows that earn their place are the last two. UTF-8 encodes 'â' (U+00E2) as C3 A2 and
// '€' (U+20AC) as E2 82 AC, so the *code point* 0xE2 is byte-equal to '€'s leading byte;
// likewise '¬' (U+00AC) against '€'s trailing AC. An implementation that compared a rune's
// numeric value against the haystack's raw bytes — the obvious "optimization" of a search
// that is already byte-level everywhere else in this file — answers true for both. Comparing
// *decoded* runes is what makes them false, and nothing about the ASCII cases above would
// notice the difference.
func TestExec_StringContainsRune(t *testing.T) {
	t.Parallel()
	const src = `
module main
let show = (b: bool) -> string => if b { "T" } else { "F" }
let main = () -> void => {
  // first rune, interior, last rune, absent, repeated, empty receiver
  println("${show("hello".contains_rune('h'))}${show("hello".contains_rune('l'))}${show("hello".contains_rune('o'))}${show("hello".contains_rune('z'))}${show("".contains_rune('x'))}");
  // multi-byte haystack and needle, including a needle that is the whole string
  println("${show("héllo".contains_rune('é'))}${show("héllo".contains_rune('z'))}${show("日本語".contains_rune('本'))}${show("日本語".contains_rune('中'))}${show("€".contains_rune('€'))}");
  // byte collisions: 0xE2 is '€'s first byte and 'â's code point; 0xAC is '€'s last byte
  // and '¬'s code point. Both must be false.
  println("${show("€".contains_rune('â'))}${show("€".contains_rune('¬'))}${show("â".contains_rune('â'))}");
}
`
	want := "TTTFF\nTFTFT\nFFT"
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != want {
		t.Errorf("contains_rune:\n%s\nwant:\n%s", got, want)
	}
}

// contains_rune is defined as index_rune(...).is_some(), and the two must not drift: a
// containment test that disagreed with the search it is built on would be worse than
// either being wrong alone. Checked as a property over a haystack rather than as fixed
// pairs, so it covers the runes actually present as well as ones that are not.
func TestExec_ContainsRuneAgreesWithIndexRune(t *testing.T) {
	t.Parallel()
	const src = `
module main
let main = () -> void => {
  let s = "héllo 日本";
  var disagreements = 0;
  var checked = 0;
  // every rune of s, which must all be found
  for c in s {
    checked += 1;
    if s.contains_rune(c) != s.index_rune(c).is_some() { disagreements += 1 }
  }
  // and a handful that are not in it
  for c in "zqx€" {
    checked += 1;
    if s.contains_rune(c) != s.index_rune(c).is_some() { disagreements += 1 }
    if s.contains_rune(c) { disagreements += 1 }
  }
  println("${checked} ${disagreements}");
}
`
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "12 0" {
		t.Errorf("contains_rune vs index_rune: got %q, want \"12 0\"", got)
	}
}

// Both are `pure noalloc`: the walk decodes runes it already has and never calls `slice`,
// so a containment test is usable from the code that most wants one. Pinned because the
// bound is invisible until something silently starts allocating — the shape that let
// `trim` reach `slice` from inside a `noalloc` function.
func TestCheck_RuneSearchIsNoalloc(t *testing.T) {
	t.Parallel()
	const src = `
module main
let is_path = pure noalloc (s: string) -> bool => s.contains_rune('/') || s.index_rune(':').is_some()
let main = () -> void => { println("${is_path("a/b")}") }
`
	if diags := checkWithPrelude(t, src); len(diags) != 0 {
		t.Errorf("index_rune/contains_rune allocate nothing; noalloc must accept them; got: %v", diags)
	}
}
