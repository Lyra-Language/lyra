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

	// As in the golden test: a Lyra raw string's backticks cannot sit inside a Go one.
	const bt2 = "\x60"

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
		// **The harvest's new fields (09/24)**, in the compositions the goldens do not
		// isolate — and two of these found real disagreements rather than confirming
		// agreement, which is what the oracle is for.
		//
		// The escapes: `\u` at four digits and `\o` in a *char* appear in no golden at
		// all, and the lettered ones only as `\n` and `\t`. A fixed width per prefix is
		// what makes `"\012"` NUL followed by `12` rather than a newline.
		{"a four-digit unicode escape in a char", `let c = '\u00E9'`},
		{"a four-digit unicode escape in a string", `let s = "\u2603 snow"`},
		{"an octal escape in a char", `let c = '\o101'`},
		{"the lettered escapes", `let s = "\a\b\e\f\v"`},
		{"an escaped quote and backslash", `let s = "a \" b \\ c"`},
		{"a raw string with escape-looking text", "let s = " + bt2 + `${not} \n` + bt2},
		// Optional chaining composed with what slices 5 and 6 built.
		{"an optional index on a call result", "let a = rows()?[0]"},
		{"an optional member inside an interpolation", `let s = "${a?.b}"`},
		{"a bare const name", "let m = MAX"},
		{"a const name indexed", "let m = LIMITS[0]"},
		// A lambda carrying only `async`, where both goldens carry `pure` too — and the
		// declaration form of the modifiers beside generic parameters, which is where the
		// two spellings of `pure` have to agree.
		{"a lambda with only async", "let f = async () => 1"},
		{"modifiers and generics together", "let pure id<t>(x: t) -> t => x"},
		{"a generic parameter list with two variables", "let pair<a, b> = (x: a, y: b) => x"},
		// A default that is not a literal, which is the only golden's shape.
		{"a default value that is an expression", "let f = (n: i64 = 1 + 2) => n"},
		{"a default value that is a call", "let f = (n: i64 = size()) => n"},
		// A written annotation in the shapes the two goldens do not reach: a declared type
		// rather than a primitive, and a declaration with no value at all.
		{"an annotated declaration of a declared type", "let p: Point = make()"},
		{"a var with an annotation and no value", "var x: i64"},
		// An anonymous tuple away from a `tuple` declaration. **The return type is the
		// case that found two bugs**: `ReturnType`'s label goes through Go's `GetName()`,
		// which renders a tuple's elements rather than its `Name` field, and `bool`'s AST
		// name is `boolean` — the one primitive whose name is not what the source wrote.
		// Neither is visible in any matching golden.
		{"an anonymous tuple as a struct field type", "struct S { pair: (i64, i64) }"},
		{"an anonymous tuple as a return type", "let f = () -> (i64, bool) => t"},
		{"a nested anonymous tuple", "tuple T(((i64, i64), i64))"},
		// **The literal family (09/24)**, in the shapes the goldens do not reach. The
		// emptiness rule is the first one: `[]` is an empty composite and disappears,
		// `#[]` is not — the flavor is a field, so a fixed empty array has a non-zero one
		// — and `0.0` disappears where `0` does not, floats having a zero case in
		// `isZeroValue` where integers have none.
		{"an empty dynamic array literal", "let a = []"},
		{"an empty fixed array literal", "let a = #[]"},
		{"a float of zero", "let a = 0.0"},
		{"a float that is a whole number", "let a = 1.0"},
		// The float parse, at and past the range where it is exact. 10^22 is the last
		// power of ten a double holds exactly; the long mantissas are the ones that used
		// to **trap**, taking the whole file down rather than collecting imprecisely.
		{"a float at the top of the exact range", "let a = 1.0e22"},
		{"a float at the bottom of the exact range", "let a = 1.0e-22"},
		{"a float with seventeen significant digits", "let a = 1.2345678901234567"},
		{"a float with more digits than a mantissa holds", "let a = 1.234567890123456789012345"},
		{"a float whose integer part overruns the mantissa",
			"let a = 123456789012345678901234567890.5"},
		// Nesting, which is where a wrapper taken for an element would show.
		{"nested array literals", "let a = [[1, 2], [3]]"},
		{"an array of tuples", "let a = [(1, 2), (3, 4)]"},
		{"a tuple holding an array", "let a = ([1, 2], 3)"},
		{"a fixed array inside a dynamic one", "let a = [#[1], #[2]]"},
		{"a repeat whose value is an array", "let a = [[0]; 4]"},
		{"a repeat whose count is an expression", "let a = [0; n * 2]"},
		{"a tuple literal with one element", "let a = (1,)"},
		{"a named tuple with two generic arguments", "let a = Pair::<i32, string>(1, s)"},
		// And composed with what the earlier slices built.
		{"an array literal as a call argument", "let a = f([1, 2])"},
		{"an array literal indexed", "let a = [1, 2][0]"},
		{"a concat of members", "let s = a.left ++ b.right"},
		{"a concat inside an interpolation", `let s = "${a ++ b}"`},
		{"a float inside an interpolation", `let s = "${1.5}"`},
		{"a negated float", "let a = -1.5"},
		{"a float in a range", "let a = 0.5..<1.5"},
		// The second harvest pass's two, in the shapes the goldens do not reach.
		{"a const with no annotation", `const NAME = "lyra"`},
		{"a public const", "pub const SIZE: i64 = 8"},
		{"a const holding an array", "const ORIGIN = #[0, 0]"},
		{"a generic call with one argument", "let a = parse::<i64>(s)"},
		{"a generic call with no value arguments", "let a = empty::<string>()"},
		{"a generic call whose callee is a member", "let a = xs.fold::<i64>(0, f)"},
		{"a generic call nested in a generic call", "let a = outer::<i64>(inner::<bool>(x))"},
		{"a generic call with a tuple type argument", "let a = make::<(i64, bool)>(1)"},
		// No case for a float too large for an `f64` (`1.0e400`): the Go collector reports
		// an error there, and this test fails on one rather than comparing. The two do
		// agree on the tree — it places a zero-valued node, which prints nothing, and so
		// does this — but the agreement has to be stated here rather than asserted.
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
