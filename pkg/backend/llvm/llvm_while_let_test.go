package llvm

import "testing"

// `while let` is erased by the collector into `loop { if let … { … } else { break } }`, so
// these pin the meaning the erasure has to keep: the scrutinee is evaluated again on every
// pass, `continue` returns to it, a labeled `break` leaves, and a pattern that cannot fail
// loops until something breaks.
func TestExec_WhileLet(t *testing.T) {
	t.Parallel()
	const at = `
data Item = Got(i64) | Done
let at = pure (xs: []i64, i: i64) -> Item => if i < xs.len() { Got(xs[i]) } else { Done }
`
	cases := []struct {
		name, src string
		want      int
	}{
		{"runs until the pattern fails", at + `
let main = () -> u8 => {
  let xs = [1, 2, 3, 4, 5]
  var i = 0
  var total = 0
  while let Got(x) = at(xs, i) {
    total += x
    i += 1
  }
  u8(total)
}`, 15},
		{"continue re-evaluates the scrutinee", at + `
let main = () -> u8 => {
  let xs = [1, 2, 3, 4, 5]
  var i = 0
  var total = 0
  while let Got(x) = at(xs, i) {
    i += 1
    if x == 4 { continue }
    total += x
  }
  u8(total)
}`, 11},
		{"a labeled break leaves from a nested loop", at + `
let main = () -> u8 => {
  let xs = [1, 2, 3]
  var i = 0
  var n = 0
  outer: while let Got(x) = at(xs, i) {
    i += 1
    loop {
      n += x
      if x == 2 { break outer }
      break
    }
  }
  u8(n)
}`, 3},
		{"an irrefutable pattern loops until a break", `
let main = () -> u8 => {
  let pairs = [(1, 10), (2, 20), (3, 30)]
  var i = 0
  var total = 0
  while let (a, b) = pairs[i] {
    total += a + b
    i += 1
    if i == pairs.len() { break }
  }
  u8(total)
}`, 66},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := buildAndRun(t, c.src); got != c.want {
				t.Errorf("exited %d; want %d", got, c.want)
			}
		})
	}
}

// Each pass's scrutinee is a fresh string, bound by the pattern and dropped at the end of
// the pass — on every exit from it: falling through, `continue`, and the final failed match.
func TestExec_WhileLetReleasesEachPassUnderASan(t *testing.T) {
	t.Parallel()
	src := `
let word = pure (i: i64) -> Maybe<string> => if i < 4 { Some("w" ++ "${i}") } else { None }

let main = () -> u8 => {
  var i = 0
  var n = 0
  while let Some(w) = word(i) {
    i += 1
    if w == "w1" { continue }
    n += w.len()
  }
  u8(n)
}`
	if got := buildAndRunASanWithPrelude(t, src); got != 6 {
		t.Errorf("under ASan: exited %d; want 6", got)
	}
}
