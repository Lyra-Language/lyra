package llvm

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

// buildAndRunASanStdout is buildAndRunASanWithPrelude for a program that answers by printing:
// it fails the test on any non-zero exit, with the sanitizer's report, and returns stdout.
func buildAndRunASanStdout(t *testing.T, src string) string {
	t.Helper()
	ir := instrumentForASan(emitWithPrelude(t, "module main\n"+src))
	cmd := command(compileCached(t, lookClang(t), ir, "-fsanitize=address"))
	cmd.Env = append(os.Environ(), asanOptions())
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	asanRunSlots <- struct{}{}
	defer func() { <-asanRunSlots }()
	if code := exitCode(t, cmd.Run()); code != 0 {
		t.Fatalf("exit %d, stderr:\n%s", code, stderr.String())
	}
	return stdout.String()
}

// A comprehension clause whose source names an earlier clause: `[row in grid, cell in row |
// cell]`. The source is a different array each iteration, so it is lowered inside the loop it
// depends on and the box grows. ASan throughout, because what this can get wrong is memory: a
// source's temporary released at the wrong depth, a push past a stale buffer, a local read
// again after its one use moved it.
func TestExec_ArrayCompDependentClause(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, src, want string
	}{
		{"an array of arrays, an empty row included", `
let main = () -> void => {
  let grid: [][]i64 = [[1, 2], [], [3, 4, 5, 6, 7, 8, 9]]
  print("${[row in grid, cell in row | "${cell}"].join(",")}")
}`, "1,2,3,4,5,6,7,8,9"},
		{"nothing to walk", `
let main = () -> void => {
  let grid: [][]i64 = []
  let empty_rows: [][]i64 = [[], []]
  print("${[row in grid, cell in row | cell].len()} ${[row in empty_rows, cell in row | cell].len()}")
}`, "0 0"},
		{"a chain of three", `
let main = () -> void => {
  let cube: [][][]i64 = [[[1], [2, 3]], [[4]]]
  print("${[plane in cube, row in plane, cell in row | "${cell}"].join(",")}")
}`, "1,2,3,4"},
		{"a range bounded by the earlier clause", `
let main = () -> void => {
  print("${[n in 0..<4, i in 0..<n | "${n}${i}"].join(",")}")
}`, "10,20,21,30,31,32"},
		{"a range at the earlier clause's written type", `
let main = () -> void => {
  let xs = [n: u8 in 0..<3, i in 0..<n | i]
  let last: u8 = xs[xs.len() - 1]
  print("${xs.len()} ${last}")
}`, "3 1"},
		{"a string, by rune", `
let main = () -> void => {
  let words = ["ab", "é", ""]
  print("${[w in words, r in w | r].len()}")
}`, "3"},
		{"a fixed-size array", `
let main = () -> void => {
  let grid: [2][3]i64 = #[#[1, 2, 3], #[4, 5, 6]]
  print("${[row in grid, cell in row | "${cell}"].join(",")}")
}`, "1,2,3,4,5,6"},
		{"a field of the earlier clause", `
struct Team { names: []string }
let main = () -> void => {
  let teams = [Team { names: ["a", "b"] }, Team { names: [] }, Team { names: ["c"] }]
  print("${[team in teams, name in team.names | name].join(",")}")
}`, "a,b,c"},
		{"an element chosen by the earlier clause", `
let main = () -> void => {
  let grid: [][]i64 = [[1, 2], [3]]
  print("${[i in 0..<2, cell in grid[i] | "${i}${cell}"].join(",")}")
}`, "01,02,13"},
		{"guards over both clauses", `
let main = () -> void => {
  let grid: [][]i64 = [[1, 2], [3], [4, 5, 6]]
  print("${[row in grid, cell in row | row.len() > 1, cell % 2 == 0 | "${cell}"].join(",")}")
}`, "2,4,6"},
		{"managed elements outlive their rows", `
let rows = () -> [][]string => [["a" ++ "${1}", "b" ++ "${2}"], ["c" ++ "${3}"]]
let main = () -> void => {
  let flat = [row in rows(), cell in row | cell]
  print("${flat.join(",")}")
}`, "a1,b2,c3"},
		{"an owned source each iteration", `
let spell = (n: i64) -> []string => [i in 0..<n | "${n}:${i}"]
let main = () -> void => {
  print("${[n in [1, 2, 0, 3], s in spell(n) | s].join(",")}")
}`, "1:0,2:0,2:1,3:0,3:1,3:2"},
		{"an owned string source each iteration", `
let main = () -> void => {
  let words = ["ab", "c"]
  print("${[w in words, r in (w ++ "é") | r].len()}")
}`, "5"},
		{"a local whose last read is an owning one in the source", `
let with_row = (label: own string, n: i64) -> []string => [i in 0..<n | "${label}${i}"]
let main = () -> void => {
  let label = "L" ++ "${7}"
  print("${[n in [1, 2], s in with_row(label, n) | s].join(",")}")
}`, "L70,L70,L71"},
		{"a local whose last read is a borrowing one in the source", `
let with_row = (label: string, n: i64) -> []string => [i in 0..<n | "${label}${i}"]
let main = () -> void => {
  let label = "L" ++ "${7}"
  print("${[n in [1, 2], s in with_row(label, n) | s].join(",")}")
}`, "L70,L70,L71"},
		{"a later clause's name is still the outer one in an earlier source", `
let main = () -> void => {
  let z = 7
  print("${[a in [1, 2], b in [a, z], z in [0] | "${b}${z}"].join(",")}")
}`, "10,70,20,70"},
		{"a closure in the source captures the earlier clause", `
let main = () -> void => {
  let xs = [1, 2]
  print("${[a in [10, 20], b in xs.map((x) => x + a) | "${b}"].join(",")}")
}`, "11,12,21,22"},
		{"inside a closure", `
let main = () -> void => {
  let grid: [][]i64 = [[1], [2, 3]]
  let flat = () -> []i64 => [row in grid, cell in row | cell]
  print("${flat().len()}")
}`, "3"},
		{"inside a loop", `
let main = () -> void => {
  var total = 0
  for k in 0..<3 {
    total = total + [n in 0..<k, s in ([i in 0..<n | "${i}"]) | s].len()
  }
  print("${total}")
}`, "1"},
		{"in a generic function", `
let flat<t> = (grid: [][]t) -> []t => [row in grid, cell in row | cell]
let main = () -> void => {
  print("${flat([["a" ++ "${1}"], ["b"]]).join(",")} ${flat([[1], [2, 3]]).len()}")
}`, "a1,b 3"},
		{"inside a sequence's body", `
let cells = gen (grid: [][]i64) -> Seq<i64> => {
  for c in [row in grid, cell in row | cell] { yield c }
}
let main = () -> void => {
  let grid: [][]i64 = [[1], [2, 3]]
  print("${[c in cells(grid) | "${c}"].join(",")}")
}`, "1,2,3"},
		{"the result takes the context's element", `
let main = () -> void => {
  let grid: [][]i64 = [[1], [2, 3]]
  let xs: []u8 = [row in grid, cell in row | 7]
  let m: []Maybe<i16> = [row in grid, cell in row | None]
  print("${xs.len()} ${m.len()}")
}`, "3 3"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := strings.TrimSpace(buildAndRunASanStdout(t, c.src)); got != c.want {
				t.Errorf("got %q; want %q", got, c.want)
			}
		})
	}
}

