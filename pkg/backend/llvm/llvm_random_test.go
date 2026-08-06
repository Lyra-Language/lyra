package llvm

import (
	"strconv"
	"strings"
	"testing"
)

// Randomness: the `random_seed()` entropy builtin (random.go) and the prelude's
// generator built on it (`Rng`, `next_u64`, `below`, `between`, `random_below`,
// `random_between`).
//
// These run through buildAndRunWithPrelude (llvm_input_test.go) because the
// generator is real Lyra in `std/prelude.lyra` — testing a pasted copy would test
// a second implementation free to drift from the one users get.

// The property that makes a seeded generator worth having: same seed, same
// sequence. It is checked *within* one process (two generators from one seed) and
// *across* processes (the recorded values below), because those catch different
// mistakes — the first catches state leaking between generators, the second
// catches a seed quietly picking up entropy.
func TestExec_RngSeededIsReproducible(t *testing.T) {
	t.Parallel()
	const src = `
module main
let main = () -> void => {
  var a = rng_seeded(42);
  var b = rng_seeded(42);
  var i = 0;
  for i < 5 {
    println("${a.below(1000)} ${b.below(1000)}");
    i = i + 1;
  }
}
`
	first := buildAndRunWithPrelude(t, src, "")
	for _, line := range strings.Split(strings.TrimSpace(first), "\n") {
		pair := strings.Fields(line)
		if len(pair) != 2 || pair[0] != pair[1] {
			t.Fatalf("two generators seeded alike diverged: %q", line)
		}
	}
	// A second process must produce the identical sequence — a seeded generator
	// that mixed in any entropy would pass the within-process check above.
	if second := buildAndRunWithPrelude(t, src, ""); second != first {
		t.Errorf("seeded sequence differs across runs:\n%s\nvs\n%s", first, second)
	}
}

// Seed 0 must not be a fixed point. xorshift maps 0 to 0, so a generator that
// stored the seed verbatim would emit zero forever — and `rng_seeded(0)` is the
// most natural thing a caller reaching for a fixed seed would write, so this is a
// trap rather than an edge case.
func TestExec_RngSeedZeroIsNotAFixedPoint(t *testing.T) {
	t.Parallel()
	const src = `
module main
let main = () -> void => {
  var r = rng_seeded(0);
  var i = 0;
  var zeros = 0;
  for i < 20 {
    if r.below(1000) == 0 { zeros = zeros + 1; }
    i = i + 1;
  }
  println("${zeros}");
}
`
	out := strings.TrimSpace(buildAndRunWithPrelude(t, src, ""))
	if out != "0" && out != "1" {
		t.Errorf("seed 0 produced %s zeros in 20 draws; want ~0 (a stuck generator gives 20)", out)
	}
}

// `below` must stay in range and cover it. A generator that is stuck, or one whose
// rejection loop is wrong, shows up here as a missing or out-of-range value.
func TestExec_RngBelowIsInRangeAndSpread(t *testing.T) {
	t.Parallel()
	const src = `
module main
let main = () -> void => {
  var r = rng_seeded(12345);
  var i = 0;
  for i < 2000 {
    println("${r.below(10)}");
    i = i + 1;
  }
}
`
	seen := map[int]int{}
	for _, line := range strings.Fields(buildAndRunWithPrelude(t, src, "")) {
		n, err := strconv.Atoi(line)
		if err != nil {
			t.Fatalf("non-numeric draw %q", line)
		}
		if n < 0 || n >= 10 {
			t.Fatalf("draw %d outside 0..<10", n)
		}
		seen[n]++
	}
	if len(seen) != 10 {
		t.Errorf("only %d of 10 buckets were hit in 2000 draws: %v", len(seen), seen)
	}
	// A very loose uniformity floor. 2000 draws over 10 buckets expects 200 each;
	// anything below 100 means a bucket is being systematically starved (a bad
	// reduction), while a real generator's variance stays far above it. Deliberately
	// not a chi-squared test — this is a seeded, fixed sequence, so the bar only has
	// to catch a broken reduction, and a tight bound on a fixed sequence is a test
	// that fails the day the algorithm is legitimately changed.
	for bucket, count := range seen {
		if count < 100 {
			t.Errorf("bucket %d hit only %d times in 2000 draws: %v", bucket, count, seen)
		}
	}
}

