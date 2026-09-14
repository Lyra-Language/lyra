package checker

import (
	"math"
	"math/rand"
	"testing"

	"github.com/Lyra-Language/lyra/pkg/ast"
)

// **The widened bounds contain the value an f32 or f64 program computes**, checked by brute
// force: random operand intervals, random operands inside them, each operation performed at
// the program's own width, and the result required to lie within the interval floatArith
// produced from the operands' bounds. An interval that is too narrow removes a trap the
// program needed, so this is the property the elision stands on.
func TestFloatInterval_WidenedBoundsContainTheResult(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	ops := []ast.MathBinaryOp{ast.MathBinaryOpAdd, ast.MathBinaryOpSub, ast.MathBinaryOpMul, ast.MathBinaryOpDiv}
	pick := func() float64 {
		switch r.Intn(4) {
		case 0:
			return float64(r.Intn(2001) - 1000)
		case 1:
			return (r.Float64() - 0.5) * 1e6
		case 2:
			return (r.Float64() - 0.5) * 1e-3
		default:
			return float64(int64(r.Uint64()>>12)) * (r.Float64() - 0.5)
		}
	}
	for i := 0; i < 200000; i++ {
		for _, width := range []int{32, 64} {
			a := outward(sorted(pick(), pick()), width)
			b := outward(sorted(pick(), pick()), width)
			op := ops[r.Intn(len(ops))]
			res, ok := floatArith(op, a, b, width)
			if !ok || !floatBounded(res) {
				continue
			}
			x := a.lo + (a.hi-a.lo)*r.Float64()
			y := b.lo + (b.hi-b.lo)*r.Float64()
			var got float64
			if width == 32 {
				x32, y32 := float32(x), float32(y)
				if float64(x32) < a.lo || float64(x32) > a.hi || float64(y32) < b.lo || float64(y32) > b.hi {
					continue // rounding the sample to f32 left its interval
				}
				got = float64(apply32(op, x32, y32))
			} else {
				got = apply64(op, x, y)
			}
			if math.IsNaN(got) || got < res.lo || got > res.hi {
				t.Fatalf("width %d: %v %v %v = %v, outside %v (from %v, %v)", width, x, op, y, got, res, a, b)
			}
		}
	}
}

// outward rounds an interval's bounds to the width, as every tracked interval is.
func outward(r floatInterval, width int) floatInterval {
	return floatInterval{roundDown(bigOf(r.lo), width), roundUp(bigOf(r.hi), width)}
}

func sorted(a, b float64) floatInterval {
	if a > b {
		a, b = b, a
	}
	return floatInterval{a, b}
}

//go:noinline
func apply64(op ast.MathBinaryOp, x, y float64) float64 {
	switch op {
	case ast.MathBinaryOpAdd:
		return x + y
	case ast.MathBinaryOpSub:
		return x - y
	case ast.MathBinaryOpMul:
		return x * y
	}
	return x / y
}

//go:noinline
func apply32(op ast.MathBinaryOp, x, y float32) float32 {
	switch op {
	case ast.MathBinaryOpAdd:
		return x + y
	case ast.MathBinaryOpSub:
		return x - y
	case ast.MathBinaryOpMul:
		return x * y
	}
	return x / y
}
