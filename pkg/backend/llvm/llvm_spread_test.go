package llvm

import "testing"

// `[...xs, v]` — the one append-shaped syntax the language has, and until 08/27 the one the
// grammar parsed, the collector built, every lint pass counted as a use, and the
// typechecker had no arm for: it fell to the default and reported
// `unknown expression type "...xs"`. The website's arrays guide documented it as working.
//
// A spread makes the result a `[]T` **always**, even where every operand is a fixed `[N]T`
// whose lengths would add up: deciding the arity from whether the operands happen to be
// fixed would make `[...xs, 1]` change type when `xs`'s declaration changes, with the
// literal reading identically.
func TestExec_ArraySpread(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
		want int
	}{
		{
			"append to a dynamic array",
			`let main = () -> u8 => {
  let xs: []u8 = [1, 2]
  let ys: []u8 = [...xs, 3]
  u8(ys.len()) + ys[2]
}`,
			6, // 3 + 3
		},
		{
			"concatenate two dynamic arrays",
			`let main = () -> u8 => {
  let a: []u8 = [1, 2]
  let b: []u8 = [3, 4]
  let c: []u8 = [...a, ...b]
  u8(c.len()) + c[0] + c[3]
}`,
			9, // 4 + 1 + 4
		},
		{
			// A fixed operand unrolls rather than looping, and still yields a `[]T`.
			"spread a fixed array, with elements on both sides",
			`let main = () -> u8 => {
  let a: [3]u8 = [1, 2, 3]
  let c: []u8 = [0, ...a, 4]
  u8(c.len()) + c[1] + c[4]
}`,
			10, // 5 + 1 + 4
		},
		{
			"spread an empty array",
			`let main = () -> u8 => {
  let e: []u8 = []
  let c: []u8 = [...e, 7]
  u8(c.len()) + c[0]
}`,
			8, // 1 + 7
		},
		{
			"only spreads, nothing else",
			`let main = () -> u8 => {
  let a: []u8 = [5]
  let c: []u8 = [...a, ...a, ...a]
  u8(c.len()) + c[2]
}`,
			8, // 3 + 5
		},
		{
			// The operand is a postfix expression, not a bare name — the widening that made
			// the node hold an Expression instead of a string.
			"a call, a member and an index as operands",
			`struct Holder { xs: []u8 }
let mk = () -> []u8 => [1, 2]
let main = () -> u8 => {
  let h = Holder { xs: [3, 4] }
  let nested: [][]u8 = [[5, 6]]
  let c: []u8 = [...mk(), ...h.xs, ...nested[0]]
  u8(c.len()) + c[0] + c[2] + c[4]
}`,
			15, // 6 + 1 + 3 + 5
		},
		{
			// Managed elements: the new box becomes a second owner of every copied one.
			"managed elements are retained per element",
			`let main = () -> u8 => {
  let a: []string = ["x" ++ "1", "y" ++ "2"]
  let c: []string = [...a, "z"]
  u8(c.len()) + u8(c[0].len()) + u8(c[2].len())
}`,
			6, // 3 + 2 + 1
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

// Every operand is evaluated exactly once, in source order — which the two-phase lowering
// has to preserve deliberately, since it walks the elements twice: once to size the box and
// once to fill it. Sizing is what forces the split (a `[]T` operand's length is a load), so
// the risk it creates is precisely double evaluation.
func TestExec_ArraySpreadEvaluatesOperandsOnceInOrder(t *testing.T) {
	t.Parallel()
	src := `let f = (n: i64) -> []i64 => { println("f${n}"); [n] }
let g = (n: i64) -> i64 => { println("g${n}"); n }
let main = () -> void => {
  let c: []i64 = [...f(1), g(2), ...f(3)]
  println("len=${c.len()} ${c[0]}${c[1]}${c[2]}")
}`
	got, _ := buildAndRunCapture(t, src)
	want := "f1\ng2\nf3\nlen=3 123\n"
	if got != want {
		t.Errorf("output %q; want %q", got, want)
	}
}

// The refcounting, where a missing retain on a copied element or an extra release shows up.
func TestExec_ArraySpreadUnderASan(t *testing.T) {
	t.Parallel()
	src := `let main = () -> u8 => {
  var got = 0
  for i in 0..<50 {
    let a: []string = ["alpha" ++ "!", "beta" ++ "!"]
    let b: []string = [...a, "gamma" ++ "!"]
    let c: []string = [...b, ...a]
    got = got + b[0].len() + c[4].len() + c.len()
  }
  if got == 800 { 3 } else { 1 }
}`
	clang := lookClang(t)
	if got := buildAndRun(t, src); got != 3 {
		t.Errorf("exited %d; want 3", got)
	}
	if got := buildAndRunASan(t, clang, src); got != 3 {
		t.Errorf("under ASan: exited %d; want 3", got)
	}
}
