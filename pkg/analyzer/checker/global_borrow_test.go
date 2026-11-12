package checker_test

import (
	"strings"
	"testing"

	"github.com/Lyra-Language/lyra/pkg/analyzer/checker"
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector"
	"github.com/Lyra-Language/lyra/pkg/analyzer/typechecker"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/parser"
	"github.com/Lyra-Language/lyra/pkg/typetable"
)

const globalBorrowPreamble = `struct H { n: i64, xs: []i64, name: string }
struct Big { a: i64, b: i64 }
alias Row = []i64
trait Bump { bump: (mut Self) -> void }
impl Bump for H { bump = (self) => { self.n += 1 } }
var g = [0, 0, 0]
var row: Row = [0, 0]
var grid = [[0, 0]]
var hs = [H { n: 0, xs: [], name: "a" }]
var b = H { n: 0, xs: [1], name: "b" }
var bigs = [Big { a: 1, b: 2 }]
var fixed: [2]i64 = #[1, 2]
var fixed_hs: [2]Big = #[Big { a: 1, b: 2 }, Big { a: 3, b: 4 }]
var shared_h: shared H = H { n: 0, xs: [], name: "c" }
let table = [1, 2, 3]
let add = (x: mut i64) -> void => { x += 1 }
let peek = (x: ref i64) -> i64 => x
let look = (x: ref Big) -> i64 => x.a
let name_len = (s: ref string) -> i64 => 1
let fill = (xs: mut []i64) -> void => { xs[0] = 1 }
let up = (self: mut H) -> void => { self.n += 1 }
let lookup = (xs: ref []i64) -> i64 => 1
let by_value = (n: i64) -> i64 => n
let own_it = (s: own string) -> i64 => 1
let with_bound<t> where t: Bump = (x: mut t) -> void => { x.bump() }
`

func checkGlobalBorrows(t *testing.T, source string) []diag.Diagnostic {
	t.Helper()
	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	c := collector.NewCollector([]byte(source))
	program, symTable, scopeTable, collectErrs := c.Collect(tree.RootNode())
	if len(collectErrs) > 0 {
		t.Fatalf("collector errors: %v", collectErrs)
	}
	tt := typetable.New()
	tc := typechecker.New(symTable, scopeTable, tt)
	tc.Check(program)
	return checker.CheckGlobalBorrows(program, symTable, tt, tc.MethodTable())
}

func TestGlobalBorrow_RefusesStorageInAGlobalsBox(t *testing.T) {
	for _, stmt := range []string{
		`add(g[0])`,
		`add(row[0])`,
		`add(grid[0][1])`,
		`fill(grid[0])`,
		`add(b.xs[0])`,
		`add(hs[0].n)`,
		`up(hs[0])`,
		`hs[0].up()`,
		`hs[0].bump()`,
		`with_bound(hs[0])`,
		`add(shared_h.n)`,
		`let n = look(bigs[0])`,
		`let n = name_len(hs[0].name)`,
		`let n = lookup(grid[0])`,
	} {
		t.Run(stmt, func(t *testing.T) {
			got := checkGlobalBorrows(t, globalBorrowPreamble+"\nlet main = () -> void => {\n  "+stmt+"\n}\n")
			if len(got) != 1 {
				t.Fatalf("expected 1 error, got %d: %v", len(got), got)
			}
			if got[0].Code != diag.CodeGlobalBorrow || got[0].Severity != diag.SeverityError {
				t.Errorf("wrong diagnostic: %+v", got[0])
			}
			if !strings.Contains(got[0].Message, "Copy it into a local") {
				t.Errorf("should say what to do instead: %q", got[0].Message)
			}
		})
	}
}

func TestGlobalBorrow_RefusesInAnyFunctionBody(t *testing.T) {
	for name, src := range map[string]string{
		"a nested lambda": `let f = () -> void => {
  let inner = () -> void => { add(g[0]) }
  inner()
}`,
		"an impl method": `struct Q { n: i64 }
trait Go { go: (Self) -> void }
impl Go for Q { go = (self) => { add(g[0]) } }`,
		"a trait's default method": `trait Go { go: (Self) -> void = (self) => { add(g[0]) } }`,
		"an inherent method": `struct Q { n: i64 }
impl Q {
  let go = (self) => add(g[0])
}`,
		"a generic function": `let f<t> = (x: t) -> void => { add(g[0]) }`,
	} {
		t.Run(name, func(t *testing.T) {
			got := checkGlobalBorrows(t, globalBorrowPreamble+src+"\n")
			if len(got) != 1 {
				t.Fatalf("expected 1 error, got %d: %v", len(got), got)
			}
		})
	}
}

