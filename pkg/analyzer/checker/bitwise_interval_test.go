package checker

import (
	"math"
	"testing"

	"github.com/Lyra-Language/lyra/pkg/ast"
)

// Soundness of the bitwise interval rules, checked by brute force rather than by
// argument.
//
// These intervals feed trap elision (checkArith's noOverflow), so an interval that
// is too *narrow* is a miscompile — the backend drops a check the program needed —
// while one that is too wide only costs precision. That asymmetry is why this is an
// exhaustive containment test over concrete values and not a set of hand-picked
// examples: it enumerates every value in every candidate interval, computes what the
// machine would actually produce (including the wrapping `<<` really does), and
// asserts the abstract answer contains it.

// intType is a concrete integer type's shape, enough to model both the value set
// the analysis can see and the truncation the hardware applies.
type intType struct {
	name   string
	width  int
	signed bool
	lo, hi int64
}

// Width 4, both signednesses. Small enough that the test below enumerates *every*
// interval and *every* value in it — a real exhaustive proof rather than a sample —
// and wide enough to exercise the truncation `<<` relies on. The rules are
// width-parametric (they read the type's bounds and the count limit, never a
// hard-coded width), so a hole at width 4 would be a hole at every width; the
// targeted tests further down cover the real widths at the boundaries that matter.
//
// Wider types are deliberately not enumerated here: an i16 interval spans 65,536
// values, and the cross product with every candidate interval pair does not finish.
var bitwiseTestTypes = []intType{
	{"i4", 4, true, -8, 7},
	{"u4", 4, false, 0, 15},
}

// truncTo reduces v to what a width-bit machine integer of this type would hold —
// the low bits, reinterpreted. This is what makes the `<<` case a real test: Lyra's
// shift wraps rather than trapping, so the concrete result is the truncated one.
func (t intType) truncTo(v int64) int64 {
	if t.width >= 64 {
		return v
	}
	mask := int64(1)<<uint(t.width) - 1
	r := v & mask
	if t.signed && r >= int64(1)<<uint(t.width-1) {
		r -= int64(1) << uint(t.width)
	}
	return r
}

// candidateBounds returns every value of the type, so the caller forms every
// possible interval over it.
func (t intType) candidateBounds() []int64 {
	var out []int64
	for v := t.lo; v <= t.hi; v++ {
		out = append(out, v)
	}
	return out
}

// concreteOp is what the machine produces for in-range operands, per Lyra's
// lowering: plain and/or/xor, a wrapping `shl`, and a `>>` that is arithmetic on a
// signed type and logical on an unsigned one.
func (t intType) concreteOp(op ast.MathBinaryOp, x, y int64) int64 {
	switch op {
	case ast.MathBinaryOpBitAnd:
		return t.truncTo(x & y)
	case ast.MathBinaryOpBitOr:
		return t.truncTo(x | y)
	case ast.MathBinaryOpBitXor:
		return t.truncTo(x ^ y)
	case ast.MathBinaryOpShl:
		return t.truncTo(x << uint(y))
	case ast.MathBinaryOpShr:
		if t.signed {
			return x >> uint(y) // Go's signed >> is arithmetic, matching ashr
		}
		// Unsigned values are non-negative, so an arithmetic shift of the
		// (non-negative) value is the logical shift.
		return x >> uint(y)
	}
	panic("unhandled op")
}

func TestBitwiseIntervalsAreSound(t *testing.T) {
	t.Parallel()
	ops := []ast.MathBinaryOp{
		ast.MathBinaryOpBitAnd,
		ast.MathBinaryOpBitOr,
		ast.MathBinaryOpBitXor,
		ast.MathBinaryOpShl,
		ast.MathBinaryOpShr,
	}
	for _, ty := range bitwiseTestTypes {
		for _, op := range ops {
			t.Run(ty.name+"_"+string(op), func(t *testing.T) {
				t.Parallel()
				checkOpSound(t, ty, op)
			})
		}
	}
}

func checkOpSound(t *testing.T, ty intType, op ast.MathBinaryOp) {
	t.Helper()
	tyIv := interval{ty.lo, ty.hi}
	bounds := ty.candidateBounds()

	// The right operand of a shift is a *count*: an out-of-range one traps, so no
	// value is produced and the interval owes it nothing. Only non-trapping counts
	// are enumerated below.
	isShift := op == ast.MathBinaryOpShl || op == ast.MathBinaryOpShr
	rightBounds := bounds
	if isShift {
		rightBounds = nil
		for v := int64(0); v < int64(ty.width); v++ {
			rightBounds = append(rightBounds, v)
		}
	}

	proved := 0
	for _, alo := range bounds {
		for _, ahi := range bounds {
			if alo > ahi {
				continue
			}
			for _, blo := range rightBounds {
				for _, bhi := range rightBounds {
					if blo > bhi {
						continue
					}
					a, b := interval{alo, ahi}, interval{blo, bhi}
					r, ok := bitwiseI(op, a, b, tyIv)
					if !ok {
						continue // widened to the type — always sound
					}
					got := clampToType(r, tyIv)
					proved++
					for x := alo; x <= ahi; x++ {
						for y := blo; y <= bhi; y++ {
							if isShift && (y < 0 || y >= int64(ty.width)) {
								continue // would trap; produces no value
							}
							v := ty.concreteOp(op, x, y)
							if v < got.lo || v > got.hi {
								t.Fatalf("UNSOUND %s: %s(%d..%d, %d..%d) = [%d,%d] but %d %s %d == %d",
									ty.name, op, alo, ahi, blo, bhi, got.lo, got.hi, x, op, y, v)
							}
						}
					}
				}
			}
		}
	}
	// Anti-vacuity: a rule that always gave up would pass the loop above having
	// checked nothing, which is exactly the failure mode this suite has hit before.
	if proved == 0 {
		t.Errorf("%s %s proved no interval at all — the rule is inert, not sound", ty.name, op)
	}
	t.Logf("%s %s: %d intervals proved", ty.name, op, proved)
}

