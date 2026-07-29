package llvm

import (
	"os/exec"
	"testing"
)

// Assigning a field of a `shared` struct addresses it through the box
// (box → payload → field), the write counterpart to reading a shared field.
func TestExec_SharedMemberAssignment(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
		want int
	}{
		{
			"shared struct field",
			`struct Point { x: u8, y: u8 }
let main = () -> u8 => {
  var p: shared Point = Point { x: 1, y: 2 }
  p.x = 9
  p.x + p.y
}`,
			11, // 9 + 2
		},
		{
			// Nested: a stack Point field inside a shared Line.
			"stack field nested in a shared struct",
			`struct Point { x: u8, y: u8 }
struct Line { start: Point, end: Point }
let main = () -> u8 => {
  var ln: shared Line = Line { start: Point { x: 1, y: 2 }, end: Point { x: 3, y: 4 } }
  ln.start.x = 9
  ln.start.x + ln.end.y
}`,
			13, // 9 + 4
		},
		{
			// A managed (string) field of a shared struct: release-old + own-new,
			// through the box.
			"managed field of a shared struct",
			`struct Named { name: string, id: u8 }
let main = () -> u8 => {
  var n: shared Named = Named { name: "old", id: 1 }
  n.name = "new"
  if n.name == "new" { 1 } else { 0 }
}`,
			1,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := buildAndRun(t, c.src); got != c.want {
				t.Errorf("expected exit %d, got %d", c.want, got)
			}
		})
	}
}

// A managed field of a *shared* struct is fully leak-free: the assignment frees the
// overwritten string, and the box's drop glue frees the final field. Verified under
// AddressSanitizer.
func TestExec_SharedMemberAssignment_ASan(t *testing.T) {
	t.Parallel()
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Skip("clang not found on PATH; skipping ASan test")
	}
	if !asanAvailable(t, clang) {
		t.Skip("ASan runtime not available; skipping")
	}
	src := `struct Named { name: string, id: u8 }
let main = () -> u8 => {
  var n: shared Named = Named { name: "a" ++ "1", id: 1 }
  n.name = "b" ++ "2"
  if n.name == "b2" { 0 } else { 1 }
}
`
	if code := buildAndRunASan(t, clang, src); code != 0 {
		t.Errorf("ASan run: expected exit 0, got %d", code)
	}
}
