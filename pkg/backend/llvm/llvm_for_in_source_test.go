package llvm

import (
	"strings"
	"testing"
)

// A loop's source written while the loop walks it. The walk reads a box it loaded once, and the
// loop variable borrows out of it, so replacing the array or an element frees what they read.
// ASan is the point: a missed hold is a use-after-free, a hold not released on a way out is
// what the leak check on Linux reports, and one released twice is a double free on both.
const forInSourceLib = `
struct H { items: []string }
var trail: []string = []
let mk = (n: i64) -> []string => ["n" ++ "${n}", "m" ++ "${n}"]
let one = (n: i64) -> Maybe<i64> => if n > 1 { None } else { Some(n) }
let walk_ref = gen (xs: ref []string) -> Seq<string> => { for x in xs { yield x } }
let walk_bare = gen (xs: []string) -> Seq<string> => { for x in xs { yield x } }
let index_ref = gen (xs: ref []string) -> Seq<string> => {
  var i = 0
  while i < xs.len() { yield xs[i]; i += 1 }
}
let index_bare = gen (xs: []string) -> Seq<string> => {
  var i = 0
  while i < xs.len() { yield xs[i]; i += 1 }
}
let chars = gen (s: string) -> Seq<rune> => { for r in s { yield r } }
let reset = (xs: mut []string) -> void => { xs = mk(9) }
let swap_for = (xs: mut []string, s: string) -> string => { xs = mk(9); s }
`

