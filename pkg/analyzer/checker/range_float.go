package checker

import (
	"math"
	"math/big"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// Float intervals, for one purpose: dropping the trap in front of a float→int conversion
// that cannot fail.
//
// `x.floor()`, `.ceil()` and `.round()` answer an i64, and `fptosi` is poison rather than
// saturating out of range, so the backend guards every one with a compare against ±2^63 and a
// branch to a trap (rounding.go). Until 09/13 this pass tracked integers only, so the guard
// stayed on a value bounded by construction — `(f64(i) / 10.0).floor()` for a loop counter
// `i` — and the integer the conversion produced was untracked too, taking every check after
// it with it.
//
// # Why the bounds are sound
//
// Only float literals, integer→float conversions of a tracked integer, float bindings holding
// one of those, negation and `+ - * /` are tracked. **Each bound is computed exactly and then
// rounded outward** — a lower bound down, an upper bound up, to the program's own width — which
// is ordinary interval arithmetic with directed rounding. The value at run time is the operation
// on operands inside their intervals, rounded to nearest; the exact operation is monotone on
// each corner, and rounding to nearest never goes below rounding down nor above rounding up, so
// the result lies inside. It holds whether or not the program's compiler fuses a multiply into
// an add, since a fused result is the exact expression rounded once. An exact bound stays exact,
// which is what lets `(f64(i) / 10.0).floor()` for a u8 start at 0 rather than -1. f16 is not
// tracked.
//
// **No bound may be infinite or near the width's range, and no divisor interval may contain
// zero.** With finite operands, `+ - *` produce neither infinity nor NaN unless they
// overflow, which the magnitude limit rules out; a division by an interval excluding zero is
// likewise finite. That is what lets a finite interval promise the value is not NaN — the
// case the guard also exists to catch.
type floatInterval struct{ lo, hi float64 }

// floatTrackLimit bounds every tracked magnitude, far inside f32's range (3.4e38) so no
// intermediate result can overflow to infinity, and far beyond any value an i64 conversion
// can accept.
const floatTrackLimit = 1e30

// floatIntervalOf answers the bounds of a float expression, or false when it is not one this
// pass can bound. It emits nothing and changes no state: an integer operand is evaluated
// quietly against a copy of the environment.
func (c *rangeChecker) floatIntervalOf(st rangeEnv, e ast.Expression) (floatInterval, bool) {
	width := c.floatWidth(e)
	if width == 0 {
		return floatInterval{}, false
	}
	var r floatInterval
	ok := false
	switch v := e.(type) {
	case *ast.FloatLiteralExpr:
		// Value is the source text rounded to f64; an f32 program rounds the text to f32,
		// which may differ from rounding the f64, so an f32 literal steps one value out first.
		lo, hi := v.Value, v.Value
		if width == 32 {
			lo, hi = math.Nextafter(lo, math.Inf(-1)), math.Nextafter(hi, math.Inf(1))
		}
		r, ok = floatInterval{roundDown(bigOf(lo), width), roundUp(bigOf(hi), width)}, true
	case *ast.IntegerLiteralExpr:
		if !v.Unsigned && !v.IsWide() {
			x := new(big.Float).SetInt64(v.Value)
			r, ok = floatInterval{roundDown(x, width), roundUp(x, width)}, true
		}
	case *ast.IdentifierExpr:
		r, ok = c.trackedFloat(st, v.Name)
	case *ast.NegationExpr:
		if inner, innerOK := c.floatIntervalOf(st, v.Operand); innerOK {
			r, ok = floatInterval{-inner.hi, -inner.lo}, true
		}
	case *ast.MathBinaryOpExpr:
		l, lok := c.floatIntervalOf(st, v.Left)
		rt, rok := c.floatIntervalOf(st, v.Right)
		if lok && rok {
			r, ok = floatArith(v.Operator, l, rt, width)
		}
	case *ast.FunctionCallExpr:
		r, ok = c.floatConversionInterval(st, v)
	}
	if !ok || !floatBounded(r) {
		return floatInterval{}, false
	}
	return r, true
}

// floatConversionInterval bounds `f64(x)` / `f32(x)`: a float operand's own bounds, or a
// tracked integer's converted. A call the typechecker resolved to a declaration is not a
// conversion, whatever it is named.
func (c *rangeChecker) floatConversionInterval(st rangeEnv, call *ast.FunctionCallExpr) (floatInterval, bool) {
	ident, ok := call.Function.(*ast.IdentifierExpr)
	if !ok || len(call.Arguments) != 1 {
		return floatInterval{}, false
	}
	target, isConversion := types.ConversionTargetName(ident.Name)
	if !isConversion || (target != types.Float32 && target != types.Float64) {
		return floatInterval{}, false
	}
	if fn, resolved := c.tt.Callee(call); resolved && fn != nil {
		return floatInterval{}, false
	}
	arg := call.Arguments[0]
	width := 64
	if target == types.Float32 {
		width = 32
	}
	if c.floatWidth(arg) != 0 {
		// Narrowing rounds to the target; widening is exact, and rounding outward to a wider
		// width changes nothing.
		inner, ok := c.floatIntervalOf(st, arg)
		if !ok {
			return floatInterval{}, false
		}
		return floatInterval{roundDown(bigOf(inner.lo), width), roundUp(bigOf(inner.hi), width)}, true
	}
	iv, tracked := c.quietInterval(st, arg)
	// A ±∞ sentinel is not a value: a u64's upper bound is one, and its true maximum does not
	// fit an int64 at all.
	if !tracked || iv.lo == negInf || iv.hi == posInf {
		return floatInterval{}, false
	}
	lo, hi := new(big.Float).SetInt64(iv.lo), new(big.Float).SetInt64(iv.hi)
	return floatInterval{roundDown(lo, width), roundUp(hi, width)}, true
}

// quietInterval is an integer expression's interval, evaluated against a copy of the
// environment with diagnostics and safety facts suppressed — this is a question asked a
// second time about an expression the walk has visited or will visit loudly.
func (c *rangeChecker) quietInterval(st rangeEnv, e ast.Expression) (interval, bool) {
	saved := c.silent
	c.silent = true
	iv, ok, _ := c.eval(st.clone(), e)
	c.silent = saved
	return iv, ok
}

// trackedFloat is tracked's float counterpart, under the same address-taken rule.
func (c *rangeChecker) trackedFloat(env rangeEnv, name string) (floatInterval, bool) {
	if c.addressTaken[name] {
		return floatInterval{}, false
	}
	fv, ok := env.floats[name]
	return fv, ok
}

// floatWidth is 64 or 32 for an expression of that float type, and 0 for anything else —
// f16 included, whose precision the widening below does not model.
func (c *rangeChecker) floatWidth(e ast.Expression) int {
	t, ok := c.tt.Get(e)
	if !ok {
		return 0
	}
	p, ok := types.StripNewtype(t).(types.PrimitiveType)
	if !ok {
		return 0
	}
	switch p.Name {
	case types.Float64, types.UntypedFloat:
		return 64
	case types.Float32:
		return 32
	}
	return 0
}

func floatArith(op ast.MathBinaryOp, a, b floatInterval, width int) (floatInterval, bool) {
	var lo, hi *big.Float
	corner := func(x, y float64) {
		v, ok := exactOp(op, x, y)
		if !ok {
			return
		}
		if lo == nil || v.Cmp(lo) < 0 {
			lo = v
		}
		if hi == nil || v.Cmp(hi) > 0 {
			hi = v
		}
	}
	switch op {
	case ast.MathBinaryOpAdd:
		corner(a.lo, b.lo)
		corner(a.hi, b.hi)
	case ast.MathBinaryOpSub:
		corner(a.lo, b.hi)
		corner(a.hi, b.lo)
	case ast.MathBinaryOpMul:
		corner(a.lo, b.lo)
		corner(a.lo, b.hi)
		corner(a.hi, b.lo)
		corner(a.hi, b.hi)
	case ast.MathBinaryOpDiv:
		if b.lo <= 0 && b.hi >= 0 {
			return floatInterval{}, false // a divisor that may be zero may give ±inf or NaN
		}
		corner(a.lo, b.lo)
		corner(a.lo, b.hi)
		corner(a.hi, b.lo)
		corner(a.hi, b.hi)
	default:
		return floatInterval{}, false
	}
	if lo == nil || hi == nil {
		return floatInterval{}, false
	}
	return floatInterval{roundDown(lo, width), roundUp(hi, width)}, true
}

// exactPrec is enough bits for the exact product of two 53-bit values and the exact sum of
// two within floatTrackLimit, with room to spare; a quotient is rarely exact at any
// precision, and is computed twice with directed rounding (see exactOp).
const exactPrec = 1024

// exactOp is x op y as a big.Float: exact for `+ - *`, and for `/` exact when the quotient
// fits exactPrec — otherwise rounded, and marked so by its accuracy, which roundDirected reads
// to step outward before rounding to the program's width.
func exactOp(op ast.MathBinaryOp, x, y float64) (*big.Float, bool) {
	bx, by := bigOf(x), bigOf(y)
	z := new(big.Float).SetPrec(exactPrec)
	switch op {
	case ast.MathBinaryOpAdd:
		return z.Add(bx, by), true
	case ast.MathBinaryOpSub:
		return z.Sub(bx, by), true
	case ast.MathBinaryOpMul:
		return z.Mul(bx, by), true
	case ast.MathBinaryOpDiv:
		if y == 0 {
			return nil, false
		}
		return z.Quo(bx, by), true
	}
	return nil, false
}

func bigOf(x float64) *big.Float { return new(big.Float).SetPrec(exactPrec).SetFloat64(x) }

// roundDown and roundUp round an exact value to the largest value of the width at or below
// it, and the smallest at or above it. A magnitude under 2^-100 is snapped to that bound
// rather than rounded, since a subnormal has less precision than the mantissa width models;
// a bound need not be representable at the width, only on the correct side.
func roundDown(x *big.Float, width int) float64 { return roundDirected(x, width, big.ToNegativeInf) }
func roundUp(x *big.Float, width int) float64   { return roundDirected(x, width, big.ToPositiveInf) }

func roundDirected(x *big.Float, width int, mode big.RoundingMode) float64 {
	tiny := new(big.Float).SetMantExp(big.NewFloat(1), -100)
	abs := new(big.Float).Abs(x)
	if x.Sign() != 0 && abs.Cmp(tiny) < 0 {
		t, _ := tiny.Float64()
		switch {
		case mode == big.ToNegativeInf && x.Sign() > 0:
			return 0
		case mode == big.ToNegativeInf:
			return -t
		case x.Sign() < 0:
			return 0
		default:
			return t
		}
	}
	prec := uint(53)
	if width == 32 {
		prec = 24
	}
	// A quotient at exactPrec may itself have been rounded; step it one unit outward first so
	// the directed rounding below starts on the correct side of the true value. An exact
	// value is left alone, so an exact bound stays exact.
	y := new(big.Float).SetPrec(exactPrec).Set(x)
	if x.Acc() != big.Exact {
		unit := new(big.Float).SetMantExp(big.NewFloat(1), x.MantExp(nil)-int(exactPrec)+1)
		if mode == big.ToNegativeInf {
			y.Sub(y, unit)
		} else {
			y.Add(y, unit)
		}
	}
	f, _ := new(big.Float).SetMode(mode).SetPrec(prec).Set(y).Float64()
	return f
}

// floatBounded reports a finite, ordered interval within floatTrackLimit — which also excludes
// NaN, since every comparison with one is false.
func floatBounded(r floatInterval) bool {
	return r.lo <= r.hi && r.lo >= -floatTrackLimit && r.hi <= floatTrackLimit
}

// evalRounding handles `x.floor()`, `.ceil()` and `.round()` on a float: it records the
// conversion as unable to fail when the receiver's bounds are well inside i64's range, and
// answers the integer result's interval. Called only for the builtin's shape (isRoundingCall
// on a float receiver, with no declaration resolved), so the receiver is its only child.
func (c *rangeChecker) evalRounding(st rangeEnv, call *ast.FunctionCallExpr) (interval, bool, rangeEnv) {
	member := call.Function.(*ast.MemberExpr)
	round := math.Round // half away from zero, as llvm.round is
	switch member.Property.Name {
	case "floor":
		round = math.Floor
	case "ceil":
		round = math.Ceil
	}
	before := st
	_, _, st = c.eval(st, member.Object)
	fv, bounded := c.floatIntervalOf(before, member.Object)
	// 2^62, not 2^63: the conversion's own limit is 2^63, and a margin that wide leaves no
	// question about the edge.
	const limit = 1 << 62
	if !bounded || fv.lo < -limit || fv.hi > limit {
		iv, tracked, after := c.typeIntervalIn(call, st)
		return iv, tracked, after
	}
	if !c.silent {
		c.safe.noFloatToIntTrap[call] = true
	}
	return interval{int64(round(fv.lo)), int64(round(fv.hi))}, true, st
}

// isBuiltinRounding reports the shape evalRounding handles: a no-argument `.floor()`,
// `.ceil()` or `.round()` on a float, resolved to no declaration — a method of that name on
// some other type, or one the program declared, is an ordinary call.
func (c *rangeChecker) isBuiltinRounding(call *ast.FunctionCallExpr) bool {
	member, ok := call.Function.(*ast.MemberExpr)
	if !ok || len(call.Arguments) != 0 {
		return false
	}
	switch member.Property.Name {
	case "floor", "ceil", "round":
	default:
		return false
	}
	if fn, resolved := c.tt.Callee(call); resolved && fn != nil {
		return false
	}
	return c.floatWidth(member.Object) != 0
}