// Storage no box holds can't be freed, and a borrow of it can't dangle.
func TestGlobalBorrow_AllowsWhatNothingCanFree(t *testing.T) {
	for _, stmt := range []string{
		`fill(g)`,
		`add(b.n)`,
		// The slot holding the array is the root's own, and a pointer to a slot survives what
		// the slot comes to hold.
		`fill(b.xs)`,
		`fill(row)`,
		`add(fixed[0])`,
		`add(fixed_hs[0].a)`,
		`let n = look(fixed_hs[1])`,
		`up(b)`,
		`b.up()`,
		`b.bump()`,
		// A scalar through `ref` is passed by value, and a value is not a borrow.
		`let n = peek(g[0])`,
		`let n = by_value(g[0])`,
		`let n = own_it(hs[0].name)`,
		// A `let` global is never written.
		`let n = lookup(table)`,
		`let n = peek(table[0])`,
		// A builtin runs no code of the program's.
		`grid[0].push(1)`,
		`g.push(1)`,
		`b.xs.push(1)`,
		`let n = grid[0].len()`,
		// A local of the same name is not the global.
		"var g = [1]\n  add(g[0])",
		"var ys = [1]\n  add(ys[0])",
		"var hs = [H { n: 0, xs: [], name: \"x\" }]\n  hs[0].up()",
		"for g in [[1]] { let n = lookup(g) }",
		"if true {\n    var g = [2]\n    add(g[0])\n  }",
		// A temporary has no place to dangle from.
		`let n = look(Big { a: 1, b: 2 })`,
		// Copying out and assigning back is the way round it.
		"var n = g[0]\n  add(n)\n  g[0] = n",
	} {
		t.Run(stmt, func(t *testing.T) {
			if got := checkGlobalBorrows(t, globalBorrowPreamble+"\nlet main = () -> void => {\n  "+stmt+"\n}\n"); len(got) != 0 {
				t.Errorf("expected no error, got %v", got)
			}
		})
	}
}

func TestGlobalBorrow_AParameterIsNotTheGlobal(t *testing.T) {
	for name, src := range map[string]string{
		"a function's":   `let f = (g: mut []i64) -> void => { add(g[0]) }`,
		"a lambda's":     `let f = () -> void => { let h = (g: []i64) -> i64 => peek(g[0])  let n = h([1]) }`,
		"an impl's":      `struct Q { n: i64 }` + "\n" + `trait Go { go: (Self, []i64) -> i64 }` + "\n" + `impl Go for Q { go = (self, g) => lookup(g) }`,
		"a local inside": `let f = () -> void => { var hs = [1]  add(hs[0]) }`,
	} {
		t.Run(name, func(t *testing.T) {
			if got := checkGlobalBorrows(t, globalBorrowPreamble+src+"\n"); len(got) != 0 {
				t.Errorf("expected no error, got %v", got)
			}
		})
	}
}

// A C function can't reach a Lyra global, so a `mut` scalar wrapping an in-out pointer stays legal.
func TestGlobalBorrow_AllowsACFunction(t *testing.T) {
	src := globalBorrowPreamble + `unsafe extern c_bump: (x: mut i64) -> void
let main = () -> void => {
  unsafe { c_bump(g[0]) }
}
`
	if got := checkGlobalBorrows(t, src); len(got) != 0 {
		t.Errorf("expected no error, got %v", got)
	}
}

// A function returning a sequence runs between its consumer's iterations, so a box's storage
// can't be lent to one, a local's included. What no box holds stays legal, as for lyra-E091.
func TestSeqBorrow(t *testing.T) {
	t.Parallel()
	const lib = `struct P { n: i64, m: i64 }
let each = gen (x: ref P, n: i64) -> Seq<i64> => { for i in 0..<n { yield x.n } }
let bump = gen (x: mut P) -> Seq<i64> => { x.n += 1; yield x.n }
let alias = (x: ref P) -> Seq<i64> => each(x, 2)
let each_of<t> = gen (x: ref t) -> Seq<i64> => { yield 1 }
let by_value = gen (x: P) -> Seq<i64> => { yield x.n }
let plain = (x: ref P) -> i64 => x.n
let walk = gen (xs: ref []P) -> Seq<i64> => { for x in xs { yield x.n } }
let scalar = gen (x: ref i64) -> Seq<i64> => { yield x }
`
	cases := []struct {
		name, body string
		refused    bool
	}{
		{"an element", `var ys = [P { n: 1, m: 0 }]
  for v in each(ys[0], 2) { print("${v}") }`, true},
		{"a `mut` element", `var ys = [P { n: 1, m: 0 }]
  for v in bump(ys[0]) { print("${v}") }`, true},
		{"through a plain function naming it", `var ys = [P { n: 1, m: 0 }]
  for v in alias(ys[0]) { print("${v}") }`, true},
		{"a generic parameter", `var ys = [P { n: 1, m: 0 }]
  for v in each_of(ys[0]) { print("${v}") }`, true},
		{"a comprehension's source", `var ys = [P { n: 1, m: 0 }]
  print("${[v in each(ys[0], 2) | v].len()}")`, true},
		{"a parameter's element", `let f = (ys: ref []P) -> i64 => [v in each(ys[0], 2) | v].len()
  print("${f([P { n: 1, m: 0 }])}")`, true},
		{"a whole local", `var p = P { n: 1, m: 0 }
  for v in each(p, 2) { print("${v}") }`, false},
		{"a whole array", `var ys = [P { n: 1, m: 0 }]
  for v in walk(ys) { print("${v}") }`, false},
		{"an element of a fixed array", `var ps: [1]P = #[P { n: 1, m: 0 }]
  for v in each(ps[0], 2) { print("${v}") }`, false},
		{"a temporary", `for v in each(P { n: 1, m: 0 }, 2) { print("${v}") }`, false},
		{"by value", `var ys = [P { n: 1, m: 0 }]
  for v in by_value(ys[0]) { print("${v}") }`, false},
		{"a scalar", `var ns = [1]
  for v in scalar(ns[0]) { print("${v}") }`, false},
		{"a function that isn't one", `var ys = [P { n: 1, m: 0 }]
  print("${plain(ys[0])}")`, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got := seqBorrowDiagnostics(t, lib+"let main = () -> void => {\n  "+c.body+"\n}\n")
			if c.refused != (len(got) > 0) {
				t.Errorf("refused = %v; diagnostics %v", c.refused, got)
			}
		})
	}
}

