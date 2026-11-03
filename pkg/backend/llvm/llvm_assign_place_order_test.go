package llvm

import (
	"strings"
	"testing"
)

// An operand that changes what the place is: `g[shrink()] = 9`, where `shrink` does `g = [1, 2]`.
// The path walk used to load the box, and take the element's address, before the index and the
// value had run, so the store landed in a box the operand had freed, or a buffer it had grown out
// of. The place is found after them now. ASan throughout, because what this gets wrong is memory.
func TestExec_AssignmentFindsItsPlaceAfterItsOperands(t *testing.T) {
	t.Parallel()
	const lib = `
struct Holder { xs: []i64, n: i64 }
struct V { x: i64 }
impl Add for V { (_+_) = (self, other) => V { x: self.x + other.x } }
var g = [0, 0, 0, 0, 0, 0]
var b = Holder { xs: [0, 0, 0, 0, 0, 0], n: 0 }
var grid = [[0, 0, 0, 0, 0, 0], [0]]
var names = ["a" ++ "", "b" ++ ""]
var vs = [V { x: 1 }, V { x: 2 }, V { x: 3 }, V { x: 4 }]
var fixed: [4]i64 = #[0, 0, 0, 0]
var box: shared Holder = Holder { xs: [], n: 0 }
var trail = ""
let shrink = () -> i64 => {
  g = [1, 2]
  0
}
let five = () -> i64 => {
  g = [1, 2]
  5
}
let shrink_field = () -> i64 => {
  b.xs = [1, 2]
  0
}
let shrink_holder = () -> i64 => {
  b = Holder { xs: [1, 2], n: 3 }
  0
}
let shrink_row = () -> i64 => {
  grid[0] = [1, 2]
  0
}
let shrink_grid = () -> i64 => {
  grid = [[1, 2]]
  0
}
let rename = () -> string => {
  names = ["x" ++ "", "y" ++ ""]
  "z" ++ ""
}
let shrink_vs = () -> V => {
  vs = [V { x: 10 }, V { x: 20 }]
  V { x: 5 }
}
let write_fixed = () -> i64 => {
  fixed[1] = 8
  7
}
let replace_box = () -> i64 => {
  box = Holder { xs: [], n: 40 }
  1
}
let grow = (xs: mut []i64) -> i64 => {
  for i in 0..<1000 { xs.push(i) }
  1
}
let renew = (xs: mut []i64) -> i64 => {
  xs = [1, 2]
  1
}
let put = (xs: mut []i64) -> void => {
  xs[0] = grow(xs)
}
let mark = (s: string, n: i64) -> i64 => {
  trail = trail ++ s
  n
}
let bump = () -> i64 => {
  g[1] = 100
  5
}
`
	cases := []struct {
		name, src, want string
	}{
		// A global's box, replaced by each operand.
		{"the index", `g[shrink()] = 9
  print("${g[0]} ${g[1]}")`, "9 2"},
		{"the value", `g[1] = shrink()
  print("${g[0]} ${g[1]}")`, "1 0"},
		{"the value, compound", `g[1] += five()
  print("${g[0]} ${g[1]}")`, "1 5"},
		{"the index, compound", `g[shrink()] += 9
  print("${g[0]} ${g[1]}")`, "10 2"},
		{"both", `g[shrink()] = five()
  print("${g[0]} ${g[1]}")`, "5 2"},

		// Further along a path.
		{"a global struct's field, the value", `b.xs[1] = shrink_field()
  print("${b.xs[0]} ${b.xs[1]}")`, "1 0"},
		{"a global struct's field, the index", `b.xs[shrink_field()] = 9
  print("${b.xs[0]} ${b.xs[1]}")`, "9 2"},
		{"a global struct, replaced whole", `b.xs[1] = shrink_holder()
  print("${b.xs[0]} ${b.xs[1]} ${b.n}")`, "1 0 3"},
		{"a field beside it", `b.n = shrink_holder()
  print("${b.n} ${b.xs[1]}")`, "0 2"},
		{"a row, the value", `grid[0][1] = shrink_row()
  print("${grid[0][0]} ${grid[0][1]}")`, "1 0"},
		{"a row, the index", `grid[0][shrink_row()] = 9
  print("${grid[0][0]} ${grid[0][1]}")`, "9 2"},
		{"the outer array, the value", `grid[0][1] = shrink_grid()
  print("${grid[0][0]} ${grid[0][1]}")`, "1 0"},
		{"an element's field, compound", `b.xs[1] += shrink_field()
  print("${b.xs[0]} ${b.xs[1]}")`, "1 0"},
		{"a shared struct's box", `box.n = replace_box()
  print("${box.n}")`, "1"},

		// What the slot holds owes a release, which must be the one now there.
		{"a managed element", `names[1] = rename()
  print("${names[0]} ${names[1]}")`, "x z"},
		{"a managed element, the old one read", `names[1] = names[0] ++ rename()
  print("${names[0]} ${names[1]}")`, "x az"},

		// A local, which an operand reaches by a block or a `mut` argument.
		{"a block reassigning a local", `var ys = [0, 0, 0]
  ys[1] = {
    ys = [1, 2]
    5
  }
  print("${ys[0]} ${ys[1]}")`, "1 5"},
		{"a mut argument renewing a local, the value", `var ys = [0, 0, 0]
  ys[1] = renew(ys)
  print("${ys[0]} ${ys[1]}")`, "1 1"},
		{"a mut argument renewing a local, the index", `var ys = [0, 0, 0]
  ys[renew(ys)] = 7
  print("${ys[0]} ${ys[1]}")`, "1 7"},
		{"a mut argument growing a local, the value", `var ys = [0, 0, 0]
  ys[1] = grow(ys)
  print("${ys[0]} ${ys[1]} ${ys.len()}")`, "0 1 1003"},
		{"a mut argument growing a local, the index", `var ys = [0, 0, 0]
  ys[grow(ys)] = 7
  print("${ys[1]} ${ys.len()}")`, "7 1003"},
		{"a mut argument growing a local, compound", `var ys = [0, 0, 0]
  ys[1] += grow(ys)
  print("${ys[1]} ${ys.len()}")`, "1 1003"},
		{"a push in a block", `var ys = [0, 0, 0]
  ys[1] = {
    for i in 0..<1000 { ys.push(i) }
    5
  }
  print("${ys[1]} ${ys.len()}")`, "5 1003"},
		{"a parameter, passed on", `var ys = [0, 0, 0]
  put(ys)
  print("${ys[0]} ${ys.len()}")`, "1 1003"},

		// Overloaded: the operator is a call, and the right side is run beside it.
		{"an overloaded operator", `vs[1] += shrink_vs()
  print("${vs[0].x} ${vs[1].x}")`, "10 7"},
		{"an overloaded operator with nothing to write", `vs[1] += V { x: 5 }
  print("${vs[1].x}")`, "7"},

		// Storage the operand can't replace is written where it always was.
		{"a fixed-size global", `fixed[0] = write_fixed()
  print("${fixed[0]} ${fixed[1]}")`, "7 8"},

		// What doesn't change: each operand once, left to right, the read before the right.
		{"operands left to right", `var grid2 = [[0, 0], [0, 0]]
  grid2[mark("a", 1)][mark("b", 0)] = mark("c", 7)
  print("${trail} ${grid2[1][0]}")`, "abc 7"},
		{"compound operands left to right, each once", `var ys = [0, 0, 0]
  ys[mark("a", 1)] += mark("b", 7)
  print("${trail} ${ys[1]}")`, "ab 7"},
		{"compound reads before the right side runs", `g[1] += bump()
  print("${g[1]}")`, "5"},
		{"a tuple assignment's right side first", `(g[0], g[1]) = (shrink(), 7)
  print("${g[0]} ${g[1]}")`, "0 7"},

		// An operand that doesn't come back.
		{"an index that returns", `var ys = [0, 0, 0]
  ys[if ys[0] == 0 { return } else { 0 }] = 7
  print("no")`, ""},
		{"a compound index that returns", `var ys = [0, 0, 0]
  ys[if ys[0] == 0 { return } else { 0 }] += 7
  print("no")`, ""},
		{"a value that returns", `var ys = [0, 0, 0]
  ys[0] = if ys[0] == 0 { return } else { 0 }
  print("no")`, ""},
		{"a compound value that returns", `var ys = [0, 0, 0]
  ys[0] += if ys[0] == 0 { return } else { 0 }
  print("no")`, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			src := lib + "let run = () -> void => {\n  " + c.src + "\n}\nlet main = () -> void => { run() }"
			if got := strings.TrimSpace(buildAndRunASanStdout(t, src)); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

// The bounds check is a question about the place, so it comes with finding it: after the operands,
// against the array the store will write into.
func TestExec_AssignmentChecksBoundsAfterItsOperands(t *testing.T) {
	t.Parallel()
	const lib = `
var g = [0, 0, 0, 0, 0, 0]
let shrink = () -> i64 => {
  g = [1, 2]
  0
}
let boom = () -> i64 => panic("boom")
`
	cases := []struct {
		name, src, want string
	}{
		{"against the new array", `g[5] = shrink()`, "out of bounds"},
		{"compound, against the new array", `g[5] += shrink()`, "out of bounds"},
		{"after the value", `g[9] = boom()`, "boom"},
		{"compound, after the read and so before the value", `g[9] += boom()`, "out of bounds"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			out, code := buildAndRunPanic(t, lib+"let main = () -> void => {\n  "+c.src+"\n}\n")
			if code != 101 {
				t.Fatalf("exited %d; want the trap's 101, output %q", code, out)
			}
			if !strings.Contains(out, c.want) {
				t.Errorf("output = %q; want %q", out, c.want)
			}
		})
	}
}
