package llvm

import "testing"

// `xs[i] = v` mutates an array element in place: a fixed-size array through its
// storage, a dynamic array through its box. The index is bounds-checked (a negative
// index counts from the end), and the root binding must be mutable (a `var`, `let
// mut`, or `mut`/`own` parameter) — the typechecker enforces that.
func TestExec_ArrayElementAssignment(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want int
	}{
		{
			"fixed-size: constant index",
			`let main = () -> u8 => {
  var xs: [3]u8 = [1, 2, 3]
  xs[1] = 9
  xs[1]
}`,
			9,
		},
		{
			"fixed-size: mutate one, read another",
			`let main = () -> u8 => {
  var xs: [3]u8 = [10, 20, 30]
  xs[0] = 5
  xs[0] + xs[2]
}`,
			35,
		},
		{
			"fixed-size: runtime index",
			`let main = () -> u8 => {
  var xs: [3]u8 = [1, 2, 3]
  var i: i64 = 2
  xs[i] = 8
  xs[2]
}`,
			8,
		},
		{
			"fixed-size: negative index from the end",
			`let main = () -> u8 => {
  var xs: [3]u8 = [1, 2, 3]
  xs[-1] = 9
  xs[2]
}`,
			9,
		},
		{
			"shared fixed-size array",
			`let main = () -> u8 => {
  var xs: shared [3]u8 = [1, 2, 3]
  xs[1] = 9
  xs[1]
}`,
			9,
		},
		{
			"dynamic array",
			`let main = () -> u8 => {
  var xs: []u8 = [10, 20, 30]
  xs[1] = 7
  xs[1]
}`,
			7,
		},
		{
			// A `mut []u8` borrow mutates the caller's array through the shared box.
			"dynamic array via mut parameter",
			`let bump = (xs: mut []u8) -> void => {
  xs[0] = 42
}
let main = () -> u8 => {
  var xs: []u8 = [1, 2, 3]
  bump(xs)
  xs[0]
}`,
			42,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := buildAndRun(t, c.src); got != c.want {
				t.Errorf("expected exit %d, got %d", c.want, got)
			}
		})
	}
}

// An out-of-range index in an assignment traps, exactly like a read.
func TestExec_ArrayElementAssignment_BoundsTrap(t *testing.T) {
	src := `let set = (xs: mut []u8, i: i64) -> void => {
  xs[i] = 1
}
let main = () -> u8 => {
  var xs: []u8 = [1, 2, 3]
  set(xs, 5)
  0
}`
	if got := buildAndRun(t, src); got != 101 {
		t.Errorf("expected an out-of-bounds trap (exit 101), got %d", got)
	}
}

// A managed array-element type is deferred with a loud error (struct-field
// assignment now lowers — see llvm_member_assign_test.go).
func TestEmit_ArrayElementAssignment_Deferred(t *testing.T) {
	src := `let main = () -> u8 => {
  var xs: []string = ["a", "b"]
  xs[0] = "c"
  0
}
`
	if _, err := emitSource(t, src); err == nil {
		t.Errorf("expected a loud error for a managed array-element assignment:\n%s", src)
	}
}