// `below(1)` is the degenerate case the rejection loop is most likely to hang on:
// every value lands in the single bucket, so a cutoff computed one off could reject
// everything and spin forever.
func TestExec_RngBelowOne(t *testing.T) {
	t.Parallel()
	const src = `
module main
let main = () -> void => {
  var r = rng_seeded(7);
  println("${r.below(1)} ${r.below(1)} ${r.below(1)}");
}
`
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "0 0 0" {
		t.Errorf("below(1) = %q; want \"0 0 0\"", got)
	}
}

// A non-positive bound is a caller error, not a value to invent — `below` panics
// rather than returning something arbitrary.
func TestExec_RngBelowRejectsNonPositiveBound(t *testing.T) {
	t.Parallel()
	const src = `
module main
let main = () -> void => {
  var r = rng_seeded(7);
  println("${r.below(0)}");
}
`
	out := buildAndRunWithPrelude(t, src, "")
	if strings.Contains(out, "0") && !strings.Contains(out, "panic") {
		t.Errorf("below(0) produced output %q; expected a panic", out)
	}
}

// `between` is inclusive at both ends — that is what "a number between 0 and 100"
// means, and an exclusive high bound would silently make 100 unreachable.
func TestExec_RngBetweenIsInclusive(t *testing.T) {
	t.Parallel()
	const src = `
module main
let main = () -> void => {
  var r = rng_seeded(999);
  var lo = 0;
  var hi = 0;
  var bad = 0;
  var i = 0;
  for i < 3000 {
    let n = r.between(1, 6);
    if n == 1 { lo = lo + 1; }
    if n == 6 { hi = hi + 1; }
    if n < 1 || n > 6 { bad = bad + 1; }
    i = i + 1;
  }
  println("${lo} ${hi} ${bad}");
}
`
	f := strings.Fields(strings.TrimSpace(buildAndRunWithPrelude(t, src, "")))
	if len(f) != 3 {
		t.Fatalf("unexpected output %q", f)
	}
	if f[2] != "0" {
		t.Errorf("%s draws fell outside 1..=6", f[2])
	}
	if f[0] == "0" {
		t.Error("the low bound 1 was never drawn — between is exclusive at the bottom")
	}
	if f[1] == "0" {
		t.Error("the high bound 6 was never drawn — between is exclusive at the top")
	}
}

// A single-value range: `between(5, 5)` must yield 5, not panic and not loop.
func TestExec_RngBetweenSingleValue(t *testing.T) {
	t.Parallel()
	const src = `
module main
let main = () -> void => {
  var r = rng_seeded(3);
  println("${r.between(5, 5)} ${r.between(5, 5)}");
}
`
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "5 5" {
		t.Errorf("between(5,5) = %q; want \"5 5\"", got)
	}
}

// Negative bounds must work — `below` takes an i64 and `between` shifts by `lo`,
// so a range that straddles zero is ordinary arithmetic and not a special case.
func TestExec_RngBetweenNegativeRange(t *testing.T) {
	t.Parallel()
	const src = `
module main
let main = () -> void => {
  var r = rng_seeded(2024);
  var bad = 0;
  var i = 0;
  for i < 500 {
    let n = r.between(-10, -5);
    if n < -10 || n > -5 { bad = bad + 1; }
    i = i + 1;
  }
  println("${bad}");
}
`
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "0" {
		t.Errorf("%s draws fell outside -10..=-5", got)
	}
}

