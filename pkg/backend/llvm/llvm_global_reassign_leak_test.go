package llvm

import "testing"

// A managed global reassigned inside a function releases what it held, as the same
// assignment in `main` does (10/29: `q = q.slice(1, q.len())` in a function leaked every
// slice it replaced). LeakSanitizer runs on Linux only (./asan.sh).
func TestExec_AFunctionReassigningAGlobalReleasesWhatItReplaced(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
		want int
	}{
		{"an array, in main", `var q: []i64 = [1, 2, 3, 4]
let main = () -> u8 => {
  q = q.slice(1, q.len())
  q = q.slice(1, q.len())
  u8(q.len() + q[0])
}`, 5},
		{"an array, in a function", `var q: []i64 = [1, 2, 3, 4]
let pop = () -> void => { q = q.slice(1, q.len()) }
let main = () -> u8 => {
  pop()
  pop()
  u8(q.len() + q[0])
}`, 5},
		{"a string, in a function", `var s: string = "a"
let grow = () -> void => { s = s ++ "b" }
let main = () -> u8 => {
  grow()
  grow()
  u8(s.len())
}`, 3},
		{"a struct holding an array, in a function", `struct Box { xs: []i64, n: i64 }
var b: Box = Box { xs: [1], n: 0 }
let bump = () -> void => { b = Box { xs: [b.n, 7], n: b.n + 1 } }
let main = () -> u8 => {
  bump()
  bump()
  u8(b.n + b.xs.len())
}`, 4},
		{"an array, in a loop in a function", `var q: []i64 = [1, 2, 3, 4, 5]
let drain = () -> i64 => {
  var n = 0
  while q.len() > 1 {
    q = q.slice(1, q.len())
    n += 1
  }
  n
}
let main = () -> u8 => u8(drain() + q[0])`, 9},
		{"a field written, in a function", `struct Box { xs: []i64, n: i64 }
var b: Box = Box { xs: [1], n: 0 }
let set = () -> void => { b.xs = [b.n, 5, 6] }
let main = () -> u8 => {
  set()
  set()
  u8(b.xs.len() + b.xs[1])
}`, 8},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := buildAndRunLSanWithPrelude(t, tc.src); got != tc.want {
				t.Errorf("exit %d, want %d", got, tc.want)
			}
		})
	}
}

// Releasing what a global held is safe only because a borrowed read of one is held for its
// statement: here each borrow outlives a call that reassigns the global, which ASan reports
// as a use-after-free the moment the hold is missing.
func TestExec_ABorrowedGlobalOutlivesItsReassignment(t *testing.T) {
	t.Parallel()
	const common = `var q: []i64 = [1, 2, 3, 4]
let pop = () -> void => { q = q.slice(1, q.len()) }
struct Box { xs: []i64, n: i64 }
var b: Box = Box { xs: [10, 20], n: 0 }
let renew = () -> void => { b = Box { xs: [b.n], n: b.n + 1 } }
`
	cases := []struct {
		name string
		src  string
		want int
	}{
		{"passed to a function that reassigns it", `let first = (xs: []i64) -> i64 => {
  pop()
  xs[0] + xs[3]
}
let main = () -> u8 => u8(first(q) + q.len())`, 8},
		{"iterated while the body reassigns it", `let main = () -> u8 => {
  var total = 0
  for x in q {
    pop()
    total += x
  }
  u8(total)
}`, 10},
		{"a field passed on while the struct is replaced", `let both = (xs: []i64) -> i64 => {
  renew()
  xs[0] + xs[1]
}
let main = () -> u8 => u8(both(b.xs) + b.n)`, 31},
		{"matched while an arm reassigns it", `var m: Maybe<string> = Some("x" ++ "yz")
let clear = () -> void => { m = None }
let main = () -> u8 => {
  let n = match m {
    Some(s) => {
      clear()
      "${s}".len()
    },
    None => 0,
  }
  u8(n)
}`, 3},
		{"an element passed to a function that reassigns it", `var names: []string = ["a" ++ "bc", "d"]
let shift = () -> void => { names = names.slice(1, names.len()) }
let size = (s: string) -> i64 => {
  shift()
  "${s}".len()
}
let main = () -> u8 => u8(size(names[0]) + names.len())`, 4},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := buildAndRunLSanWithPrelude(t, common+tc.src); got != tc.want {
				t.Errorf("exit %d, want %d", got, tc.want)
			}
		})
	}
}
