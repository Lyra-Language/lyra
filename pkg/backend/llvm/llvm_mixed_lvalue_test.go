package llvm

import "testing"

// Mixed index + member assignment paths — `grid[i].y = v`, `p.arr[i] = v`,
// `m[i][j] = v` — lower via the recursive lvalueAddress, which nests an `[i]` hop
// and a `.field` hop in either order.
func TestExec_MixedLValueAssignment(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want int
	}{
		{
			// Array element's field: grid[0].y
			"field of an array element",
			`struct Point { x: u8, y: u8 }
let main = () -> u8 => {
  var grid: [2]Point = [Point { x: 1, y: 2 }, Point { x: 3, y: 4 }]
  grid[0].y = 9
  grid[0].y + grid[1].x
}`,
			12, // 9 + 3
		},
		{
			// Struct field that is a fixed-size array: b.data[1]
			"element of a struct's array field",
			`struct Buf { data: [3]u8, n: u8 }
let main = () -> u8 => {
  var b: Buf = Buf { data: [1, 2, 3], n: 3 }
  b.data[1] = 8
  b.data[1] + b.n
}`,
			11, // 8 + 3
		},
		{
			// Struct field that is a dynamic array: v.items[1] (mutates the shared box)
			"element of a struct's dynamic array field",
			`struct Vec { items: []u8, tag: u8 }
let main = () -> u8 => {
  var v: Vec = Vec { items: [10, 20, 30], tag: 1 }
  v.items[1] = 5
  v.items[1] + v.tag
}`,
			6, // 5 + 1
		},
		{
			// Two-dimensional fixed-size array: m[0][1]
			"element of a 2-D array",
			`let main = () -> u8 => {
  var m: [2][2]u8 = [[1, 2], [3, 4]]
  m[0][1] = 9
  m[0][1] + m[1][0]
}`,
			12, // 9 + 3
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
