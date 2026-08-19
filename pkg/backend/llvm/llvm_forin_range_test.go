package llvm

import (
	"strings"
	"testing"
)

// `for i in START..<END` (and `..<=`, with an optional `:step`) lowers as a counter
// loop: the counter is the loop variable, `..<` is an exclusive end and `..<=`
// inclusive.
func TestExec_ForInRange(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
		want int
	}{
		{
			"exclusive range sums 0..3",
			`let main = () -> u8 => {
  var sum: u8 = 0
  for i in 0..<4 {
    sum += u8(i)
  }
  sum
}`,
			6, // 0+1+2+3
		},
		{
			"inclusive range sums 0..<=4",
			`let main = () -> u8 => {
  var sum: u8 = 0
  for i in 0..<=4 {
    sum += u8(i)
  }
  sum
}`,
			10, // 0+1+2+3+4
		},
		{
			"variable end",
			`let main = () -> u8 => {
  var sum: u8 = 0
  let n: i64 = 5
  for i in 0..<n {
    sum += u8(i)
  }
  sum
}`,
			10, // 0+1+2+3+4
		},
		{
			// Typed bounds make the counter that width (u8 here), no conversion needed.
			"typed u8 bounds",
			`let main = () -> u8 => {
  var sum: u8 = 0
  let lo: u8 = 1
  let hi: u8 = 4
  for i in lo..<hi {
    sum += i
  }
  sum
}`,
			6, // 1+2+3
		},
		{
			"stepped range",
			`let main = () -> u8 => {
  var sum: u8 = 0
  for i in 0..<10:2 {
    sum += u8(i)
  }
  sum
}`,
			20, // 0+2+4+6+8
		},
		{
			"break out of a range loop",
			`let main = () -> u8 => {
  var sum: u8 = 0
  for i in 0..<10 {
    if i == 3 { break }
    sum += u8(i)
  }
  sum
}`,
			3, // 0+1+2
		},
		{
			"continue skips a value",
			`let main = () -> u8 => {
  var sum: u8 = 0
  for i in 0..<5 {
    if i == 2 { continue }
    sum += u8(i)
  }
  sum
}`,
			8, // 0+1+3+4
		},
		{
			// The idiom: index a dynamic array by a range up to its length.
			"range over 0..<xs.len() indexes the array",
			`let main = () -> u8 => {
  var sum: u8 = 0
  let xs: []u8 = [10, 20, 12]
  for i in 0..<xs.len() {
    sum += xs[i]
  }
  sum
}`,
			42, // 10+20+12
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

// The advance is guarded: the counter moves only when it can move by `step` and stay
// inside the range. An unguarded add wraps at the type's edge, and every case here was
// an infinite loop before 08/12: an inclusive end at the counter type's max wrapped
// (255 → 0 over u8) and re-entered the range, and a large step leapt an *exclusive*
// end the same way (200 + 100 over u8 is 44, still under 250). A silent infinite loop
// is the wrap-instead-of-trap answer the language's arithmetic exists to rule out; the
// loop's fix is to exit rather than trap, because visiting the type's own max is what
// the author asked for and nothing overflowed in what they wrote.
func TestExec_ForInRangeEdgeTermination(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
		want int
	}{
		{
			"inclusive end at the u8 max terminates",
			`let main = () -> u8 => {
  let hi: u8 = 255
  var n: i64 = 0
  for i in 0..<=hi { n += 1 }
  u8(n % 100)
}`,
			56, // 256 iterations
		},
		{
			"inclusive full i8 domain, min to max",
			`let main = () -> u8 => {
  let lo: i8 = -128
  let hi: i8 = 127
  var n: i64 = 0
  for i in lo..<=hi { n += 1 }
  u8(n % 100)
}`,
			56, // 256 iterations
		},
		{
			// The step never lands on the end, so the equality the inclusive case
			// exits on is not the mechanism — the distance comparison is.
			"stepped inclusive end at the max, step skips past it",
			`let main = () -> u8 => {
  let hi: u8 = 255
  var n: i64 = 0
  for i in 0..<=hi:2 { n += 1 }
  u8(n)
}`,
			128, // 0, 2, …, 254
		},
		{
			"large step leaps an exclusive end",
			`let main = () -> u8 => {
  let hi: u8 = 250
  var n: i64 = 0
  for i in 0..<hi:100 { n += 1 }
  u8(n)
}`,
			3, // 0, 100, 200
		},
		{
			"descending inclusive end at the u8 min",
			`let main = () -> u8 => {
  let hi: u8 = 255
  var n: i64 = 0
  for i in hi..>=0 { n += 1 }
  u8(n % 100)
}`,
			56, // 256 iterations
		},
		{
			// A runtime step that is fine stays fine — the guard is for the bad values.
			"positive runtime step iterates normally",
			`let pick = (x: i64) -> i64 => x
let main = () -> u8 => {
  let n = pick(3)
  var c: i64 = 0
  for i in 0..<10:n { c += 1 }
  u8(c)
}`,
			4, // 0, 3, 6, 9
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

// A step only knowable at run time gets the constant rule as a runtime check: zero or
// negative traps instead of spinning forever. The same ladder as a shift amount —
// provable → compile error (types.InvalidStepReason), otherwise → trap. Before 08/12
// a runtime zero step compiled clean and looped forever.
func TestExec_ForInRangeRuntimeStepTraps(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, src string }{
		{
			"zero runtime step traps",
			`let pick = (x: i64) -> i64 => x - x
let main = () -> u8 => {
  let n = pick(5)
  var c: i64 = 0
  for i in 0..<10:n { c += 1 }
  u8(c)
}`,
		},
		{
			"negative runtime step traps",
			`let pick = (x: i64) -> i64 => 0 - x
let main = () -> u8 => {
  let n = pick(1)
  var c: i64 = 0
  for i in 0..<10:n { c += 1 }
  u8(c)
}`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			out, code := buildAndRunPanic(t, c.src)
			if code != overflowTrapExitCode {
				t.Errorf("exited %d; want %d (the trap)\n%s", code, overflowTrapExitCode, c.src)
			}
			if !strings.Contains(out, "range step must be positive") {
				t.Errorf("stderr = %q; want the range-step trap message", out)
			}
		})
	}
}

// A range with a **negative bound** gives its counter the `untyped_signed_int` type,
// while a non-negative literal is `untyped_int` — so `d != 0` compared two untyped
// integers of different signedness, which assignability refuses and ordering accepts.
// `d < 0` compiled and `d != 0` did not.
//
// The counter runs to the end here rather than only being compared, because the fix is in
// the *typechecker* and the thing worth pinning at this level is that the loop still
// emits the arithmetic it always did at whatever width the literal settles on.
func TestExec_ForInRange_NegativeBoundCounterCompares(t *testing.T) {
	t.Parallel()
	const src = `
module main
let main = () -> void => {
  var nonzero = 0
  var sum = 0
  for d in -1..<=1 {
    if d != 0 { nonzero = nonzero + 1 }
    sum = sum + d
  }
  println("${nonzero} ${sum}")
}
`
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "2 0" {
		t.Errorf("got %q; want \"2 0\"", got)
	}
}

// The neighbour-offset loop the bug was found in — two nested ranges with negative
// bounds, skipping the centre.
func TestExec_ForInRange_NeighbourOffsets(t *testing.T) {
	t.Parallel()
	const src = `
module main
let main = () -> void => {
  var visited = 0
  for dy in -1..<=1 {
    for dx in -1..<=1 {
      if dx != 0 || dy != 0 { visited = visited + 1 }
    }
  }
  println("${visited}")
}
`
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "8" {
		t.Errorf("got %q; want \"8\"", got)
	}
}
