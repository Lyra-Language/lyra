package llvm

import (
	"strings"
	"testing"
)

// What an assignment evaluates to find its place: `ys[mk().byte_len()] = 7`. The backend lowers
// an index on the path like any other expression, so it needs the annotations any other
// expression gets, and until 11/03 the ownership pass walked only the value and a pointer. A
// temporary in the index leaked, which only the leak check on Linux sees, and an `own` argument
// in it, given no retain, was released twice, which ASan sees everywhere.
//
// Each case is a function `main` calls, not `main` itself: a leaked pointer left in the frame
// of a function still running is reachable to LeakSanitizer, and `ys[mk().byte_len()] = 7`
// written in `main` passed with the leak in place.
func TestExec_AssignmentPlaceOperands(t *testing.T) {
	t.Parallel()
	const lib = `
struct P { name: string, n: i64, xs: []i64 }
struct Fixed { xs: [5]i64, n: i64 }
var queue = "q" ++ "ueue"
var globals = [0, 0, 0, 0, 0, 0]
let mk = () -> string => "mk" ++ "!"
let count = (s: string) -> i64 => s.byte_len()
let consume = (s: own string) -> i64 => s.byte_len()
let apply = (f: () -> i64) -> i64 => f()
let swallow = (xs: own []i64) -> i64 => xs.len() - 3
let swallow_all = (xss: own [][]i64) -> i64 => xss[0].len() - 3
let swallow_p = (p: own P) -> string => p.name ++ "!"
let maybe = (n: i64) -> Maybe<i64> => if n > 1 { None } else { Some(n) }
let numbers = () -> []i64 => [0, 0, 0, 0, 0, 0]
let names = () -> []string => ["a" ++ "", "b" ++ "", "c" ++ "", "d" ++ "", "e" ++ ""]
let people = () -> []P => [
  P { name: "p" ++ "0", n: 0, xs: numbers() },
  P { name: "p" ++ "1", n: 0, xs: numbers() },
  P { name: "p" ++ "2", n: 0, xs: numbers() },
  P { name: "p" ++ "3", n: 0, xs: numbers() },
]
`
	const main = `
let main = () -> void => { run() }`
	cases := []struct {
		name, src, want string
	}{
		// A temporary, in each spelling of a place.
		{"a call's result", `
let run = () -> void => {
  var ys = numbers()
  ys[mk().byte_len()] = 7
  print("${ys[3]}")
}`, "7"},
		{"a concatenation", `
let run = () -> void => {
  var ys = numbers()
  let s = "a" ++ "b"
  ys[(s ++ "!").byte_len()] = 7
  print("${ys[3]} ${s}")
}`, "7 ab"},
		{"the same index read", `
let run = () -> void => {
  let ys = [0, 1, 2, 3, 4]
  print("${ys[mk().byte_len()]}")
}`, "3"},
		{"a fixed-size array", `
let run = () -> void => {
  var a: [5]i64 = #[0, 0, 0, 0, 0]
  a[mk().byte_len()] = 7
  print("${a[3]}")
}`, "7"},
		{"an element's field", `
let run = () -> void => {
  var ps = people()
  ps[mk().byte_len()].n = 7
  print("${ps[3].n}")
}`, "7"},
		{"every index on the path", `
let run = () -> void => {
  var ps = people()
  ps[mk().byte_len()].xs[(mk() ++ "?").byte_len()] = 7
  print("${ps[3].xs[4]}")
}`, "7"},
		{"an element of an element", `
let run = () -> void => {
  var grid = [numbers(), numbers(), numbers(), numbers()]
  grid[mk().byte_len()][(mk() ++ "?").byte_len()] = 7
  print("${grid[3][4]}")
}`, "7"},
		{"an element's tuple position", `
let run = () -> void => {
  var ts = [(0, "a"), (0, "b"), (0, "c"), (0, "d")]
  ts[mk().byte_len()].0 = 7
  print("${ts[3].0}")
}`, "7"},
		{"a global's element", `
let run = () -> void => {
  globals[mk().byte_len()] = 7
  print("${globals[3]}")
}`, "7"},
		{"a tuple assignment's place", `
let run = () -> void => {
  var ys = numbers()
  (ys[mk().byte_len()], ys[0]) = (7, 1)
  print("${ys[3]} ${ys[0]}")
}`, "7 1"},
		{"an aggregate in the index", `
let run = () -> void => {
  var ys = numbers()
  ys[["a" ++ "", "b" ++ "", "c" ++ ""].len()] = 7
  print("${ys[3]}")
}`, "7"},
		{"a branch in the index", `
let run = () -> void => {
  var ys = numbers()
  ys[if ys[0] == 0 { mk().byte_len() } else { (mk() ++ "?").byte_len() }] = 7
  print("${ys[3]}")
}`, "7"},

		// Where the statement is.
		{"in a branch", `
let run = () -> void => {
  var ys = numbers()
  if ys[0] == 0 { ys[mk().byte_len()] = 7 }
  print("${ys[3]}")
}`, "7"},
		{"in a loop", `
let run = () -> void => {
  var ys = numbers()
  for i in 0..<3 { ys[(mk() ++ "${i}").byte_len() - i] = i }
  print("${ys[4]} ${ys[3]} ${ys[2]}")
}`, "0 1 2"},
		{"in a closure", `
let run = () -> void => {
  let f = () -> i64 => {
    var ys = numbers()
    ys[mk().byte_len()] = 7
    ys[3]
  }
  print("${f()}")
}`, "7"},
		{"in a generic body", `
let put<t> = (x: t, show: (t) -> string) -> i64 => {
  var ys = numbers()
  ys[show(x).byte_len()] = 7
  ys[3]
}
let run = () -> void => {
  print("${put("abc" ++ "", (s) => s ++ "")}")
}`, "7"},
		{"beside a value that leaves", `
let attempt = (n: i64) -> Maybe<i64> => {
  var ys = numbers()
  ys[mk().byte_len()] = maybe(n)?
  Some(ys[3])
}
let run = () -> void => {
  print("${attempt(1) ?? -1} ${attempt(2) ?? -1}")
}`, "1 -1"},

		// An `own` argument, which is a retain or a move and not only a release.
		{"own: a temporary", `
let run = () -> void => {
  var ys = numbers()
  ys[consume(mk())] = 7
  print("${ys[3]}")
}`, "7"},
		{"own: a binding at its last use", `
let run = () -> void => {
  var ys = numbers()
  let s = mk()
  ys[consume(s)] = 7
  print("${ys[3]}")
}`, "7"},
		{"own: a field", `
let run = () -> void => {
  var ys = numbers()
  let p = P { name: "ab" ++ "c", n: 0, xs: [] }
  ys[consume(p.name)] = 7
  print("${ys[3]} ${p.name}")
}`, "7 abc"},
		{"own: an element's field through the written array", `
let run = () -> void => {
  var ps = people()
  ps[consume(ps[0].name)].n = 7
  print("${ps[2].n} ${ps[0].name}")
}`, "7 p0"},

		// Last uses: the walk makes an index's read of a binding a use the table can see.
		{"last use: an integer", `
let run = () -> void => {
  var ys = numbers()
  let n = 3
  ys[n] = 7
  print("${ys[3]}")
}`, "7"},
		{"last use: a borrowed binding", `
let run = () -> void => {
  var ys = numbers()
  let s = mk()
  ys[count(s)] = 7
  print("${ys[3]}")
}`, "7"},
		{"last use: a receiver", `
let run = () -> void => {
  var ys = numbers()
  let s = mk()
  ys[s.byte_len()] = 7
  print("${ys[3]}")
}`, "7"},
		{"last use: the written array itself", `
let total = (xs: []i64) -> i64 => xs.len() - 3
let run = () -> void => {
  var ys = numbers()
  ys[total(ys)] = 7
}`, ""},
		{"last use: in a branch", `
let run = () -> void => {
  var ys = numbers()
  let s = mk()
  if ys[0] == 0 { ys[count(s)] = 7 }
  print("${ys[3]}")
}`, "7"},
		{"last use: in a loop", `
let run = () -> void => {
  var ys = numbers()
  let s = mk()
  for i in 0..<2 { ys[count(s) - i] = i + 1 }
  print("${ys[3]} ${ys[2]}")
}`, "1 2"},

		// A binding in both, which the value moves after the index has read it.
		{"both: borrowed, then moved", `
let run = () -> void => {
  var xs = names()
  let s = mk()
  xs[count(s)] = s
  print("${xs[3]}")
}`, "mk!"},
		{"both: borrowed, then read", `
let run = () -> void => {
  var xs = names()
  let s = mk()
  xs[count(s)] = s ++ "?"
  print("${xs[3]}")
}`, "mk!?"},
		{"both: and used after", `
let run = () -> void => {
  var xs = names()
  let s = mk()
  xs[count(s)] = s
  print("${xs[3]} ${s}")
}`, "mk! mk!"},
		{"both: through a field", `
let run = () -> void => {
  var ps = people()
  let s = mk()
  ps[count(s)].name = s
  print("${ps[3].name}")
}`, "mk!"},

		// The written binding itself, read before the store: its reference can't go to anyone,
		// whichever operand's read looks last, because the store is still to come. lyra-E019
		// refuses a bare `ys[swallow(ys)] = 7`; these are the moves it doesn't track.
		{"moving: the written array, inside an operand", `
let run = () -> void => {
  var ys = numbers()
  ys[swallow_all([ys])] = 7
  print("${ys[3]}")
}`, "7"},
		{"moving: the written array, to a nested function called by its name", `
let run = () -> void => {
  var ys = numbers()
  let f = (xs: own []i64) -> i64 => swallow(xs)
  ys[f(ys)] = 7
  print("${ys[3]}")
}`, "7"},
		{"moving: a struct's own field", `
let run = () -> void => {
  var p = P { name: "ab" ++ "c", n: 0, xs: [] }
  p.name = swallow_p(p)
}`, ""},
		{"moving: a different binding still moves", `
let run = () -> void => {
  var ys = numbers()
  let zs = numbers()
  ys[swallow(zs)] = 7
  print("${ys[3]}")
}`, "7"},

		// A closure in the index is a function of its own, and needs analysing as one.
		{"closure: a temporary in its body", `
let run = () -> void => {
  var ys = numbers()
  ys[apply(() => mk().byte_len())] = 7
  print("${ys[3]}")
}`, "7"},
		{"closure: an environment", `
let run = () -> void => {
  var ys = numbers()
  let s = mk()
  ys[apply(() => count(s))] = 7
  print("${ys[3]} ${s}")
}`, "7 mk!"},
		{"closure: its capture's last use", `
let run = () -> void => {
  var ys = numbers()
  let s = mk()
  ys[apply(() => consume(s))] = 7
  print("${ys[3]}")
}`, "7"},

		// A mutable global read in the index is held for the statement, as anywhere.
		{"a mutable global read", `
let run = () -> void => {
  var ys = numbers()
  ys[count(queue) - 1] = 7
  print("${ys[4]} ${queue}")
}`, "7 queue"},

		// A compound assignment walked its place all along: held to the same.
		{"compound: a temporary", `
let run = () -> void => {
  var ys = numbers()
  ys[mk().byte_len()] += 7
  print("${ys[3]}")
}`, "7"},
		{"compound: an element's field", `
let run = () -> void => {
  var ps = people()
  ps[mk().byte_len()].n += 7
  print("${ps[3].n}")
}`, "7"},
		{"compound: every index", `
let run = () -> void => {
  var ps = people()
  ps[mk().byte_len()].xs[(mk() ++ "?").byte_len()] += 7
  print("${ps[3].xs[4]}")
}`, "7"},
		{"compound: a global's element", `
let run = () -> void => {
  globals[mk().byte_len()] += 7
  print("${globals[3]}")
}`, "7"},
		{"compound: own, at its last use", `
let run = () -> void => {
  var ys = numbers()
  let s = mk()
  ys[consume(s)] += 7
  print("${ys[3]}")
}`, "7"},
		{"compound: a binding in both", `
let run = () -> void => {
  var ys = numbers()
  let s = mk()
  ys[count(s)] += count(s)
  print("${ys[3]}")
}`, "3"},
		{"compound: the written array itself", `
let total = (xs: []i64) -> i64 => xs.len() - 3
let run = () -> void => {
  var ys = numbers()
  ys[total(ys)] += 7
}`, ""},
		{"compound: a closure", `
let run = () -> void => {
  var ys = numbers()
  ys[apply(() => mk().byte_len())] += 7
  print("${ys[3]}")
}`, "7"},

		// Through a pointer, whose operand was walked and whose path past it was not.
		{"pointer: a temporary in the operand", `
let run = () -> void => {
  var ys = numbers()
  unsafe {
    let p = &mut ys[0]
    p.offset(mk().byte_len())^ = 7
  }
  print("${ys[3]}")
}`, "7"},
		{"pointer: an operand's field", `
let run = () -> void => {
  var ps = [Fixed { xs: #[0, 0, 0, 0, 0], n: 0 }, Fixed { xs: #[0, 0, 0, 0, 0], n: 0 }]
  unsafe {
    let p = &mut ps[0]
    p.offset(mk().byte_len() - 2)^.n = 7
  }
  print("${ps[1].n}")
}`, "7"},
		{"pointer: an index past it", `
let run = () -> void => {
  var f = Fixed { xs: #[0, 0, 0, 0, 0], n: 0 }
  unsafe {
    let p = &mut f
    p^.xs[mk().byte_len()] = 7
  }
  print("${f.xs[3]}")
}`, "7"},
		{"pointer: an index on both sides", `
let run = () -> void => {
  var ps = [Fixed { xs: #[0, 0, 0, 0, 0], n: 0 }, Fixed { xs: #[0, 0, 0, 0, 0], n: 0 }]
  unsafe {
    let p = &mut ps[0]
    p.offset(mk().byte_len() - 2)^.xs[(mk() ++ "?").byte_len()] = 7
  }
  print("${ps[1].xs[4]}")
}`, "7"},
		{"pointer: compound", `
let run = () -> void => {
  var f = Fixed { xs: #[0, 0, 0, 0, 0], n: 0 }
  unsafe {
    let p = &mut f
    p^.xs[mk().byte_len()] += 7
  }
  print("${f.xs[3]}")
}`, "7"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := strings.TrimSpace(buildAndRunASanStdout(t, lib+c.src+main)); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}
