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

// repeatAliasWarnings runs the pipeline lyra-W019 needs — it reads the TypeTable, so
// the typechecker has to have run — and returns one message per warning.
func repeatAliasWarnings(t *testing.T, source string) []string {
	t.Helper()
	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	c := collector.NewCollector([]byte(source))
	program, symTable, scopeTable, _ := c.Collect(tree.RootNode())
	tt := typetable.New()
	typechecker.New(symTable, scopeTable, tt).Check(program)
	var msgs []string
	for _, d := range checker.CheckArrayRepeatAliasing(program, symTable, tt) {
		if d.Code != diag.CodeSharedRepeatElement {
			t.Fatalf("unexpected code %q", d.Code)
		}
		if d.Severity != diag.SeverityWarning {
			t.Fatalf("lyra-W019 must be a warning, got severity %v", d.Severity)
		}
		msgs = append(msgs, d.Message)
	}
	return msgs
}

func assertShared(t *testing.T, source, wantNamed string) {
	t.Helper()
	got := repeatAliasWarnings(t, source)
	if len(got) != 1 {
		t.Fatalf("want exactly one lyra-W019, got %d: %v", len(got), got)
	}
	if !strings.Contains(got[0], wantNamed) {
		t.Fatalf("message does not name %q: %s", wantNamed, got[0])
	}
}

func assertNotShared(t *testing.T, source string) {
	t.Helper()
	if got := repeatAliasWarnings(t, source); len(got) != 0 {
		t.Fatalf("want no lyra-W019, got: %v", got)
	}
}

// The case the diagnostic exists for: `[[' '; WIDTH]; HEIGHT]` is one row referenced
// HEIGHT times, so every `grid[py][px] = c` writes the same place. Found in
// examples/mandelbrot.lyra, where a uniform image read as bad arithmetic.
func TestRepeatAlias_GridOfRows(t *testing.T) {
	assertShared(t, `
let main = () => {
  var row: []rune = [' ', ' ']
  var grid: [][]rune = [row; 4]
  grid[0][0] = 'x'
}
`, "[]rune")
}

// The exclusion that keeps the warning readable. A string is managed and copying one
// shares its box — so a check built on ownership.IsManaged would fire here — but it is
// immutable, so the sharing is unobservable and `["hi"; 3]` is ordinary correct code.
func TestRepeatAlias_StringElementIsSilent(t *testing.T) {
	assertNotShared(t, `
let main = () => {
  var words: []string = ["hi"; 3]
  words[0] = "bye"
}
`)
}

// Scalars are copied outright, and so is a fixed-size `[N]T` of them: its storage is
// inline, one region per slot.
func TestRepeatAlias_ScalarAndFixedArrayAreSilent(t *testing.T) {
	assertNotShared(t, `
let main = () => {
  var ns: []i64 = [0; 3]
  ns[0] = 7
  var rows: [][2]i64 = [[0, 0]; 3]
  rows[0][0] = 7
}
`)
}

// A struct of scalars is copied per slot; a struct holding a `[]T` is copied
// *shallowly*, so its array is shared. The message names the field, since `Row` alone
// says nothing about what it holds.
func TestRepeatAlias_StructSharesThroughItsArrayField(t *testing.T) {
	assertNotShared(t, `
struct Cell { n: i64 }
let main = () => {
  var cs: []Cell = [Cell { n: 0 }; 3]
  cs[0].n = 7
}
`)
	assertShared(t, `
struct Row { cells: []i64 }
let main = () => {
  var rs: []Row = [Row { cells: [0, 0] }; 3]
  rs[0].cells[0] = 7
}
`, "`cells`")
}

// `readonly` stops the direct write (`rs[0].cells[0] = 7` is lyra-E001) and not the
// two-line launder that gets the same effect, so a frozen field whose type shares
// mutable state still shares it. Measured, not assumed.
func TestRepeatAlias_ReadonlyFieldStillShares(t *testing.T) {
	assertShared(t, `
struct Row { readonly cells: []i64 }
let main = () => {
  var rs: []Row = [Row { cells: [0, 0] }; 3]
  let mut c = rs[0].cells
  c[0] = 7
}
`, "`cells`")
}

// A `data` payload is reached through, and the type's own spelling already shows what
// it holds — so no field path is appended to `Held<[]i64>`.
//
// Substituted rather than read off the declaration, which is the same trap
// parameterizedOwnsManaged documents: the declaration's payload is the variable `t`
// and shares nothing at all, so `Held<[]i64>` and `Held<i64>` would answer alike.
// (Declared here rather than using the prelude's `Maybe` because this harness collects
// one source file and has no prelude in its symbol table.)
func TestRepeatAlias_DataPayload(t *testing.T) {
	assertShared(t, `
data Held<t> = Full(t) | Empty
let main = () => {
  let inner: []i64 = [0, 0]
  var ms: []Held<[]i64> = [Full(inner); 3]
}
`, "Held<[]i64>")
	assertNotShared(t, `
data Held<t> = Full(t) | Empty
let main = () => {
  var ms: []Held<i64> = [Full(1); 3]
}
`)
}

// A count of 0 or 1 fills no second slot, so there is nothing to share with. A count
// only known at run time is assumed plural — that is the case the author cannot see
// the number for either.
func TestRepeatAlias_CountBoundaries(t *testing.T) {
	assertNotShared(t, `
let main = () => {
  var row: []rune = [' ']
  var one: [][]rune = [row; 1]
  var none: [][]rune = [row; 0]
}
`)
	assertShared(t, `
let main = (n: i64) => {
  var row: []rune = [' ']
  var many: [][]rune = [row; n]
}
`, "[]rune")
}

// The reason this is a standalone post-typecheck pass rather than an arm of
// inferArrayRepeatType: under a `[][]rune` annotation the inner `[' '; WIDTH]` infers
// as the fixed `[WIDTH]rune`, which shares nothing, and only propagation widens it to
// the `[]rune` that does. Checking at inference would clear the motivating program.
func TestRepeatAlias_ElementWidenedByPropagation(t *testing.T) {
	got := repeatAliasWarnings(t, `
const WIDTH = 4
let main = () => {
  var grid: [][]rune = [[' '; WIDTH]; 3]
  grid[0][0] = 'x'
}
`)
	if len(got) != 1 {
		t.Fatalf("want one lyra-W019 (the outer repeat only), got %d: %v", len(got), got)
	}
	if !strings.Contains(got[0], "[]rune") {
		t.Fatalf("message names the pre-propagation type: %s", got[0])
	}
}
