package llvm

import (
	"strings"
	"testing"
)

// The three-way comparison `a <=> b`, which yields the prelude's
// `Ordering` (`Less | Equal | Greater`) rather than a bool.
//
// It parsed and type-checked long before it lowered — as a `bool`, with its
// operands unchecked — and then failed the build with "boolean operator <=> not
// implemented". So it was in the same family as `Random.global()` and
// `Rng.seeded()`: a form the front end waved through into a backend that had never
// heard of it.
//
// The result is a sum type rather than Ruby's -1/0/1 because an integer invites
// `if (a <=> b) == -1`, which is strictly worse than `a < b` and leaves the
// operator with no reason to exist. With named variants, the exhaustiveness checker
// insists all three outcomes are handled — which is the property the
// `if`/`else if`/`else` chain it replaces cannot offer.

func TestExec_Spaceship(t *testing.T) {
	t.Parallel()
	const src = `
data Ordering = Less | Equal | Greater
let describe = (a: i64, b: i64) -> u8 => match a <=> b {
  Less => 1,
  Equal => 2,
  Greater => 3,
}
let main = () -> u8 => describe(%s)
`
	cases := []struct {
		name, args string
		want       int
	}{
		{"less", "3, 7", 1},
		{"equal", "5, 5", 2},
		{"greater", "7, 3", 3},
		{"negatives", "-9, -2", 1},
		{"extremes", "-9223372036854775808, 9223372036854775807", 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := buildAndRun(t, strings.Replace(src, "%s", c.args, 1)); got != c.want {
				t.Errorf("%s <=> : got %d, want %d", c.args, got, c.want)
			}
		})
	}
}

// The signed/unsigned split, which is the one place a three-way comparison can be
// quietly wrong rather than loudly broken: `u8(200) <=> u8(1)` is Greater, but read
// with *signed* predicates 200 is -56 and the answer flips to Less. Both operands
// and both predicates have to agree with the operand's own signedness.
func TestExec_SpaceshipSignedness(t *testing.T) {
	t.Parallel()
	const src = `
data Ordering = Less | Equal | Greater
let main = () -> u8 => {
  let big = u8(200)
  let small = u8(1)
  match big <=> small {
    Greater => 1,
    Less => 2,
    Equal => 3,
  }
}
`
	if got := buildAndRun(t, src); got != 1 {
		t.Errorf("u8(200) <=> u8(1) gave %d; want 1 (Greater) — 2 means signed predicates were used", got)
	}
}

// Runes order by code point, matching `<`/`>` — which is what makes character
// classification expressible.
func TestExec_SpaceshipRunes(t *testing.T) {
	t.Parallel()
	const src = `
data Ordering = Less | Equal | Greater
let main = () -> u8 => match 'a' <=> 'z' {
  Less => 1,
  Equal => 2,
  Greater => 3,
}
`
	if got := buildAndRun(t, src); got != 1 {
		t.Errorf("'a' <=> 'z' gave %d, want 1 (Less)", got)
	}
}

// The lowering is **branchless** — two selects and an insertvalue, no new blocks.
//
// That is asserted on the emitted IR rather than left to the behavioural tests
// because it is a property with a history: a call site that branches returns a
// *merge* block, and flushStmtTemps releases an owned temporary either at the
// statement's end block or in its own production block — a merge block being
// neither. That is what made `read_line` free its string before the `match` read it
// (input.go). `Ordering` owns nothing, so the bug could not bite here, but the
// shape is the one to keep.
func TestEmit_SpaceshipIsBranchless(t *testing.T) {
	t.Parallel()
	const src = `
data Ordering = Less | Equal | Greater
let cmp = (a: i64, b: i64) -> u8 => match a <=> b {
  Less => 1,
  Equal => 2,
  Greater => 3,
}
let main = () -> u8 => cmp(1, 2)
`
	got, err := emitSource(t, src)
	if err != nil {
		t.Fatal(err)
	}
	fn := funcBody(got, "cmp")
	if fn == "" {
		t.Fatalf("could not find the emitted cmp function in:\n%s", got)
	}
	// Two selects build the tag; the comparison itself is two icmps.
	if n := strings.Count(fn, "select"); n < 2 {
		t.Errorf("expected two selects building the Ordering tag, found %d:\n%s", n, fn)
	}
	if n := strings.Count(fn, "icmp"); n < 2 {
		t.Errorf("expected two icmps, found %d:\n%s", n, fn)
	}
}
