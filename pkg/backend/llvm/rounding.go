package llvm

import (
	"fmt"
	"math"

	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/constant"
	"github.com/llir/llvm/ir/enum"
	lltypes "github.com/llir/llvm/ir/types"
	"github.com/llir/llvm/ir/value"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// roundingIntrinsicOps maps a Lyra builtin rounding method name
// (typechecker/builtins.go's floatRoundingOps) to the LLVM intrinsic that
// implements it. `round` uses llvm.round (half away from zero, C/Rust-style),
// not llvm.rint/nearbyint (round-to-even, mode-dependent).
var roundingIntrinsicOps = map[string]string{
	"floor": "floor",
	"ceil":  "ceil",
	"round": "round",
}

// floatMathIntrinsicOps maps the unary float-math builtins
// (typechecker/builtins.go's floatUnaryMathOps) to their LLVM intrinsics. They share the
// rounding builtins' shape — one float in, no arguments — and differ in what comes out: a
// float of the receiver's own width, so the result is the intrinsic's and there is no
// conversion to guard.
//
// The Lyra name and the intrinsic name coincide for every one of them, which is why this
// is a set spelled as a map: it stays a map so a future builtin whose spellings differ
// (`ln` over `llvm.log`, say) needs no restructuring.
//
// LLVM lowers each to the libm call of the same name, which is why `lyrac build` links
// `-lm` unconditionally — `sqrt` is the one that often becomes a hardware instruction
// instead, on every target this compiles for.
var floatMathIntrinsicOps = map[string]string{
	"log":   "log",
	"log2":  "log2",
	"log10": "log10",
	"sqrt":  "sqrt",
}

// lowerFloatMathMethod lowers the rounding builtins (`x.floor()`/`.ceil()`/`.round()`)
// and the unary float-math ones (`log`/`log2`/`log10`/`sqrt`).
//
// One function for both because they are one shape: a receiver, no arguments, one LLVM
// intrinsic. What separates them is the *return* — a float-math builtin answers a float of
// the receiver's own width, so the intrinsic's result is the answer; a rounding builtin
// answers i64, so it takes the guarded `fptosi` below.
//
// It is also the fallthrough of the builtin-method dispatcher, which is why an unmatched
// name is reported here: at this point the method resolved in the front end and matched
// nothing this backend lowers, so it is a gap to name rather than a program to blame
// (rule 5).
func (l *lowerer) lowerFloatMathMethod(block *ir.Block, call *ast.FunctionCallExpr, member *ast.MemberExpr) (value.Value, *ir.Block, error) {
	op, isRounding := roundingIntrinsicOps[member.Property.Name]
	if mathOp, isMath := floatMathIntrinsicOps[member.Property.Name]; isMath {
		op = mathOp
	} else if !isRounding {
		return nil, nil, fmt.Errorf("llvm: unsupported method call %q", member.Property.Name)
	}
	if len(call.Arguments) != 0 {
		return nil, nil, fmt.Errorf("llvm: %s() expects 0 arguments, got %d", member.Property.Name, len(call.Arguments))
	}
	recvT, ok := l.recordedType(member.Object)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: no type recorded for %s receiver", member.Property.Name)
	}
	recvP, ok := recvT.(types.PrimitiveType)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: %s() on non-primitive receiver %s not implemented", member.Property.Name, recvT)
	}
	suffix, ok := floatIntrinsicSuffix(recvP.Name)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: %s() on non-float receiver %s not implemented", member.Property.Name, recvT)
	}
	recv, block, err := l.lowerExpr(block, member.Object)
	if err != nil {
		return nil, nil, err
	}
	fT, ok := recv.Type().(*lltypes.FloatType)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: %s() lowered receiver is not a float value (%s)", member.Property.Name, recv.Type())
	}
	result := block.NewCall(l.roundingIntrinsicFunc(op, suffix, fT), recv)
	if !isRounding {
		// A unary float-math builtin answers a float of the receiver's own width — the
		// intrinsic's result, unconverted. Outside its domain each gives IEEE's value
		// rather than trapping (`log(0)` is -inf, `log(-1)` and `sqrt(-1)` are NaN),
		// which is the answer the float operators already give; feeding either to an
		// integer conversion is what traps, and that is the right place for it.
		return result, block, nil
	}
	block = l.guardFloatToInt(block, result, fT)
	// The builtin's fixed return type is i64 (builtins.go's floatRoundingOps);
	// narrow further with an explicit int conversion, e.g. i32(x.floor()).
	return block.NewFPToSI(result, lltypes.I64), block, nil
}

// guardFloatToInt traps unless v is a number an i64 can hold, and returns the block to
// carry on in. Emitted before every `fptosi`, which is poison out of range rather than
// saturating (see panicFloatToIntFunc).
//
// **The bounds are `-2^63 <= v < 2^63`, and the asymmetry is not a slip.** i64's minimum
// is exactly -2^63 and representable as a float; its *maximum* is 2^63-1, which is **not**
// representable in binary64 — the nearest float above 2^63-1 is 2^63 itself. So a `<=`
// against a float spelled `9223372036854775807` would compare against 2^63 and admit a
// value one past the end. The exclusive upper bound is exact in every float width.
//
// **A NaN traps here too, and for free.** The check is written as "trap unless in range"
// using *ordered* comparisons, which are false for a NaN — so it takes the trap edge
// without a test of its own. Written the other way round (`trap if v < lo || v >= hi`,
// with unordered compares) a NaN would slip through to the conversion, which is poison for
// it as well.
func (l *lowerer) guardFloatToInt(block *ir.Block, v value.Value, fT *lltypes.FloatType) *ir.Block {
	// 2^63 exactly, in the receiver's own width. f16 cannot reach it (its maximum is
	// 65504) and f32 represents it exactly, being a power of two — so one constant
	// serves all three widths without a per-width table.
	limit := constant.NewFloat(fT, math.Ldexp(1, 63))
	negLimit := constant.NewFloat(fT, -math.Ldexp(1, 63))
	aboveMin := block.NewFCmp(enum.FPredOGE, v, negLimit)
	belowMax := block.NewFCmp(enum.FPredOLT, v, limit)
	inRange := block.NewAnd(aboveMin, belowMax)
	outOfRange := block.NewXor(inRange, constant.NewInt(lltypes.I1, 1))
	return l.emitTrapIf(block, outOfRange, l.panicFloatToIntFunc())
}

// roundingIntrinsicFunc lazily declares `llvm.<op>.<suffix>` (e.g.
// `llvm.floor.f64`), caching it on the lowerer — the same lazy-declare-and-
// cache shape as memcmpFunc (strings.go), keyed by name in a map since there
// are up to 9 of these (3 ops x 3 float widths) instead of just one.
func (l *lowerer) roundingIntrinsicFunc(op, suffix string, fT *lltypes.FloatType) *ir.Func {
	name := "llvm." + op + "." + suffix
	if fn, ok := l.roundingIntrinsics[name]; ok {
		return fn
	}
	fn := l.module.NewFunc(name, fT, ir.NewParam("", fT))
	l.roundingIntrinsics[name] = fn
	return fn
}
