package llvm

import (
	"os/exec"
	"testing"
)

// A struct literal is a postfix head as of 08/03 (tree-sitter-lyra): `Node { n: 7 }.n`,
// `.a()`, `.cells[0]`. Before that none of them parsed, while every other value-producing
// expression already worked as a receiver.
//
// These run rather than merely parse. The grammar's corpus pins the *tree*; what it cannot
// show is that the literal is constructed and then read — a shape the backend had never
// seen, since the receiver was always a binding or a call result before.
func TestExec_StructLiteralAsReceiver(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
		want int
	}{
		{
			"field access",
			`struct Node { n: i64 }
let main = () -> u8 => u8(Node { n: 7 }.n)
`,
			7,
		},
		{
			"method call",
			`struct Node { n: i64 }
trait Peek { peek: (Self) -> i64 }
impl Peek for Node { peek = (self) => self.n * 6 }
let main = () -> u8 => u8(Node { n: 7 }.peek())
`,
			42,
		},
		{
			"index through a field",
			`struct Grid { cells: [2]i64 }
let main = () -> u8 => u8(Grid { cells: [21, 9] }.cells[0])
`,
			21,
		},
		{
			// The case Rust and Go forbid: in a statement header the literal's `{` and
			// the body's are the same token to a bounded-lookahead parser. GLR keeps
			// both readings alive, so this needs no parentheses here.
			"in an if condition",
			`struct Node { n: i64 }
let main = () -> u8 => if Node { n: 7 }.n > 0 { u8(35) } else { u8(0) }
`,
			35,
		},
		{
			// A managed field read straight out of a temporary: the literal owns the
			// string for exactly as long as the expression, so the ownership pass has
			// to release the box while the field it yielded stays alive.
			"managed field off a temporary",
			`struct Tag { name: string }
let main = () -> u8 => {
  println(Tag { name: "hello" }.name)
  u8(14)
}
`,
			14,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := buildAndRun(t, c.src); got != c.want {
				t.Errorf("%s: exited %d; want %d", c.name, got, c.want)
			}
		})
	}
}

// The managed case under AddressSanitizer. A temporary receiver is new ground for the
// ownership pass — the literal's box must outlive the field read and then be released
// exactly once — and neither a missed nor a doubled release shows up in the exit code.
func TestExec_StructLiteralReceiverManagedASan(t *testing.T) {
	t.Parallel()
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Skip("clang not found on PATH")
	}
	if !asanAvailable(t, clang) {
		t.Skip("AddressSanitizer not available in this toolchain")
	}
	const src = `struct Tag { name: string }
let main = () -> u8 => {
  println(Tag { name: "hello" }.name)
  println(Tag { name: "world" }.name)
  u8(14)
}
`
	if got := buildAndRunASan(t, clang, src); got != 14 {
		t.Errorf("asan: exited %d; want 14", got)
	}
}