// A source naming no earlier clause is evaluated once, before the loops, whatever stands
// beside it; a source naming one, once per iteration of that clause. The other way round
// would make how often a call runs depend on what its neighbours read.
func TestExec_ArrayCompSourceEvaluatedAsItsDependenceSays(t *testing.T) {
	t.Parallel()
	got := buildAndRunWithPrelude(t, `module main
var once = 0
var each = 0
let outer = () -> []i64 => { once = once + 1; [1, 2, 3] }
let beside = () -> []i64 => { once = once + 10; [0] }
let inner = (n: i64) -> []i64 => { each = each + 1; [n] }
let main = () -> void => {
  let xs = [a in outer(), b in inner(a), c in beside() | a + b + c]
  print("${xs.len()} ${once} ${each}")
}`, "")
	if got = strings.TrimSpace(got); got != "3 11 3" {
		t.Errorf("got %q; want %q", got, "3 11 3")
	}
}

// The ownership rule the repeat needs, on the two forms that run per iteration without being
// the result. A guard has always run per iteration, so this was a use-after-free before
// sources could: its one owning read of a local took the local's only reference.
func TestExec_ArrayCompGuardOwningLastRead(t *testing.T) {
	t.Parallel()
	got := buildAndRunASanStdout(t, `
let keep = (s: own string, n: i64) -> bool => s.len() > n
let main = () -> void => {
  let label = "L" ++ "${7}"
  print("${[x in [0, 1, 2, 3] | keep(label, x) | x].len()}")
}`)
	if got = strings.TrimSpace(got); got != "2" {
		t.Errorf("got %q; want %q", got, "2")
	}
}

