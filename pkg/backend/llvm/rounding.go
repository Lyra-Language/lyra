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
// (typechecker/builtins.go's floatUnaryMathOps) that have an LLVM intrinsic to lower to.
// They share the rounding builtins' shape — one float in, no arguments — and differ in
// what comes out: a float of the receiver's own width, so the result is the intrinsic's
// and there is no conversion to guard.
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
	"exp":   "exp",
	"exp2":  "exp2",
	"sin":   "sin",
	"cos":   "cos",
}

// floatMathLibmOps are the unary float-math builtins with **no intrinsic to lower to**,
// emitted as the libm call the intrinsic would have become anyway.
//
// **The split is a version floor, not a semantic distinction.** `llvm.tan`, `llvm.asin`,
// `llvm.acos`, `llvm.atan` and `llvm.atan2` only arrived in LLVM 19/20, and the emitted IR
// has to be accepted by the oldest clang this compiler's suite runs against — the ASan
// container pins **clang-15** on purpose (workspace CLAUDE.md), and an intrinsic it does
// not know is a parse error rather than a slow path. Naming the libm symbol directly is
// correct on every version instead of on the newest, costs the `-lm` that is already
// linked unconditionally, and is what LLVM lowers the intrinsic to on these targets.
//
// A future compiler that raises the floor can move a name from here to the table above
// with no other change; nothing outside this file knows which list a builtin is in.
//
// **The value is the `f64` symbol.** `f32` takes libm's `f` suffix and `f16` has no entry
// point at all — see emitLibmMathCall.
var floatMathLibmOps = map[string]string{
	"tan":  "tan",
	"asin": "asin",
	"acos": "acos",
	"atan": "atan",
}

// floatBinaryIntrinsicOps and floatBinaryLibmOps are that same split for the builtins
// that take an argument (typechecker/builtins.go's floatBinaryMathOps): `x.pow(y)` has
// `llvm.pow` as far back as the floor goes, `x.atan2(y)` does not.
var floatBinaryIntrinsicOps = map[string]string{
	"pow": "pow",
}

var floatBinaryLibmOps = map[string]string{
	"atan2": "atan2",
}

// floatMathOp is how one builtin name is lowered: which symbol, whether that symbol is an
// LLVM intrinsic or a bare libm entry point, and whether it takes an argument.
//
// It exists so the four tables above are read **once**, at the top of the lowering, rather
// than tested one after another down its body — the arity in particular has to be known
// before the arguments are checked, and a name found in one table and re-tested against
// the others is how two of them come to disagree (rule 8).
type floatMathOp struct {
	symbol string // the LLVM intrinsic's operation, or libm's f64 symbol
	libm   bool   // emit a direct libm call rather than an intrinsic
	binary bool   // takes one argument besides the receiver
}

// lookupFloatMathOp finds how name is lowered, across both arities and both emission
// kinds. `ok` is false for a name that is not a float-math builtin at all — including the
// rounding builtins, which the caller matches separately because their *return* differs.
func lookupFloatMathOp(name string) (floatMathOp, bool) {
	if op, ok := floatMathIntrinsicOps[name]; ok {
		return floatMathOp{symbol: op}, true
	}
	if op, ok := floatMathLibmOps[name]; ok {
		return floatMathOp{symbol: op, libm: true}, true
	}
	if op, ok := floatBinaryIntrinsicOps[name]; ok {
		return floatMathOp{symbol: op, binary: true}, true
	}
	if op, ok := floatBinaryLibmOps[name]; ok {
		return floatMathOp{symbol: op, libm: true, binary: true}, true
	}
	return floatMathOp{}, false
}

