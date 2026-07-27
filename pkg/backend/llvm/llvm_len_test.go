package llvm

import "testing"

// `xs.len()` is a compiler-provided method on arrays: a fixed-size array's length is
// its compile-time size (a constant), a dynamic array's is the runtime `len` field
// of its box. It returns i64.
func TestExec_ArrayLen(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want int
	}{
		{
			"dynamic array length",
			`let main = () -> u8 => {
  let xs: []i64 = [10, 20, 30]
  u8(xs.len())
}`,
			3,
		},
		{
			"empty dynamic array length is 0",
			`let main = () -> u8 => {
  let xs: []i64 = []
  u8(xs.len())
}`,
			0,
		},
		{
			"fixed-size array length is its size",
			`let main = () -> u8 => {
  let xs: [4]u8 = [1, 2, 3, 4]
  u8(xs.len())
}`,
			4,
		},
		{
			"shared fixed-size array length",
			`let main = () -> u8 => {
  let xs: shared [3]u8 = [10, 20, 30]
  u8(xs.len())
}`,
			3,
		},
		{
			// The practical idiom: index a dynamic array up to its runtime length.
			"len drives a C-style index loop",
			`let main = () -> u8 => {
  var sum: u8 = 0
  let xs: []u8 = [10, 20, 12]
  for var i: i64 = 0; i < xs.len(); i += 1 {
    sum += xs[i]
  }
  sum
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
