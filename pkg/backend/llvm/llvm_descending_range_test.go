package llvm

import (
	"strings"
	"testing"

	"github.com/Lyra-Language/lyra/pkg/driver"
)

// Descending ranges — `5..>1` and `5..>=1`.
//
// **Direction is the operator's, never the bounds'.** `5..<1` is an *ascending* range that
// happens to be empty, not a descending one; `1..>5` is a descending range that is empty.
// That is what makes the direction a parse-time fact, so a range with variable bounds
// cannot run the opposite way from the way it reads — which is why the inclusive end is
// spelled `..<=`/`..>=` rather than one operator inferring its direction from the values.
//
// Before this, a descending range was inexpressible and a negative step either did nothing
// or looped forever, depending on which way the bounds happened to point.

func TestExec_ForInDescendingRange(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, rng string
		want      int
	}{
		{"exclusive", "5..>1", 14},   // 5+4+3+2
		{"inclusive", "5..>=1", 15},  // 5+4+3+2+1
		{"stepped", "10..>=0:2", 30}, // 10+8+6+4+2+0
		{"single", "3..>=3", 3},
		{"empty exclusive", "3..>3", 0},
		{"empty wrong way", "1..>5", 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got := buildAndRun(t, "let main = () -> u8 => {\n  var s = 0\n  for i in "+c.rng+" { s = s + i }\n  u8(s)\n}")
			if got != c.want {
				t.Errorf("for i in %s: summed to %d, want %d", c.rng, got, c.want)
			}
		})
	}
}

// An **ascending** range whose bounds point the other way stays empty rather than
// quietly reversing. This is the assertion that fails if direction is ever taken from the
// operands instead of the operator.
func TestExec_AscendingRangeWithReversedBoundsIsEmpty(t *testing.T) {
	t.Parallel()
	got := buildAndRun(t, `
let main = () -> u8 => {
  var n = 0
  for i in 5..<1 { n = n + 1 }
  for i in 5..<=1 { n = n + 1 }
  u8(n + 7)
}`)
	if got != 7 {
		t.Errorf("expected 7 (both ascending ranges empty), got %d", got)
	}
}

// The same range as a comprehension source. The count is derived from the span measured
// *along* the direction, so the capacity still bounds the fill by construction.
func TestExec_ArrayCompDescendingRange(t *testing.T) {
	t.Parallel()
	got := buildAndRun(t, `
let main = () -> u8 => {
  let down = [x in 5..>=1 | x]
  let ex = [x in 5..>1 | x]
  let none = [x in 1..>5 | x]
  u8(down.len() * 10 + down[0] + ex.len() + none.len())
}`)
	if got != 59 {
		t.Errorf("expected 59 (len 5 → 50, first 5, then 4 and 0), got %d", got)
	}
}

// Descending with a guard, so the survivor count and the ordering both have to be right.
func TestExec_ArrayCompDescendingWithGuard(t *testing.T) {
	t.Parallel()
	got := buildAndRun(t, `
let main = () -> u8 => {
  let evens = [x in 10..>=1 | x % 2 == 0 | x]
  u8(evens.len() * 10 + evens[0] - 5)
}`)
	if got != 55 {
		t.Errorf("expected 55 (5 evens → 50, first is 10, minus 5), got %d", got)
	}
}

// An unsigned counter descends correctly: the predicate's signedness comes from the
// counter's type, not from the direction, so the last iteration does not depend on a wrap
// the author never wrote.
func TestExec_DescendingRangeOverUnsigned(t *testing.T) {
	t.Parallel()
	got := buildAndRun(t, `
let main = () -> u8 => {
  var s: u8 = 0
  let hi: u8 = 4
  let lo: u8 = 1
  for i in hi..>=lo { s = s + i }
  s
}`)
	if got != 10 {
		t.Errorf("expected 10 (4+3+2+1 over u8), got %d", got)
	}
}

// A negative step is refused now that the operator carries the direction: it was the one
// spelling that could still say "downwards", and in an ascending range it looped forever.
func TestAnalyze_NegativeStepIsRefused(t *testing.T) {
	t.Parallel()
	res := driver.Analyze([]byte(`
let main = () -> u8 => {
  var s = 0
  for i in 10..>=0:-2 { s = s + i }
  u8(s)
}`))
	if !res.HasErrors() {
		t.Fatal("expected a negative step to be refused")
	}
	var joined strings.Builder
	for _, d := range res.Errors() {
		joined.WriteString(d.Message)
	}
	if !strings.Contains(joined.String(), "distance, not a direction") {
		t.Errorf("expected the message to explain the step is a magnitude, got %q", joined.String())
	}
}
