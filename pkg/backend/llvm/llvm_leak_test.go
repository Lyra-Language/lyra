package llvm

import (
	"runtime"
	"testing"
)

// asanOptions is the environment every ASan run in this package uses, and it turns
// **leak detection on wherever LeakSanitizer exists**: Linux, which is CI and ./asan.sh.
// Apple's clang has no LeakSanitizer and its ASan runtime refuses `detect_leaks=1`, so on
// macOS a run still checks memory safety and nothing more.
//
// Leaks were off everywhere until 09/13, because the ownership model still leaked in
// known places and a suite failing on those would have buried the faults ASan is there
// for. Turning it on had to wait for the last of those, and waiting cost something: a
// test skipping the helper had been failing CI on leaks for two days while every
// developer-facing run passed.
func asanOptions() string {
	if runtime.GOOS == "linux" {
		return "ASAN_OPTIONS=detect_leaks=1"
	}
	return "ASAN_OPTIONS=detect_leaks=0"
}

// buildAndRunLSanWithPrelude is buildAndRunASanWithPrelude for a test that exists to say a
// shape does not leak, so it skips rather than passing vacuously where leaks cannot be seen.
// A leak exits 1, so a program under test answers something else.
func buildAndRunLSanWithPrelude(t *testing.T, src string) int {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("LeakSanitizer runs on Linux only; ./asan.sh runs this test there")
	}
	return buildAndRunASanWithPrelude(t, src)
}