func TestExec_ForInSourceWritten(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, src, want string }{
		{"reassigned", `
let main = () -> void => {
  var ys = mk(1)
  var o = ""
  for y in ys { ys = mk(2); o = o ++ y }
  print(o ++ ys[0])
}`, "n1m1n2"},
		{"reassigned, of integers", `
let main = () -> void => {
  var ys = [1, 2, 3]
  var c = 0
  for y in ys { ys = [7]; c += y }
  print("${c} ${ys.len()}")
}`, "6 1"},
		{"reassigned, the loop's two names", `
let main = () -> void => {
  var ys = mk(1)
  var o = ""
  for i, y in ys { ys = mk(i + 2); o = o ++ y }
  print(o ++ ys[0])
}`, "n1m1n3"},
		{"reassigned on one path", `
let main = () -> void => {
  var ys = mk(1)
  var o = ""
  for y in ys { if y == "n1" { ys = mk(2) }; o = o ++ y }
  print(o)
}`, "n1m1"},
		{"a field", `
let main = () -> void => {
  var h = H { items: mk(1) }
  var o = ""
  for y in h.items { h.items = mk(2); o = o ++ y }
  print(o)
}`, "n1m1"},
		{"an element", `
let main = () -> void => {
  var grid = [mk(1), mk(2)]
  var o = ""
  for y in grid[0] { grid[0] = mk(3); o = o ++ y }
  print(o)
}`, "n1m1"},
		{"an element, by a second name for the array", `
let main = () -> void => {
  var grid = [mk(1), mk(2)]
  var also = grid
  var o = ""
  for y in grid[0] { also[0] = mk(3); o = o ++ y }
  print(o)
}`, "n1m1"},
		{"a field of a shared value, by a second name", `
let main = () -> void => {
  var a: shared H = H { items: mk(1) }
  var b = a
  var o = ""
  for y in a.items { b.items = mk(2); o = o ++ y }
  print(o)
}`, "n1m1"},
		{"handed to a call that replaces it", `
let main = () -> void => {
  var ys = mk(1)
  var o = ""
  for y in ys { reset(ys); o = o ++ y }
  print(o)
}`, "n1m1"},
		{"a `mut` parameter", `
let run = (xs: mut []string) -> string => {
  var o = ""
  for x in xs { xs = mk(9); o = o ++ x }
  o
}
let main = () -> void => {
  var ys = mk(1)
  print(run(ys) ++ ys[0])
}`, "n1m1n9"},
		{"a field of a `mut` receiver", `
trait Reset { reset_all: (mut Self) -> string }
impl Reset for H {
  reset_all = (self) => {
    var o = ""
    for x in self.items { self.items = mk(9); o = o ++ x }
    o
  }
}
let main = () -> void => {
  var h = H { items: mk(1) }
  print(h.reset_all())
}`, "n1m1"},
		{"a global, and a global's field", `
var g: []string = ["a" ++ "1", "b" ++ "2"]
var gh: H = H { items: ["c" ++ "3", "d" ++ "4"] }
let main = () -> void => {
  var o = ""
  for y in g { g = mk(2); o = o ++ y }
  for y in gh.items { gh.items = mk(2); o = o ++ y }
  print(o)
}`, "a1b2c3d4"},
		{"pushed to", `
let main = () -> void => {
  var ys = mk(1)
  var o = ""
  for y in ys { ys.push("p" ++ y); o = o ++ y }
  print("${o} ${ys.len()}")
}`, "n1m1 4"},
		{"an element overwritten, the variable read after", `
let main = () -> void => {
  var ys = mk(1)
  var o = ""
  for y in ys { ys[0] = "c" ++ "3"; ys[1] = "d" ++ "4"; o = o ++ y }
  print(o)
}`, "n1d4"},
		{"a row replaced while its own loop walks it", `
let main = () -> void => {
  var grid = [mk(1), mk(2)]
  var o = ""
  for row in grid {
    for y in row { grid[0] = mk(3); grid[1] = mk(4); o = o ++ y }
  }
  print(o)
}`, "n1m1n4m4"},
		{"a fixed-size array of strings", `
let main = () -> void => {
  var xs: [2]string = #["a" ++ "1", "b" ++ "2"]
  var o = ""
  for x in xs { xs = #["c" ++ "3", "d" ++ "4"]; o = o ++ x }
  print(o)
}`, "a1d4"},
		{"a `shared` fixed-size array", `
let main = () -> void => {
  var xs: shared [2]string = #["a" ++ "1", "b" ++ "2"]
  var o = ""
  for x in xs { xs = #["c" ++ "3", "d" ++ "4"]; o = o ++ x }
  print(o)
}`, "a1b2"},
		{"a string", `
let main = () -> void => {
  var s = "ab" ++ "cdef"
  var c = 0
  for r in s { s = "z" ++ "y"; c += 1 }
  print("${c} ${s}")
}`, "6 zy"},
		{"a string's two names", `
let main = () -> void => {
  var s = "ab" ++ "cd"
  var c = 0
  for i, r in s { s = "z" ++ "y"; c += i }
  print("${c}")
}`, "6"},
		{"a string in a field", `
struct N { text: string }
let main = () -> void => {
  var n = N { text: "ab" ++ "cdef" }
  var c = 0
  for r in n.text { n.text = "z" ++ "y"; c += 1 }
  print("${c}")
}`, "6"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := strings.TrimSpace(buildAndRunASanStdout(t, forInSourceLib+c.src)); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

// A walk ends where the array now does: what `clear` released is past its length, and it is not
// read, however the clearing got there. A push adds no iterations.
func TestExec_ForInSourceCleared(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, src, want string }{
		{"cleared by the body", `
let main = () -> void => {
  var ys = mk(1)
  var o = ""
  for y in ys { ys.clear(); o = o ++ y }
  print("${o} ${ys.len()}")
}`, "n1 0"},
		{"cleared and refilled", `
let main = () -> void => {
  var ys = mk(1)
  var o = ""
  for i, y in ys {
    o = o ++ y
    if i == 0 { ys.clear(); ys.push("a" ++ "1"); ys.push("b" ++ "2"); ys.push("c" ++ "3") }
  }
  print(o)
}`, "n1b2"},
		{"cleared to fewer", `
let main = () -> void => {
  var ys = ["a" ++ "1", "b" ++ "2", "c" ++ "3"]
  var o = ""
  for i, y in ys {
    o = o ++ y
    if i == 0 { ys.clear(); ys.push("d" ++ "4"); ys.push("e" ++ "5") }
  }
  print(o)
}`, "a1e5"},
		{"by a second name", `
let main = () -> void => {
  var ys = mk(1)
  var zs = ys
  var o = ""
  for y in ys { o = o ++ y; zs.clear() }
  print(o)
}`, "n1"},
		{"a global, by a call", `
var g: []string = ["a" ++ "1", "b" ++ "2"]
let empty = () -> void => { g.clear() }
let main = () -> void => {
  var o = ""
  for y in g { o = o ++ y; empty() }
  print(o)
}`, "a1"},
		{"a field of a shared value, by a second name", `
let main = () -> void => {
  var a: shared H = H { items: mk(1) }
  var b = a
  var o = ""
  for y in a.items { o = o ++ y; b.items.clear() }
  print(o)
}`, "n1"},
		{"by the consumer of a `gen` by value", `
let main = () -> void => {
  var ys = mk(1)
  var o = ""
  for y in walk_bare(ys) { ys.clear(); o = o ++ y }
  print(o)
}`, "n1"},
		{"by the consumer of a `gen` by `ref`", `
let main = () -> void => {
  var ys = mk(1)
  var o = ""
  for y in walk_ref(ys) { ys.clear(); o = o ++ y }
  print(o)
}`, "n1"},
		{"a `break` after it", `
let main = () -> void => {
  var ys = mk(1)
  var o = ""
  for y in ys { ys.clear(); o = o ++ y; break }
  print(o)
}`, "n1"},
		{"a comprehension's result", `
let drain = (xs: mut []string, s: string) -> string => { xs.clear(); s }
let main = () -> void => {
  var ys = mk(1)
  let r = [y in ys | drain(ys, y)]
  print("${r.join(",")} ${r.len()}")
}`, "n1 1"},
		{"a comprehension's guard", `
let drain = (xs: mut []string, s: string) -> bool => { xs.clear(); s.len() > 0 }
let main = () -> void => {
  var ys = mk(1)
  print([y in ys | drain(ys, y) | y].join(","))
}`, "n1"},
		{"a comprehension's later clause", `
let drain = (xs: mut []string, s: string) -> []string => { xs.clear(); [s, s] }
let main = () -> void => {
  var ys = mk(1)
  print([y in ys, z in drain(ys, y) | z].join(","))
}`, "n1,n1"},
		{"a comprehension, by a second name", `
let drain = (xs: mut []string, s: string) -> string => { let t = s ++ "!"; xs.clear(); t }
let main = () -> void => {
  var ys = mk(1)
  var zs = ys
  print([y in ys | drain(zs, y)].join(","))
}`, "n1!"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := strings.TrimSpace(buildAndRunASanStdout(t, forInSourceLib+c.src)); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

// Every way out of a loop whose source has been replaced: the hold goes on each, once, and so
// does a variable held for the iteration. `walk` is the three spellings of the same walk: the
// loop itself, and a `gen` over the array by reference and by value.
func TestExec_ForInSourceWrittenWaysOut(t *testing.T) {
	t.Parallel()
	walks := []struct{ name, over string }{
		{"a plain loop", "ys"},
		{"a gen by `ref`", "walk_ref(ys)"},
		{"a gen by value", "walk_bare(ys)"},
		{"a gen by `ref`, indexing", "index_ref(ys)"},
		{"a gen by value, indexing", "index_bare(ys)"},
	}
	ways := []struct{ name, src, want string }{
		{"to the end", `
let main = () -> void => {
  var ys = mk(1)
  var o = ""
  for y in WALK { ys = mk(2); o = o ++ y }
  print("${o.len() > 0}")
}`, "true"},
		{"`break`", `
let main = () -> void => {
  var ys = mk(1)
  var o = ""
  for y in WALK { ys = mk(2); o = o ++ y; break }
  print(o)
}`, "n1"},
		{"`continue`", `
let main = () -> void => {
  var ys = mk(1)
  var c = 0
  for y in WALK { ys = mk(2); c += y.len(); continue }
  print("${c}")
}`, "4"},
		{"`return`", `
let run = () -> string => {
  var ys = mk(1)
  for y in WALK { ys = mk(2); return y ++ "!" }
  "none"
}
let main = () -> void => { print(run()) }`, "n1!"},
		{"`?`", `
let run = (n: i64) -> Maybe<i64> => {
  var ys = mk(1)
  var c = 0
  for y in WALK { ys = mk(2); c += y.len() + one(n)? }
  Some(c)
}
let main = () -> void => { print("${run(1) ?? -1} ${run(5) ?? -1}") }`, "6 -1"},
		{"`break` out of an enclosing loop", `
let main = () -> void => {
  var ys = mk(1)
  var o = ""
  outer: for k in 0..<3 {
    for y in WALK { ys = mk(2); o = o ++ y; if k == 0 { break outer } }
  }
  print(o)
}`, "n1"},
		{"a `?` in the loop's source, past another", `
let run = (n: i64) -> Maybe<i64> => {
  var ys = mk(1)
  var c = 0
  for k in 0..<2 {
    for y in WALK { ys = mk(2); c += y.len() }
    c += one(n)?
  }
  Some(c)
}
let main = () -> void => { print("${(run(5) ?? -1) < 0}") }`, "true"},
	}
	for _, w := range walks {
		for _, way := range ways {
			t.Run(w.name+", "+way.name, func(t *testing.T) {
				t.Parallel()
				src := strings.ReplaceAll(way.src, "WALK", w.over)
				if got := strings.TrimSpace(buildAndRunASanStdout(t, forInSourceLib+src)); got != way.want {
					t.Errorf("got %q, want %q", got, way.want)
				}
			})
		}
	}
}

// What else walks an array or string: a `gen` handed a place its consumer writes, a method over
// the receiver, a sequence held as a value, a comprehension.
func TestExec_ForInSourceWrittenElsewhere(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, src, want string }{
		{"a `gen` by `ref` sees the array its place holds now", `
let main = () -> void => {
  var ys = mk(1)
  var o = ""
  for y in index_ref(ys) { ys = mk(2); o = o ++ y }
  print(o)
}`, "n1m2"},
		{"a `gen` by value keeps the array it was lent", `
let main = () -> void => {
  var ys = mk(1)
  var o = ""
  for y in index_bare(ys) { ys = mk(2); o = o ++ y }
  print(o)
}`, "n1m1"},
		{"a `gen` over a string", `
let main = () -> void => {
  var s = "ab" ++ "cdef"
  var c = 0
  for r in chars(s) { s = "z" ++ "y"; c += 1 }
  print("${c}")
}`, "6"},
		{"`.seq()` over the array", `
let main = () -> void => {
  var ys = mk(1)
  var o = ""
  for y in ys.seq() { ys = mk(2); o = o ++ y }
  print(o)
}`, "n1m1"},
		{"a `gen`'s element overwritten by its consumer", `
let main = () -> void => {
  var ys = mk(1)
  var o = ""
  for y in walk_bare(ys) { ys[0] = "c" ++ "3"; ys[1] = "d" ++ "4"; o = o ++ y }
  print(o)
}`, "n1d4"},
		{"a `gen` by `ref`, its element overwritten", `
let main = () -> void => {
  var ys = mk(1)
  var o = ""
  for y in walk_ref(ys) { ys[0] = "c" ++ "3"; ys[1] = "d" ++ "4"; o = o ++ y; if o.len() > 2 { break } }
  print(o)
}`, "n1d4"},
		{"a coroutine pulled past the array being replaced", `
let main = () -> void => {
  var ys = mk(1)
  var s = walk_bare(ys)
  ys = mk(2)
  let a = s.next() ?? "none"
  ys = mk(3)
  let b = s.next() ?? "none"
  print(a ++ b)
}`, "n1m1"},
		{"a coroutine dropped partway", `
let main = () -> void => {
  var ys = mk(1)
  var s = walk_bare(ys)
  ys = mk(2)
  print(s.next() ?? "none")
}`, "n1"},
		{"a consumer's `continue` leaves what its producer holds", `
let two = gen () -> Seq<string> => {
  let s = "a" ++ "b"
  yield s
  yield "c" ++ s
  print(s)
}
let main = () -> void => {
  var c = 0
  for x in two() { c += x.len(); continue }
  print("${c}")
}`, "ab5"},
		{"a comprehension whose result replaces its source", `
let main = () -> void => {
  var ys = mk(1)
  let r = [y in ys | if ys.len() > 0 { ys = mk(4); y } else { y }]
  print(r.join(","))
}`, "n1,m1"},
		{"a comprehension whose result calls what replaces it", `
let main = () -> void => {
  var ys = mk(1)
  let r = [y in ys | swap_for(ys, y)]
  print(r.join(",") ++ ys[0])
}`, "n1,m1n9"},
		{"a comprehension whose guard does", `
let keep = (xs: mut []string, s: string) -> bool => { xs = mk(9); s.len() > 0 }
let main = () -> void => {
  var ys = mk(1)
  print([y in ys | keep(ys, y) | y].join(","))
}`, "n1,m1"},
		{"a comprehension's later clause does", `
let row = (xs: mut []string, s: string) -> []string => { xs = mk(9); [s] }
let main = () -> void => {
  var ys = mk(1)
  print([y in ys, z in row(ys, y) | z].join(","))
}`, "n1,m1"},
		{"a comprehension's `?` past it", `
let run = (n: i64) -> Maybe<[]string> => {
  var ys = mk(1)
  Some([y in ys | if one(n)? > 0 { swap_for(ys, y) } else { y }])
}
let main = () -> void => { print("${(run(1) ?? []).len()} ${(run(5) ?? []).len()}") }`, "2 0"},
		{"a comprehension over an element, by a second name", `
let swap_row = (g: mut [][]string, s: string) -> string => { g[0] = mk(9); s }
let main = () -> void => {
  var grid = [mk(1)]
  var also = grid
  print([y in grid[0] | swap_row(also, y)].join(","))
}`, "n1,m1"},
		{"a comprehension over a `gen`", `
let main = () -> void => {
  var ys = mk(1)
  print([y in walk_bare(ys) | swap_for(ys, y)].join(","))
}`, "n1,m1"},
		{"a comprehension's element overwritten", `
let poke = (xs: mut []string, s: string) -> string => { xs[0] = "c" ++ "3"; xs[1] = "d" ++ "4"; s ++ "!" }
let main = () -> void => {
  var ys = mk(1)
  print([y in ys | poke(ys, y)].join(","))
}`, "n1!,d4!"},
		{"a comprehension over a string", `
let swap_str = (s: mut string, r: rune) -> rune => { s = "q" ++ "r"; r }
let main = () -> void => {
  var s = "ab" ++ "cdef"
  print("${[c in s | swap_str(s, c)].len()}")
}`, "6"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := strings.TrimSpace(buildAndRunASanStdout(t, forInSourceLib+c.src)); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}