// The precision that motivated the work: a mask bounds its result, so the addition
// that follows can be proved in range. Checked directly on the interval rule, since
// this is the property the elision depends on.
func TestMaskBoundsResult(t *testing.T) {
	t.Parallel()
	u8 := interval{0, 255}
	// x is anything a u8 can hold; the mask is the constant 0x0F.
	got, ok := andI(u8, interval{15, 15})
	if !ok {
		t.Fatal("andI gave up on a constant mask")
	}
	if got.lo != 0 || got.hi != 15 {
		t.Errorf("x & 0x0F = [%d,%d]; want [0,15]", got.lo, got.hi)
	}
	// ...and it holds when the masked value is signed and may be negative, which is
	// the case a "both operands non-negative" rule would have missed.
	got, ok = andI(interval{-128, 127}, interval{15, 15})
	if !ok || got.lo != 0 || got.hi != 15 {
		t.Errorf("signed x & 0x0F = [%d,%d] (ok=%v); want [0,15]", got.lo, got.hi, ok)
	}
}

// A ±∞ sentinel bound is not a value. Computing with one as though it were would be
// unsound for u64, whose upper bound is +∞ because its true maximum does not fit in
// an int64.
func TestBitwiseRulesRespectSentinels(t *testing.T) {
	t.Parallel()
	u64 := interval{0, posInf}
	i64 := interval{negInf, posInf}

	// AND against an unbounded-above operand still proves non-negativity, which is
	// strictly better than the type's range for a signed result.
	if got, ok := andI(i64, u64); !ok || got.lo != 0 {
		t.Errorf("andI(i64, u64) = [%d,%d] ok=%v; want a non-negative lower bound", got.lo, got.hi, ok)
	}
	// OR and XOR compute with the upper bound, so they must refuse an infinite one
	// rather than treat MaxInt64 as a real maximum.
	if _, ok := orI(u64, u64); ok {
		t.Error("orI accepted a +∞ upper bound; it computes with it, so it must refuse")
	}
	if _, ok := xorI(u64, u64); ok {
		t.Error("xorI accepted a +∞ upper bound")
	}
	// A shift count of +∞ is likewise not a count.
	if _, ok := shlI(interval{0, 1}, u64, u64); ok {
		t.Error("shlI accepted a +∞ shift count")
	}
	if _, ok := shrI(interval{0, 1}, u64, u64); ok {
		t.Error("shrI accepted a +∞ shift count")
	}
}

// `<<` wraps rather than trapping, so an interval is only claimed when the
// mathematical product cannot leave the type. A shift that could drop bits must
// widen instead — claiming the product's range would assert a bound the wrapped
// value need not satisfy.
func TestShiftLeftRefusesWhenItCouldWrap(t *testing.T) {
	t.Parallel()
	u8 := interval{0, 255}
	if got, ok := shlI(interval{0, 3}, interval{2, 2}, u8); !ok || got.lo != 0 || got.hi != 12 {
		t.Errorf("(0..3) << 2 = [%d,%d] ok=%v; want [0,12]", got.lo, got.hi, ok)
	}
	if _, ok := shlI(interval{0, 255}, interval{4, 4}, u8); ok {
		t.Error("(0..255) << 4 cannot be bounded in a u8 — it wraps — so shlI must refuse")
	}
}

// allOnesAtLeast must never overflow into a negative "ceiling", which would invert
// the interval and make every containment test vacuous.
func TestAllOnesAtLeastGuardsOverflow(t *testing.T) {
	t.Parallel()
	for _, m := range []int64{0, 1, 15, 16, math.MaxInt32} {
		got, ok := allOnesAtLeast(m)
		if !ok {
			t.Errorf("allOnesAtLeast(%d) gave up", m)
			continue
		}
		if got < m || got < 0 {
			t.Errorf("allOnesAtLeast(%d) = %d; must be >= m and non-negative", m, got)
		}
	}
	if _, ok := allOnesAtLeast(math.MaxInt64); ok {
		t.Error("allOnesAtLeast(MaxInt64) must refuse rather than overflow")
	}
}
