package llvm

import "testing"

// A generic `[]t` parameter solves `t` from an **array literal** argument (08/13).
//
// `first_of([1, 2, 3])` against `(xs: []t)` reported "cannot infer type variable t
// from these arguments" while the identical call with a `[]i64` *binding* worked,
// and so did a `[3]t` parameter — so the literal was the only thing between a legal
// call and a diagnostic naming the wrong problem. An array literal is the one
// expression whose representation is chosen by its context (`[1, 2, 3]` is a fixed
// `[3]T` or a heap `[]T` by what it is used as), and that choice cannot be made by
// the usual propagation here, because the context is `[]t` and `t` is what is being
// solved. The shape is read off the declaration instead.
//
// These run the compiled program: solving the variable is only half the claim, the
// other half being that the literal is actually built as a ref-counted dynamic array
// the callee can index.
func TestExec_GenericDynamicArrayParamFromLiteral(t *testing.T) {
	t.Parallel()
	const prelude = `let first_of<t> = (xs: []t) -> t => xs[0]
let len_of<t> = (xs: []t) -> i64 => xs.len()
`
	cases := []struct {
		name    string
		main    string
		wantOut string
	}{
		{"solves from an int literal", `println(first_of([7, 8, 9]))`, "7\n"},
		{"solves the length", `println(len_of([1, 2, 3, 4]))`, "4\n"},
		// A managed element type: the literal must be built as a real box whose
		// elements are retained, not as stack storage reinterpreted.
		{"solves from a string literal", `println(first_of(["hello", "world"]))`, "hello\n"},
		{"length of a string array", `println(len_of(["a", "b"]))`, "2\n"},
		// `[v; n]` is the other malleable array form and is adapted the same way.
		{"repeat literal", `println(first_of([7; 3]))`, "7\n"},
		{"repeat literal length", `println(len_of([0; 5]))`, "5\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			out, code := buildAndRunCapture(t, prelude+"let main = () -> void => "+c.main+"\n")
			if out != c.wantOut {
				t.Errorf("stdout = %q; want %q", out, c.wantOut)
			}
			if code != 0 {
				t.Errorf("exit = %d; want 0", code)
			}
		})
	}
}

// The forms that already worked, kept beside the fix so the three ways of reaching a
// generic array parameter stay consistent with each other.
func TestExec_GenericArrayParamOtherForms(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		src     string
		wantOut string
	}{
		{"dynamic array binding", `let first_of<t> = (xs: []t) -> t => xs[0]
			 let main = () -> void => {
			   let xs: []i64 = [1, 2, 3]
			   println(first_of(xs))
			 }`, "1\n"},
		{"fixed-size parameter takes a literal", `let first_fixed<t> = (xs: [3]t) -> t => xs[0]
			 let main = () -> void => println(first_fixed([4, 5, 6]))`, "4\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			out, code := buildAndRunCapture(t, c.src)
			if out != c.wantOut {
				t.Errorf("stdout = %q; want %q", out, c.wantOut)
			}
			if code != 0 {
				t.Errorf("exit = %d; want 0", code)
			}
		})
	}
}

// A dynamic array of a managed type built from a literal at a generic call balances
// its retains and releases. The conservation check is what would catch the literal
// being lowered as stack storage while the callee treats it as a box.
func TestExec_GenericDynamicArrayParamFromLiteral_ASan(t *testing.T) {
	t.Parallel()
	src := `let first_of<t> = (xs: []t) -> t => xs[0]
let main = () -> u8 => {
  let s = first_of(["ab" ++ "cd", "ef"])
  if s == "abcd" { 3 } else { 1 }
}`
	clang := lookClang(t)
	if got := buildAndRun(t, src); got != 3 {
		t.Errorf("exited %d; want 3", got)
	}
	if got := buildAndRunASan(t, clang, src); got != 3 {
		t.Errorf("under ASan: exited %d; want 3", got)
	}
}