// The ambient form must actually vary between processes — that is the entire
// difference from the seeded one, and a `random_seed` that returned a constant
// would pass every test above.
func TestExec_RandomBelowVariesAcrossRuns(t *testing.T) {
	t.Parallel()
	const src = `
module main
let main = () -> void => {
  var i = 0;
  for i < 8 {
    println("${random_below(1000000)}");
    i = i + 1;
  }
}
`
	first := buildAndRunWithPrelude(t, src, "")
	second := buildAndRunWithPrelude(t, src, "")
	if first == second {
		t.Errorf("two runs of random_below produced identical output:\n%s", first)
	}
	// Within one run the eight draws must differ too: `random_below` seeds a fresh
	// generator per call, so a seed that did not change between calls in the same
	// process would show up as eight identical values while still differing across
	// runs.
	vals := strings.Fields(strings.TrimSpace(first))
	uniq := map[string]bool{}
	for _, v := range vals {
		uniq[v] = true
	}
	if len(uniq) < len(vals)-1 {
		t.Errorf("draws within one run repeat: %v", vals)
	}
}

// The effect story, which is the reason only the *seed* is a builtin: a seeded
// generator is reproducible from its input, so it is legal in `det`; reaching for
// entropy nobody supplied is not.
//
// This is a compile-time property, so it is asserted on the diagnostics rather
// than on a program's output.
func TestCheck_SeededRngIsDetLegalAndAmbientIsNot(t *testing.T) {
	t.Parallel()
	t.Run("seeded is allowed in det", func(t *testing.T) {
		t.Parallel()
		const src = `
module main
let roll = det (seed: u64) -> i64 => {
  var rng = rng_seeded(seed);
  rng.below(6)
}
let main = () -> void => { println("${roll(7)}") }
`
		if diags := checkWithPrelude(t, src); len(diags) != 0 {
			t.Errorf("a seeded draw should be legal in `det`, got: %v", diags)
		}
	})
	t.Run("ambient is refused in det", func(t *testing.T) {
		t.Parallel()
		const src = `
module main
let roll = det () -> i64 => random_below(6)
let main = () -> void => { println("${roll()}") }
`
		diags := checkWithPrelude(t, src)
		if len(diags) == 0 {
			t.Fatal("an ambient draw must be refused in `det`")
		}
		// The message must name *randomness*, not the generic input effect — the
		// whole point of EffectRand having its own bit is that the diagnostic can say
		// which non-determinism it found.
		if !strings.Contains(diags[0], "random source") {
			t.Errorf("diagnostic should name the random source, got: %s", diags[0])
		}
	})
}

// A builtin method (`x.wrapping_mul(y)`) is pure arithmetic and must be usable
// from `pure`/`det`/`noalloc` code.
//
// It was not, until 08/05: a builtin method call reaches the purity pass as a
// MemberExpr callee, whose dotted name (`x.wrapping_mul`) names nothing in any
// table, so it fell to the unresolved-callee default — AllEffects — and was charged
// as reading input *and* allocating. That made the explicit wrapping/saturating
// arithmetic unusable from exactly the code that wants it, which is how it surfaced:
// the prelude's `next_u64` could not be marked `det`. The typechecker now publishes
// the resolution (MethodTable.SetBuiltinMethod) instead of the checker re-deriving it.
func TestCheck_BuiltinMethodsArePure(t *testing.T) {
	t.Parallel()
	const src = `
module main
let mix = pure noalloc (x: u64) -> u64 => x.wrapping_mul(6364136223846793005)
let sat = pure noalloc (x: u8) -> u8 => x.saturating_add(200)
let flr = pure noalloc (x: f64) -> i64 => x.floor()
let main = () -> void => { println("${mix(3)} ${sat(100)} ${flr(1.5)}") }
`
	if diags := checkWithPrelude(t, src); len(diags) != 0 {
		t.Errorf("builtin methods should be pure, got: %v", diags)
	}
}
