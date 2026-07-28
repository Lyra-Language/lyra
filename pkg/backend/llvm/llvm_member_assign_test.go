package llvm

import "testing"

// `p.x = v` mutates a struct field in place through the struct's storage; nested
// chains (`l.start.x = v`) gep down the path. The root binding must permit interior
// mutation (a `var` or `let mut`), which the typechecker enforces.
func TestExec_MemberAssignment(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want int
	}{
		{
			"single field",
			`struct Point { x: u8, y: u8 }
let main = () -> u8 => {
  var p: Point = Point { x: 1, y: 2 }
  p.x = 9
  p.x
}`,
			9,
		},
		{
			"mutate one field, read another",
			`struct Point { x: u8, y: u8 }
let main = () -> u8 => {
  var p: Point = Point { x: 10, y: 20 }
  p.x = 5
  p.x + p.y
}`,
			25,
		},
		{
			"nested field through a struct-of-structs",
			`struct Point { x: u8, y: u8 }
struct Line { start: Point, end: Point }
let main = () -> u8 => {
  var ln: Line = Line { start: Point { x: 1, y: 2 }, end: Point { x: 3, y: 4 } }
  ln.start.x = 9
  ln.start.x + ln.end.y
}`,
			13,
		},
		{
			"let mut permits interior mutation",
			`struct Point { x: u8, y: u8 }
let main = () -> u8 => {
  let mut p: Point = Point { x: 1, y: 2 }
  p.y = 7
  p.y
}`,
			7,
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