// Temporaries that were never released, found by LeakSanitizer once CI's Linux runner ran a
// test without the helper that turns it off (09/13). Three families, each with the program
// that found it:
//
//   - **a builtin's owned result read as borrowed** — `program_arg(i)` copies argv[i] into a
//     fresh string, and only `read_line` was listed as owning, so every `program_args()`
//     leaked one string per argument;
//   - **a temporary receiver of a `.`-call** — the ownership pass walked a receiver only when
//     the method consumed it, so `mk().len()` and `xs.join(",").len()` leaked the value the
//     method was called on;
//   - **a literal aggregate in a borrowing position** — an array, repeat, comprehension or
//     struct literal passed to a borrowed parameter owned its box and everything moved into
//     it, and only the tuple literal was marked for release.
func TestExec_TemporariesDoNotLeak(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
		want int
	}{
		{"program_args bound", `let main = () -> u8 => { let a = program_args(); u8(a.len()) + 2 }`, 3},
		{"program_args as a receiver", `let main = () -> u8 => u8(program_args().len()) + 2`, 3},
		{"a call's array as a receiver", `let mk = () -> []i64 => [1, 2, 3]
let main = () -> u8 => u8(mk().len())`, 3},
		{"a call's string as a receiver", `let s = (n: i64) -> string => "n=${n}"
let main = () -> u8 => u8(s(5).len() + s(5).byte_len())`, 6},
		{"a chain of temporaries", `let mk = () -> []string => ["a", "b"]
let main = () -> u8 => u8(mk().join(",").len() + mk().slice(0, 1).len())`, 4},
		{"a field of a temporary as a receiver", `struct H { xs: []i64 }
let mk = () -> H => H { xs: [1, 2, 3] }
let main = () -> u8 => u8(mk().xs.len())`, 3},
		{"a comprehension as a receiver", `let main = () -> u8 => u8([i in 0..<3 | "${i}"].len())`, 3},
		{"literals passed to a borrow", `struct W { xs: []i64 }
let n = (xs: []i64) -> i64 => xs.len()
let w = (v: W) -> i64 => v.xs.len()
let f = (xs: [2]string) -> i64 => xs[0].len()
let main = () -> u8 => u8(n([9, 9]) + w(W { xs: [9] }) + n([i in 0..<3 | i]) + f(#["${12}", "b"]))`, 8},
		{"a std.json read chain", `module main
import std.json.{ parse_json, JsonNull, field, as_text }
let main = () -> u8 => match parse_json("{\"a\": \"xyz\"}") {
  Ok(d) => u8(d.field("a").unwrap_or(JsonNull).as_text().unwrap_or("").len()),
  Err(_) => 2,
}`, 3},
		{"a literal default inside a callee", `let total = (m: Maybe<[]i64>) -> i64 => m.unwrap_or([]).len()
let main = () -> u8 => u8(total(Some([9, 9]))) + 1`, 3},
		// The five that were still failing with leak detection forced on, and what fixing
		// them turned up (09/13).
		{"a trait method's owned result", `struct Tag { n: i64 }
trait Speak { say: (Self) -> string }
impl Speak for Tag { say = (self) => "hi" ++ "!" }
let main = () -> u8 => { let t = Tag { n: 1 }; if t.say() == "hi!" { 3 } else { 1 } }`, 3},
		{"a bound call to a method returning a field", `struct H { s: string }
trait Named { name: (Self) -> string }
impl Named for H { name = (self) => self.s }
let shout<t> where t: Named = (v: t) -> string => v.name() ++ "?"
let main = () -> u8 => { let h = H { s: "a" ++ "b" }; u8(shout(h).len()) }`, 3},
		{"a write through a pointer releases what it overwrote", `struct H { s: string }
let main = () -> u8 => {
  var t: string = "x" ++ "y"
  var h = H { s: "a" ++ "b" }
  unsafe {
    let p = &mut t
    p^ = p^ ++ "!"
    let q = &mut h
    q^.s = "c" ++ "de"
    q^ = H { s: "f" ++ "g" }
  }
  u8(t.len() + h.s.len())
}`, 5},
		{"a swap through pointers", `let main = () -> u8 => {
  var a = "a" ++ "1"
  var b = "b" ++ "22"
  unsafe {
    let pa = &mut a
    let pb = &mut b
    (pa^, pb^) = (pb^, pa^)
  }
  u8(a.len() * 10 + b.len())
}`, 32},
		{"an overwritten aggregate element and field", `struct In { s: string }
struct Out { inner: In, n: i64 }
let main = () -> u8 => {
  var o = Out { inner: In { s: "a" ++ "b" }, n: 1 }
  let keep = o
  o.inner = In { s: "c" ++ "d" ++ "e" }
  var xs: [](string, i64) = [("a" ++ "b", 1)]
  xs[0] = ("e" ++ "fg", 3)
  u8(o.inner.s.len() * 10 + keep.inner.s.len() + xs[0].0.len())
}`, 35},
		{"hashmap churn", `module main
import std.collections.{ HashMap, hashmap_new, insert, replace, remove, len }
let main = () -> u8 => {
  var m: HashMap<string, string> = hashmap_new()
  for i in 0..<40 { m.insert("k" ++ "${i %% 10}", "v" ++ "${i}") }
  let _ = m.remove("k" ++ "3")
  let _ = m.replace("k" ++ "4", "again")
  u8(m.len())
}`, 9},
		{"a yielded temporary, held and inlined", `module main
let names = pure gen () -> Seq<string> => { for i in 0..<3 { yield "name-" ++ "${i}" } }
let main = () -> u8 => {
  var t = 0
  for s in names() { t += s.len() }
  let held = names().map((s) => s ++ "!")
  for s in held { t += s.len() }
  u8(t)
}`, 39},
		{"a sequence argument, stepped and broken out of", `module main
let multiples = pure gen (k: i64) -> Seq<i64> => { var n = k; for { yield n; n += k } }
let pair = pure gen (a: Seq<i64>, b: Seq<i64>) -> Seq<i64> => {
  var xs = a
  var ys = b
  for { match (xs.next(), ys.next()) { (Some(x), Some(y)) => { yield x + y }, _ => { break } } }
}
let main = () -> u8 => {
  var total = 0
  for v in pair(multiples(1), multiples(2)).take(3) { total += v }
  var st = multiples(5).filter((n) => n > 5)
  let first = st.next().unwrap_or(0)
  u8(total + first)
}`, 28},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := buildAndRunLSanWithPrelude(t, c.src); got != c.want {
				t.Errorf("exited %d; want %d (1 is LeakSanitizer reporting a leak)", got, c.want)
			}
		})
	}
}
