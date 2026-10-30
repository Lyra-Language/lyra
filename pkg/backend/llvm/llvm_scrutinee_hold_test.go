package llvm

import "testing"

// What an arm binds is borrowed from its scrutinee, so an arm that writes the place the
// scrutinee was read from — the binding, a field or an element of it — must not free what
// the binding still reads. `ui.sdl3`'s event loop did, on every keystroke (10/29):
//
//	while let Some(got) = next { next = poll(); got.text }
//
// The ownership pass holds such a scrutinee as a temporary of its own (holdScrutinee):
// retained as it is read, released after the statement — on every way out of the arms,
// which the exits below are here for. Under ASan a free too early is a use-after-free;
// under LeakSanitizer (Linux, ./asan.sh) a release missed on an exit is a leak.
func TestExec_AnArmWritingItsScrutineeKeepsWhatItBound(t *testing.T) {
	t.Parallel()
	const common = `
struct Got { n: i64, text: string }
var polled: i64 = 0
// 3, 5 and 7, each its text as long and one more, then nothing.
let poll = () -> Maybe<Got> => {
  if polled == 3 { return None }
  let n = 3 + 2 * polled
  polled += 1
  Some(Got { n: n, text: "${[i in 0..<n | "x"].join("")}!" })
}
// The text's length as a copy of its bytes counts it: a string's length is beside its
// pointer, so .len() alone would never touch the freed bytes ASan watches.
let size = (s: string) -> i64 => "${s}".len()
`
	cases := []struct {
		name string
		src  string
		want int
	}{
		{"while let reassigning its scrutinee", `let main = () -> u8 => {
  var total = 0
  var next = poll()
  while let Some(got) = next {
    next = poll()
    total += size(got.text)
  }
  u8(total)
}`, 18},
		{"if let", `let main = () -> u8 => {
  var next = poll()
  if let Some(got) = next {
    next = None
    return u8(size(got.text))
  }
  0
}`, 4},
		{"match", `let main = () -> u8 => {
  var next = poll()
  match next {
    Some(got) => {
      next = poll()
      u8(size(got.text))
    },
    None => 0,
  }
}`, 4},
		{"a field of a mut self", `struct Pump { cur: Maybe<Got> }
impl Pump {
  let step = (self: mut) -> i64 => match self.cur {
    Some(got) => {
      self.cur = poll()
      size(got.text)
    },
    None => 0,
  }
}
let main = () -> u8 => {
  var p = Pump { cur: poll() }
  u8(p.step() + p.step())
}`, 10},
		{"an element of an array", `let main = () -> u8 => {
  var xs: []Maybe<Got> = [poll()]
  if let Some(got) = xs[0] {
    xs = []
    return u8(size(got.text))
  }
  0
}`, 4},
		{"continue and break out of the arm", `let main = () -> u8 => {
  var total = 0
  var next = poll()
  while let Some(got) = next {
    next = poll()
    if got.n == 3 { continue }
    total += size(got.text)
    if got.n == 5 { break }
  }
  u8(total)
}`, 6},
		{"a return out of the arm", `let first = () -> i64 => {
  var next = poll()
  while let Some(got) = next {
    next = poll()
    if got.n == 5 { return size(got.text) }
  }
  0
}
let main = () -> u8 => u8(first())`, 6},
		{"a global a call in the arm writes", `var current: Maybe<Got> = None
let advance = () -> void => { current = poll() }
let main = () -> u8 => {
  current = poll()
  if let Some(got) = current {
    advance()
    return u8(size(got.text))
  }
  0
}`, 4},
		{"a guard reading the binding", `let main = () -> u8 => {
  var next = poll()
  match next {
    Some(got) if size(got.text) > 1 => {
      next = poll()
      u8(size(got.text) + got.n)
    },
    _ => 0,
  }
}`, 7},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := buildAndRunASanWithPrelude(t, "module main\n"+common+c.src); got != c.want {
				t.Errorf("exit %d, want %d", got, c.want)
			}
		})
	}
}