// Reuse reclaims a binding's cells at a `match` on its last use, which a repeat would then run
// again on reclaimed cells, so a name read per iteration is not offered for it.
func TestExec_ArrayCompMatchDoesNotReuseAcrossIterations(t *testing.T) {
	t.Parallel()
	const box = `
data Box = Empty | Full(string)
let open_up = (b: shared Box) -> string => match b { Full(s) => s, Empty => "" }
let row = (b: shared Box) -> []string => [open_up(b)]
`
	cases := []struct{ name, src, want string }{
		{"in the result", box + `
let bump = (b: own shared Box, xs: []i64) -> []string =>
  [x in xs | open_up(match b { Full(s) => Full(s ++ "!"), Empty => Empty })]
let main = () -> void => { print(bump(Full("a" ++ "${1}"), [1, 2]).join(",")) }`, "a1!,a1!"},
		{"in a guard", box + `
let bump = (b: own shared Box, xs: []i64) -> i64 =>
  [x in xs | open_up(match b { Full(s) => Full(s ++ "!"), Empty => Empty }) != "" | x].len()
let main = () -> void => { print("${bump(Full("a" ++ "${1}"), [1, 2])}") }`, "2"},
		{"in a dependent source", box + `
let bump = (b: own shared Box, xs: []i64) -> []string =>
  [x in xs, s in row(match b { Full(s) => Full(s ++ "${x}"), Empty => Empty }) | s]
let main = () -> void => { print(bump(Full("a" ++ "${1}"), [1, 2]).join(",")) }`, "a11,a12"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := strings.TrimSpace(buildAndRunASanStdout(t, c.src)); got != c.want {
				t.Errorf("got %q; want %q", got, c.want)
			}
		})
	}
}

