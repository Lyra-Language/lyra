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