func seqBorrowDiagnostics(t *testing.T, src string) []diag.Diagnostic {
	t.Helper()
	var out []diag.Diagnostic
	for _, d := range checkGlobalBorrows(t, src) {
		if d.Code == diag.CodeSeqBorrow {
			if !strings.Contains(d.Message, "sequence") {
				t.Errorf("message doesn't name the sequence: %s", d.Message)
			}
			out = append(out, d)
		}
	}
	return out
}

// Storage inside a box lent beside a `mut` argument that can hold a box is lyra-E097's: after
// `var zs = ys` the two are one array, which places can't tell, so growing `zs` frees `ys[0]`.
func TestSecondNameBorrow(t *testing.T) {
	const pre = `struct H { n: i64, xs: []string, name: string }
let via = (s: ref string, zs: mut []string) -> void => { zs.push("x") }
let bump = (x: mut i64, zs: mut []i64) -> void => { zs.push(1) }
let named = (s: string, hs: mut []H) -> void => { hs[0].name = "x" }
let fine = (x: ref i64, n: mut i64) -> void => { n += 1 }
let bare = (s: string, zs: mut []string) -> void => { zs[0] = "x" }
let scalar = (x: ref i64, zs: mut []i64) -> void => { zs.push(1) }
struct F { ns: []i64 }
let other = (s: ref string, f: mut F) -> void => { f.ns.push(1) }
let holder = (s: ref string, h: mut H) -> void => { h.xs.push("y") }
`
	for stmt, wantE097 := range map[string]bool{
		`via(ss[0], zs)`:         true,
		`bump(ys[0], ys2)`:       true,
		`named(hs[0].name, hs2)`: false, // a bare value, held for the call
		`via(h.xs[0], zs)`:       true,
		`bare(ss[0], zs)`:        false, // a value, held for the call instead
		`fine(ys[0], n)`:         false, // a `mut` scalar holds no box
		`via(fixed[0], zs)`:      false, // a fixed-size array's elements are its own
		`other(ss[0], f)`:        false, // an F can't hold a []string
		`holder(ss[0], h)`:       true,  // an H can: its `xs`
		`scalar(ys[0], ys2)`:     false, // a scalar `ref` is a copy
	} {
		t.Run(stmt, func(t *testing.T) {
			src := pre + "let main = () -> void => {\n" +
				"  var ys = [1, 2]\n  var ys2 = ys\n  var n = 0\n" +
				"  var ss = [\"a\", \"b\"]\n  var zs = ss\n" +
				"  var fixed: [2]string = #[\"a\", \"b\"]\n" +
				"  var hs = [H { n: 0, xs: [], name: \"a\" }]\n  var hs2 = hs\n" +
				"  var h = H { n: 0, xs: [\"c\"], name: \"b\" }\n  var f = F { ns: [] }\n" +
				"  " + stmt + "\n}\n"
			got := checkGlobalBorrows(t, src)
			if !wantE097 {
				if len(got) != 0 {
					t.Fatalf("expected nothing, got %v", got)
				}
				return
			}
			if len(got) != 1 || got[0].Code != diag.CodeSecondNameBorrow {
				t.Fatalf("expected one lyra-E097, got %v", got)
			}
			if !strings.Contains(got[0].Message, "Copy argument 1 into a local") {
				t.Errorf("should say what to do instead: %q", got[0].Message)
			}
		})
	}
}

// The pair one of whose places contains the other is the typechecker's to refuse, by place,
// so E097 says nothing more about it.
func TestSecondNameBorrow_LeavesAContainingPlaceToTheTypechecker(t *testing.T) {
	src := `let via = (s: ref string, zs: mut []string) -> void => { zs.push("x") }
let main = () -> void => {
  var ys = ["a", "b"]
  via(ys[0], ys)
}
`
	for _, d := range checkGlobalBorrows(t, src) {
		if d.Code == diag.CodeSecondNameBorrow {
			t.Errorf("E097 beside the typechecker's own: %v", d)
		}
	}
}
