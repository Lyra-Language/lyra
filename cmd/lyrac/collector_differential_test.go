package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Lyra-Language/lyra/pkg/analyzer/collector"
	"github.com/Lyra-Language/lyra/pkg/parser"
	"github.com/Lyra-Language/lyra/pkg/printer"
)

// **The Go collector as the oracle directly**, rather than through its stored goldens.
//
// The golden files are a curated set, and a slice can add a construct none of them
// exercises in isolation: slice 5's calls and blocks appear in plenty of goldens and in no
// *matching* one, because every golden using them also uses something further out. Pinning
// them against hand-written expectations would rebuild the weakness slice 4 deleted — a
// second construction of the same tree, free to drift.
//
// Running both collectors on the same source removes the curation. The Go AST goes through
// `pkg/printer` exactly as the goldens were produced, so agreement here is agreement with
// the file on disk, for any source either can be given.
func TestCollector_AgreesWithTheGoCollector(t *testing.T) {
	bin := buildCollector(t)
	for _, c := range []struct{ name, source string }{
		// Slice 5's two, in the shapes no matching golden covers.
		{"a call with no arguments", "let a = foo()"},
		{"a call with arguments", "let b = bar(1, x)"},
		{"a call whose callee is a member", "let c = person.greet()"},
		{"a non-empty block as a body", "let d = () => {\n  let inner = 1\n}"},
		{"a block holding several statements", "let e = () => {\n  let x = 1\n  let y = 2\n}"},
		{"a call inside a block", "let f = () => {\n  let g = h(1)\n}"},
		{"nested calls", "let i = outer(inner(2))"},
		// And the postfix family beside them, since they compose.
		{"indexing a call's result", "let j = rows()[0]"},
		{"a member of an index", "let k = grid[1].name"},
		{"a tuple index of a member", "let l = pair.first.0"},
		// Slice 6, in the compositions no single golden covers: the new nodes nest inside
		// each other and inside what slice 5 built.
		{"a try inside a call argument", "let m = wrap(read()?)"},
		{"a negation of a member", "let n = -point.x"},
		{"a boolean over calls", "let o = left() < right()"},
		{"a range over member expressions", "let p = a.lo..<b.hi"},
		{"a range whose step is a call", "let q = 0..<n:step()"},
		{"a try on a member call", "let r = handle.read()?"},
		{"logic mixing comparison and calls", "let s = a < b && c() != d"},
		{"a negation inside a range bound", "let t = -5..<5"},
		// Slice 7, in the shapes the stored goldens do not isolate: a declared name used
		// as a field type (UnresolvedType) and a bound variable used as one (GenericType),
		// which no matching golden reached until these existed.
		{"a struct field of a declared type", "struct Holder { inner: Point }"},
		{"a struct with a generic parameter", "struct Cell<t> { value: t }"},
		{"a data constructor carrying a declared type", "data Color = Named(CSSName)"},
		{"a data constructor carrying a variable", "data Holder<t> = Wrap(t)"},
		{"a tuple of mixed declared and primitive types", "tuple Row(Name, i64)"},
		// Destructuring, interpolation and newtypes, in compositions no golden isolates.
		// A nullary constructor, which carries no Pattern field at all — the shape that
		// checks the field is omitted rather than printed empty. `let Some (Ok v) = m`
		// would belong here too, but its inner `(Ok v)` is a *tuple pattern*, a kind this
		// subset does not collect (todo.md), and a case asserting agreement on a construct
		// the walk skips would be asserting the skip.
		{"a destructuring of a nullary constructor", "let None = m"},
		{"a destructuring with a var keyword", "var Some x = m"},
		{"an interpolation holding a range", `let s = "${a..<b}"`},
		{"an interpolation holding an index", `let s = "${xs[0]}"`},
		{"adjacent interpolations with no text between", `let s = "${a}${b}"`},
		{"an interpolation at the start and end", `let s = "${a} mid ${b}"`},
		{"a newtype over a declared type", "newtype Id = UserKey"},
		// All four range end operators. The descending pair is legal in an expression and
		// appears in no golden, so without these the data type modelling the operator
		// would have two constructors nothing ever built.
		{"an ascending exclusive range", "let r = 0..<10"},
		{"an ascending inclusive range", "let r = 0..<=10"},
		{"a descending exclusive range", "let r = 10..>0"},
		{"a descending inclusive range", "let r = 10..>=0"},
		// No case for an *absent* end operator: `0..` is deliberately not an expression
		// yet (todo.md, Ranges), and a range with an end always writes one. So
		// `end_operator: None` is currently reachable only through pattern ranges, which
		// this subset does not collect — the Maybe models a state the grammar permits and
		// this position does not yet reach.
	} {
		t.Run(c.name, func(t *testing.T) {
			source := filepath.Join(t.TempDir(), "in.lyra")
			if err := os.WriteFile(source, []byte(c.source), 0o644); err != nil {
				t.Fatal(err)
			}
			got, err := exec.Command(bin, source).Output()
			if err != nil {
				t.Fatalf("the Lyra collector exited non-zero on %q: %v", c.source, err)
			}
			if want := goCollectorAST(t, c.source); string(got) != want {
				t.Errorf("the two collectors disagree on %q\n Lyra:\n%s\n Go:\n%s",
					c.source, got, want)
			}
		})
	}
}

// goCollectorAST is the Go collector's answer for source, printed the way the goldens are.
func goCollectorAST(t *testing.T, source string) string {
	t.Helper()
	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("parsing %q: %v", source, err)
	}
	program, _, _, errs := collector.NewCollector([]byte(source)).Collect(tree.RootNode())
	if len(errs) > 0 {
		t.Fatalf("the Go collector rejected %q: %v", source, errs)
	}
	out := printer.PrintAST(program)
	if !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	return out
}
