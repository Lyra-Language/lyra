package llvm

import (
	"strings"
	"testing"
)

// Tuple instances lower end to end: construction (`Point(3, 4)`, `(1, 2)`) builds
// a first-class struct value via insertvalue, and positional access (`p.0`) reads
// an element back via extractvalue. These run the compiled program (buildAndRun),
// so a wrong element index or a broken aggregate shows up as the wrong exit code —
// and clang rejects malformed aggregate IR outright.

// TestExec_TupleInstances covers named and anonymous tuples, both access
// positions, and arithmetic on the extracted elements (so the element is a real
// value feeding a computation, not just returned verbatim).
func TestExec_TupleInstances(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
		want int
	}{
		{
			"named .0",
			"tuple Point(u8, u8)\nlet main = () -> u8 => {\n  let p = Point(3, 4)\n  p.0\n}\n",
			3,
		},
		{
			"named .1",
			"tuple Point(u8, u8)\nlet main = () -> u8 => {\n  let p = Point(3, 4)\n  p.1\n}\n",
			4,
		},
		// Anonymous tuple: no declaration, so its struct type is built structurally
		// from the elements (which default to i64), hence the outer u8(...).
		{
			"anonymous .1",
			"let main = () -> u8 => {\n  let p = (7, 9)\n  u8(p.1)\n}\n",
			9,
		},
		// Both elements extracted and combined: 20 + 22 = 42, at u8 width.
		{
			"arithmetic on elements",
			"tuple Pair(u8, u8)\nlet main = () -> u8 => {\n  let p = Pair(20, 22)\n  p.0 + p.1\n}\n",
			42,
		},
		// A tuple passed through a function argument and returned, then indexed —
		// construction and access across a call boundary.
		{
			"tuple element after call",
			"tuple Point(u8, u8)\nlet fst = (p: Point) -> u8 => p.0\nlet main = () -> u8 => fst(Point(11, 22))\n",
			11,
		},
	}
	for _, c := range cases {
		t.Run("", func(t *testing.T) {
			t.Parallel()
			if got := buildAndRun(t, c.src); got != c.want {
				t.Errorf("%s: exited %d; want %d", c.name, got, c.want)
			}
		})
	}
}

// TestEmit_TupleInstanceIR pins the shape of the emitted IR: a named tuple
// construction builds its declared struct type via insertvalue, and indexing
// reads it back via extractvalue.
func TestEmit_TupleInstanceIR(t *testing.T) {
	t.Parallel()
	got, err := emitSource(t, "tuple Point(u8, u8)\nlet main = () -> u8 => {\n  let p = Point(3, 4)\n  p.0\n}\n")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"%Point = type { i8, i8 }",
		"insertvalue",
		"extractvalue",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("emitted IR missing %q:\n%s", want, got)
		}
	}
}

// An anonymous tuple literal must be built at the *context's* element widths, not at
// the untyped-literal default.
//
// inferTupleLiteralExpr deliberately leaves an anonymous tuple's elements untyped so a
// surrounding context can narrow them, and propagateLiteralType did narrow the leaves —
// but it never re-recorded the type of the tuple **node**, which is what the backend
// builds the aggregate from. So `f((10, 40))` against a `(u8, u8)` parameter emitted
// `call i8 @f({ i64, i64 })` into a `{ i8, i8 }` parameter: invalid IR.
//
// It went unnoticed because Apple clang cannot diagnose it — with opaque pointers the
// two function types are indistinguishable, and arm64 passes small structs in registers
// so the low bytes happen to carry the right values and the program "works". Debian's
// older typed-pointer clang rejects it outright, which is how ./asan.sh found it. The
// array case next to it in propagateLiteralType had always re-recorded, with a comment
// explaining why; tuples simply didn't.
func TestExec_AnonymousTupleTakesContextWidth(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
		want int
	}{
		{
			// The original report (TestExec_MatchGuards/#05 under Linux clang).
			"tuple argument against a narrow parameter",
			`let f = (t: (u8, u8)) -> u8 => match t {
			   (a, b) if a > b => a,
			   (a, b) => b,
			 }
			 let main = () -> u8 => f((10, 40))`,
			40,
		},
		{
			"nested tuple argument",
			`let f = (t: ((u8, u8), u8)) -> u8 => t.0.0 + t.0.1 + t.1
			 let main = () -> u8 => f(((1, 2), 4))`,
			7,
		},
		{
			// The return position: the same re-record is what makes the body's tuple
			// match the declared result type.
			"tuple return type",
			`let mk = () -> (u8, u8) => (4, 5)
			 let main = () -> u8 => { let p = mk()  p.0 + p.1 }`,
			9,
		},
		{
			"tuple as a struct field value",
			`struct Holder { p: (u8, u8) }
			 let main = () -> u8 => { let s = Holder { p: (2, 4) }  s.p.0 + s.p.1 }`,
			6,
		},
		{
			"tuple as a data constructor payload",
			`data D = W((u8, u8))
			 let main = () -> u8 => match W((2, 3)) { W((a, b)) => a + b }`,
			5,
		},
		{
			// Elements narrow independently, so a mixed-width context must not push
			// one element's width onto the other.
			"mixed element widths",
			`let f = (t: (u8, i64)) -> u8 => t.0 + u8(t.1)
			 let main = () -> u8 => f((10, 2))`,
			12,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := buildAndRun(t, c.src); got != c.want {
				t.Errorf("exited %d; want %d", got, c.want)
			}
		})
	}
}

// The IR assertion the behavioral tests above cannot make on macOS: the call site's
// argument type must equal the callee's declared parameter type. On a modern clang this
// mismatch is unrepresentable (opaque pointers) and the program still produces the right
// answer by ABI luck, so only the emitted text shows it.
func TestEmit_TupleArgumentMatchesParameterType(t *testing.T) {
	t.Parallel()
	src := `let f = (t: (u8, u8)) -> u8 => t.0 + t.1
	 let main = () -> u8 => f((10, 40))`
	ir, err := emitSource(t, src)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ir, "define i8 @f({ i8, i8 }") {
		t.Fatalf("expected @f to take { i8, i8 }:\n%s", ir)
	}
	if !strings.Contains(ir, "call i8 @f({ i8, i8 }") {
		t.Errorf("the call site does not pass { i8, i8 } — the tuple literal was built at "+
			"the wrong width, which is invalid IR that only an older (typed-pointer) clang "+
			"rejects:\n%s", ir)
	}
}
