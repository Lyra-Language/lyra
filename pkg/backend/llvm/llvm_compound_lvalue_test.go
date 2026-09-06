package llvm

import (
	"strings"
	"testing"
)

// Compound assignment to a place that is not a binding — `xs[i].n += 1`, `p.x *= 2`.
//
// The collector accepted only an identifier on the left, so `counts[i].n += 1` was refused
// at collection ("left side of compound assignment must be an identifier") while
// `counts[i].n = counts[i].n + 1` on the identical place compiled. The compound form is the
// shorter spelling of that statement, so the restriction was a spelling rule rather than a
// rule about places.
//
// It is **not** implemented by desugaring into `place = place op rhs`, which is why
// TestExec_CompoundAssignmentEvaluatesThePathOnce exists: that spelling walks the path
// twice, so an index with a side effect would run it twice. The address is computed once
// (lvalueAddress, the same walk `=` uses) and read and written through.
func TestExec_CompoundAssignmentToAnLValue(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, src, want string }{
		{
			// The shape that motivated it: a counter held in an array of structs.
			name: "a struct field through an array index",
			src: `
struct Count { word: string, n: i64 }
let main = () -> void => {
  var counts: []Count = []
  counts.push(Count { word: "the", n: 1 })
  counts[0].n += 1
  counts[0].n *= 5
  println("${counts[0].word}=${counts[0].n}")
}`,
			want: "the=10",
		},
		{
			name: "a plain struct field",
			src: `
struct Pt { x: i64, y: i64 }
let main = () -> void => {
  var p = Pt { x: 3, y: 4 }
  p.x += 10
  p.y -= 1
  println("${p.x},${p.y}")
}`,
			want: "13,3",
		},
		{
			name: "an array element",
			src: `
let main = () -> void => {
  var xs: []i64 = [1, 2, 3]
  xs[1] += 100
  println(xs[1])
}`,
			want: "102",
		},
		{
			// A shift's count is typed independently of the target, so the *target's*
			// signedness is what picks `lshr` over `ashr`. Reading the count instead
			// answered 228 for this in the binding form; the interior form takes the
			// same care.
			name: "a shift through a field keeps the target's signedness",
			src: `
struct Reg { bits: u8 }
let main = () -> void => {
  var r = Reg { bits: 200 }
  r.bits >>= 1
  println(r.bits)
}`,
			want: "100",
		},
		{
			// An overloaded operator is reached the same way, since `a += b` is the
			// call `a = a + b` makes.
			name: "an overloaded operator through a nested path",
			src: `
trait Add { (_+_): (Self, Self) -> Self }
struct Vec2 { x: i64, y: i64 }
impl Add for Vec2 { (_+_) = (self, o) => Vec2 { x: self.x + o.x, y: self.y + o.y } }
struct Body { pos: Vec2 }
let main = () -> void => {
  var bs: []Body = [Body { pos: Vec2 { x: 1, y: 2 } }]
  bs[0].pos += Vec2 { x: 10, y: 20 }
  println("${bs[0].pos.x},${bs[0].pos.y}")
}`,
			want: "11,22",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := strings.TrimSpace(buildAndRunWithPrelude(t, c.src, "")); got != c.want {
				t.Errorf("got %q; want %q", got, c.want)
			}
		})
	}
}

// **The path is walked once.** A compound assignment reads and writes one place, and a
// desugaring to `place = place op rhs` would not: it evaluates the index expression twice,
// so a side-effecting one runs twice and an expensive one costs twice. Computing the
// address once is what makes the shorter spelling mean what it says.
func TestExec_CompoundAssignmentEvaluatesThePathOnce(t *testing.T) {
	t.Parallel()
	src := `
var calls = 0
let idx = () -> i64 => { calls = calls + 1; 0 }
let main = () -> void => {
  var xs: []i64 = [10, 20]
  xs[idx()] += 5
  println("value=${xs[0]} calls=${calls}")
}
`
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "value=15 calls=1" {
		t.Errorf("got %q; want %q", got, "value=15 calls=1")
	}
}
