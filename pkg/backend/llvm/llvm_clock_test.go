package llvm

import (
	"strconv"
	"strings"
	"testing"
)

// The clock: the `wall_clock_nanos()` builtin (clock.go).
//
// Until 08/06 there was nothing to test — `wallClock` was an entry in the effect
// table with no signature and no lowering, so it type-checked and then crashed the
// backend, which is the shape `Random.global()` had.

// It must report a real time, not a plausible-looking constant. The floor is
// 2023-11-14 (1.7e18 ns), which any correct clock passes and a zeroed struct, a
// seconds-vs-nanoseconds mixup, or a dropped `tv_sec` all fail. The ceiling catches
// the mirror mistake — nanoseconds counted twice — and is far enough out (year 2100)
// to never need touching.
func TestExec_WallClockReportsARealTime(t *testing.T) {
	t.Parallel()
	const src = `
module main
let main = () -> void => {
  println("${wall_clock_nanos()}");
}
`
	out := strings.TrimSpace(buildAndRunWithPrelude(t, src, ""))
	ns, err := strconv.ParseInt(out, 10, 64)
	if err != nil {
		t.Fatalf("non-numeric clock reading %q: %v", out, err)
	}
	const nov2023 = 1_700_000_000_000_000_000
	const year2100 = 4_100_000_000_000_000_000
	if ns < nov2023 || ns > year2100 {
		t.Errorf("clock read %d ns since the epoch, which is not a plausible time; "+
			"want between %d and %d", ns, nov2023, year2100)
	}
}

// Two readings in one process must not go backwards, and must actually move. The
// first catches a clock that is not reading anything; the second catches one whose
// nanosecond field is dropped, which leaves a value that only changes once a second
// and so looks correct in the test above.
func TestExec_WallClockAdvances(t *testing.T) {
	t.Parallel()
	const src = `
module main
let main = () -> void => {
  let t0 = wall_clock_nanos();
  var spin = 0;
  var i = 0;
  for i < 200000 {
    spin = spin + i;
    i = i + 1;
  }
  let t1 = wall_clock_nanos();
  println("${t1 >= t0} ${t1 > t0} ${spin > 0}");
}
`
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "true true true" {
		t.Errorf("clock readings around a busy loop gave %q; want \"true true true\" "+
			"(monotone, and moving at nanosecond resolution)", got)
	}
}

// The effect story, and the reason the clock is a builtin while everything derived
// from it belongs in the prelude: reading a clock nobody passed in is what makes a
// computation irreproducible, so `det` refuses it — while a *threaded* timestamp is
// ordinary `i64` data carrying no effect at all. Exactly the split `random_seed`
// has, which is what EffectTime existing as its own bit buys.
func TestCheck_AmbientClockIsNotDetButAThreadedOneIs(t *testing.T) {
	t.Parallel()
	t.Run("a threaded timestamp is allowed in det", func(t *testing.T) {
		t.Parallel()
		const src = `
module main
let elapsed = det (t0: i64, t1: i64) -> i64 => t1 - t0
let main = () -> void => { println("${elapsed(1, 9)}") }
`
		if diags := checkWithPrelude(t, src); len(diags) != 0 {
			t.Errorf("arithmetic on a passed-in timestamp should be `det`, got: %v", diags)
		}
	})
	t.Run("the ambient clock is refused in det", func(t *testing.T) {
		t.Parallel()
		const src = `
module main
let stamp = det () -> i64 => wall_clock_nanos()
let main = () -> void => { println("${stamp()}") }
`
		diags := checkWithPrelude(t, src)
		if len(diags) == 0 {
			t.Fatal("reading the ambient clock must be refused in `det`")
		}
		// It must name the *clock*, not the generic input effect — the point of
		// EffectTime having its own bit is that the diagnostic can say which
		// non-determinism it found.
		if !strings.Contains(diags[0], "system clock") {
			t.Errorf("diagnostic should name the system clock, got: %s", diags[0])
		}
	})
}
