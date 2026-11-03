package llvm

import "testing"

// `data` declares a type only where a capitalized name follows it on its line. Anywhere else
// it is a name, and a statement may begin with it: as a string in the grammar it beat
// `identifier` wherever a declaration could start, so these did not parse (11/03).
func TestExec_DataIsANameUnlessATypeFollows(t *testing.T) {
	src := `
pub data Port = Control | Status

///data Foo is a comment, not a declaration
data  Pair<t> = Both(t) | Neither

struct Regs { data: i64 }

let data = (n: i64) -> i64 => n + 1

let bump = (self: i64) -> i64 => self + 1

let port_name = (p: Port) -> string => match p {
  Control => "control",
  Status => "status",
}

let main = () -> u8 => {
  data(1)
  data(1).bump()
  let r = Regs { data: 2 }
  println("${data(2)} ${r.data} ${port_name(Status)}")
  var bytes = [1, 2]
  bytes[0] = 5
  {
    var data = bytes
    data[1] = 7
    data.push(9)
    println("${data[0]} ${data[1]} ${data.len()}")
    data = [4]
    println("${data.len()}")
  }
  let count = () -> i64 => {
    var data = 3
    data = 4
    data += 1
    data
  }
  println("${count()}")
  match Both(6) {
    Both(n) => println("${n}"),
    Neither => println("neither"),
  }
  0
}
`
	got := buildAndRunWithPrelude(t, src, "")
	want := "3 2 status\n5 7 3\n1\n5\n6\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
