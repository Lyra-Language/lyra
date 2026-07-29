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