// A way out of the function or the loop from inside a comprehension: the box and what it holds
// so far are released on that path, along with the temporaries that got that far. Every case
// runs a path that leaves after storing something and, where it matters, one that doesn't. The
// leak check on Linux is the point of this; ASan on the Mac still says the box isn't freed twice.
func TestExec_ArrayCompEarlyExitReleasesTheBox(t *testing.T) {
	t.Parallel()
	const row = `
let row = (n: i64) -> Maybe<[]string> => if n > 1 { None } else { Some(["${n}" ++ "!"]) }
let one = (n: i64) -> Maybe<string> => if n > 1 { None } else { Some("${n}" ++ "!") }
let bad = (n: i64) -> Result<string, string> => if n > 1 { Err("no" ++ "${n}") } else { Ok("${n}" ++ "!") }
let many = () -> []i64 => [0, 1, 5, 1]
let count = (m: Maybe<[]string>) -> i64 => (m ?? []).len()
`
	cases := []struct{ name, src, want string }{
		{"a `?` in the result, numbers", row + `
let all = (xs: []i64) -> Maybe<[]i64> => Some([x in xs | (one(x)?).len()])
let main = () -> void => { print("${(all([0, 1]) ?? []).len()} ${(all(many()) ?? []).len()}") }`, "2 0"},
		{"a `?` in the result, managed elements", row + `
let all = (xs: []i64) -> Maybe<[]string> => Some([x in xs | one(x)?])
let main = () -> void => { print("${count(all([0, 1]))} ${count(all(many()))}") }`, "2 0"},
		{"a `?` in a guard", row + `
let all = (xs: []i64) -> Maybe<[]string> => Some([x in xs | (one(x)?).len() > 0 | "${x}" ++ "."])
let main = () -> void => { print("${count(all([0, 1]))} ${count(all(many()))}") }`, "2 0"},
		{"a `?` in a dependent source", row + `
let all = (xs: []i64) -> Maybe<[]string> => Some([x in xs, s in (row(x)?) | s])
let main = () -> void => { print("${count(all([0, 1]))} ${count(all(many()))}") }`, "2 0"},
		{"a `?` carrying a managed error", row + `
let all = (xs: []i64) -> Result<[]string, string> => Ok([x in xs | bad(x)?])
let main = () -> void => {
  print(match all(many()) { Ok(v) => "${v.len()}", Err(e) => e })
}`, "no5"},
		{"an owned source beside the `?`", row + `
let all = () -> Maybe<[]string> => Some([x in many(), y in many() | one(x + y)?])
let main = () -> void => { print("${count(all())}") }`, "0"},
		{"an owned source and an owned dependent source", row + `
let twice = (n: i64) -> []i64 => [n, n + 5]
let all = () -> Maybe<[]string> => Some([x in many(), y in twice(x) | one(y)?])
let main = () -> void => { print("${count(all())}") }`, "0"},
		{"a temporary earlier in the same iteration", row + `
let all = (xs: []i64) -> Maybe<[]string> => Some([x in xs | ("${x}" ++ "a") ++ one(x)?])
let main = () -> void => { print("${count(all([0, 1]))} ${count(all(many()))}") }`, "2 0"},
		{"nested comprehensions", row + `
let all = (xs: []i64) -> Maybe<[][]string> => Some([a in xs | [b in xs | one(a + b)?]])
let main = () -> void => { print("${(all([0, 1]) ?? []).len()} ${(all([0, 1, 1]) ?? []).len()}") }`, "0 0"},
		{"over a sequence", row + `
let upto = gen (n: i64) -> Seq<i64> => { for i in 0..<n { yield i } }
let all = (n: i64) -> Maybe<[]string> => Some([i in upto(n) | one(i)?])
let main = () -> void => { print("${count(all(2))} ${count(all(4))}") }`, "2 0"},
		{"a `return` in the result", row + `
let all = (xs: []i64) -> []string => [x in xs | if x > 1 { return ["early" ++ "!"] } else { "${x}" ++ "." }]
let main = () -> void => { print("${all([0, 1]).len()} ${all(many())[0]}") }`, "2 early!"},
		{"a `break` in the result", row + `
let main = () -> void => {
  var runs = 0
  for k in 0..<3 {
    let got = [x in many() | if x > 1 { break } else { "${x}" ++ "." }]
    runs = runs + got.len()
  }
  print("${runs}")
}`, "0"},
		{"a `continue` in a dependent source", row + `
let main = () -> void => {
  var runs = 0
  for k in 0..<3 {
    let got = [x in many(), s in (if x > 1 { continue } else { [x] }) | "${s}" ++ "."]
    runs = runs + got.len()
  }
  print("${runs}")
}`, "0"},
		{"inside a closure", row + `
let main = () -> void => {
  let all = (xs: []i64) -> Maybe<[]string> => Some([x in xs | one(x)?])
  print("${count(all([0, 1]))} ${count(all(many()))}")
}`, "2 0"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := strings.TrimSpace(buildAndRunASanStdout(t, c.src)); got != c.want {
				t.Errorf("got %q; want %q", got, c.want)
			}
		})
	}
}

// A sequence is still a comprehension's only clause, dependent or not.
func TestEmit_ArrayCompDependentSequenceClauseIsRefused(t *testing.T) {
	t.Parallel()
	_, err := emitSource(t, `
let upto = gen (n: i64) -> Seq<i64> => { for i in 0..<n { yield i } }
let main = () -> u8 => {
  u8([n in [1, 2], i in upto(n) | i].len())
}`)
	if err == nil || !strings.Contains(err.Error(), "takes one clause") {
		t.Fatalf("expected a sequence beside another clause to be refused, got %v", err)
	}
}