// lowerFloatMathMethod lowers the rounding builtins (`x.floor()`/`.ceil()`/`.round()`)
// and the float-math ones (`log`/`log2`/`log10`/`sqrt`/`exp`/`exp2`, the six trig
// functions, and the two that take an argument, `pow` and `atan2`).
//
// One function for all of them because they are one shape: a float receiver, zero or one
// float argument, one call. What separates them is the *return* — a float-math builtin
// answers a float of the receiver's own width, so the call's result is the answer; a
// rounding builtin answers i64, so it takes the guarded `fptosi` below.
//
// It is also the fallthrough of the builtin-method dispatcher, which is why an unmatched
// name is reported here: at this point the method resolved in the front end and matched
// nothing this backend lowers, so it is a gap to name rather than a program to blame
// (rule 5).
func (l *lowerer) lowerFloatMathMethod(block *ir.Block, call *ast.FunctionCallExpr, member *ast.MemberExpr) (value.Value, *ir.Block, error) {
	name := member.Property.Name
	roundingOp, isRounding := roundingIntrinsicOps[name]
	mathOp, isMath := lookupFloatMathOp(name)
	if !isRounding && !isMath {
		return nil, nil, fmt.Errorf("llvm: unsupported method call %q", name)
	}
	if isRounding {
		mathOp = floatMathOp{symbol: roundingOp}
	}
	wantArgs := 0
	if mathOp.binary {
		wantArgs = 1
	}
	if len(call.Arguments) != wantArgs {
		return nil, nil, fmt.Errorf("llvm: %s() expects %d arguments, got %d", name, wantArgs, len(call.Arguments))
	}
	recvT, ok := l.recordedType(member.Object)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: no type recorded for %s receiver", name)
	}
	recvP, ok := recvT.(types.PrimitiveType)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: %s() on non-primitive receiver %s not implemented", name, recvT)
	}
	suffix, ok := floatIntrinsicSuffix(recvP.Name)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: %s() on non-float receiver %s not implemented", name, recvT)
	}
	recv, block, err := l.lowerExpr(block, member.Object)
	if err != nil {
		return nil, nil, err
	}
	fT, ok := recv.Type().(*lltypes.FloatType)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: %s() lowered receiver is not a float value (%s)", name, recv.Type())
	}
	operands := []value.Value{recv}
	if mathOp.binary {
		arg, nextBlock, err := l.lowerExpr(block, call.Arguments[0])
		if err != nil {
			return nil, nil, err
		}
		block = nextBlock
		// The argument is the same Lyra type as the receiver — the builtin's signature is
		// `(T) -> T` — so it must be a float, and anything else is a front-end gap to name
		// rather than a value to bend (rule 5).
		if _, ok := arg.Type().(*lltypes.FloatType); !ok {
			return nil, nil, fmt.Errorf("llvm: %s() argument is %s, want a float", name, arg.Type())
		}
		// Its *width* is coerced defensively, exactly as the integer overflow builtins
		// coerce theirs (lowerIntOverflowMethod): an untyped literal argument settles to
		// f64 and is only checked for assignability, so `f32_value.pow(3.0)` arrives as a
		// double against a float receiver. Narrowing here is what the receiver's type
		// already said the argument was.
		operands = append(operands, coerceFloatWidth(block, arg, fT))
	}
	var result value.Value
	if mathOp.libm {
		result = l.emitLibmMathCall(block, mathOp.symbol, fT, operands)
	} else {
		result = block.NewCall(l.floatMathFunc("llvm."+mathOp.symbol+"."+suffix, fT, len(operands)), operands...)
	}
	if !isRounding {
		// A float-math builtin answers a float of the receiver's own width — the call's
		// result, unconverted. Outside its domain each gives IEEE's value rather than
		// trapping (`log(0)` is -inf, `log(-1)`, `sqrt(-1)` and `asin(2)` are NaN), which
		// is the answer the float operators already give; feeding any of them to an
		// integer conversion is what traps, and that is the right place for it.
		return result, block, nil
	}
	// The guard drops where the value-range pass proved the receiver finite and well inside
	// i64's range (checker/range_float.go) — the float counterpart of every other trap it
	// elides.
	if !l.res.RangeSafety.NoFloatToIntTrap(call) {
		block = l.guardFloatToInt(block, result, fT)
	}
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

// emitLibmMathCall calls libm's own entry point for op at width fT, and answers the result
// in fT.
//
// **Three widths, two entry points.** libm's double symbol is the bare name and its single
// is the `f` suffix, so `f64` and `f32` are one lookup each. **There is no half-precision
// libm at all**, so an `f16` receiver is widened to `f32`, computed there, and rounded
// back — which is what LLVM does for `llvm.sin.f16` on these targets, so the two paths
// agree on the answer. The double rounding cannot move a result: an f32 carries 24 bits of
// significand against f16's 11, far more than the two an intermediate would need to be
// wrong by.
func (l *lowerer) emitLibmMathCall(block *ir.Block, op string, fT *lltypes.FloatType, args []value.Value) value.Value {
	callT, symbol := fT, op
	switch fT.Kind {
	case lltypes.FloatKindFloat:
		symbol = op + "f"
	case lltypes.FloatKindHalf:
		callT, symbol = lltypes.Float, op+"f"
		widened := make([]value.Value, len(args))
		for i, a := range args {
			widened[i] = block.NewFPExt(a, lltypes.Float)
		}
		args = widened
	}
	result := block.NewCall(l.floatMathFunc(symbol, callT, len(args)), args...)
	if callT != fT {
		return block.NewFPTrunc(result, fT)
	}
	return result
}

// floatMathFunc lazily declares a float-math callee of arity `params` — an LLVM intrinsic
// (`llvm.floor.f64`) or a libm entry point (`tan`, `atan2f`) — caching it on the lowerer.
// The same lazy-declare-and-cache shape as memcmpFunc (strings.go), keyed by name in a map
// since there are dozens of these (a builtin per float width) instead of just one.
//
// Every parameter and the result are the same float type, which is true of every float
// builtin there is: the unary ones are `T -> T` and `pow`/`atan2` are `(T, T) -> T`. A
// future builtin with a mixed signature (`ldexp(T, i32)`) wants its own declaration rather
// than a `params` that has stopped meaning what it says.
func (l *lowerer) floatMathFunc(name string, fT *lltypes.FloatType, params int) *ir.Func {
	if fn, ok := l.floatMathFuncs[name]; ok {
		return fn
	}
	ps := make([]*ir.Param, params)
	for i := range ps {
		ps[i] = ir.NewParam("", fT)
	}
	fn := l.module.NewFunc(name, fT, ps...)
	l.floatMathFuncs[name] = fn
	return fn
}
