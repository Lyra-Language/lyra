package llvm

import (
	"fmt"

	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/constant"
	"github.com/llir/llvm/ir/enum"
	lltypes "github.com/llir/llvm/ir/types"
	"github.com/llir/llvm/ir/value"

	"github.com/Lyra-Language/lyra/pkg/ast"
)

// The explicit escape hatches from checked-by-default arithmetic (trap.go):
// `x.wrapping_{add,sub,mul}(y)` and `x.saturating_{add,sub,mul}(y)`, the integer
// builtin methods registered in typechecker/builtins.go's intBinaryOps. Where
// plain `+`/`-`/`*` trap on overflow, these give the well-defined alternatives —
// modular two's-complement (wrapping) or clamp-to-bound (saturating) — for the
// definitional-wraparound cases (hashes, checksums, ring counters).

// intOverflowMethodBase maps each wrapping/saturating method name to the base
// arithmetic operation ("add"/"sub"/"mul") and whether it saturates. The
// receiver's Lyra type supplies signedness.
type intOverflowMethod struct {
	op       string // "add" | "sub" | "mul"
	saturate bool
}

var intOverflowMethods = map[string]intOverflowMethod{
	"wrapping_add":   {"add", false},
	"wrapping_sub":   {"sub", false},
	"wrapping_mul":   {"mul", false},
	"saturating_add": {"add", true},
	"saturating_sub": {"sub", true},
	"saturating_mul": {"mul", true},
}

// lowerIntOverflowMethod lowers a wrapping/saturating integer method call
// (`x.wrapping_add(y)`). The receiver and the single argument share the receiver's
// integer type `T` (the builtin signature is `(T) -> T`); signedness comes from
// the receiver's Lyra type.
func (l *lowerer) lowerIntOverflowMethod(block *ir.Block, call *ast.FunctionCallExpr, member *ast.MemberExpr, m intOverflowMethod) (value.Value, *ir.Block, error) {
	if len(call.Arguments) != 1 {
		return nil, nil, fmt.Errorf("llvm: %s() expects 1 argument, got %d", member.Property.Name, len(call.Arguments))
	}
	signed, err := l.getIntSignedness(member.Object)
	if err != nil {
		return nil, nil, err
	}
	left, block, err := l.lowerExpr(block, member.Object)
	if err != nil {
		return nil, nil, err
	}
	intTy, ok := left.Type().(*lltypes.IntType)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: %s() on a non-integer receiver (%s)", member.Property.Name, left.Type())
	}
	right, block, err := l.lowerExpr(block, call.Arguments[0])
	if err != nil {
		return nil, nil, err
	}
	// The argument is the same Lyra type T as the receiver, so it should already be
	// intTy; coerce defensively (e.g. an untyped literal arg that fell back to i64)
	// so the op never sees mismatched widths.
	right = coerceIntWidth(block, right, signed, intTy)

	if !m.saturate {
		return l.emitWrappingOp(block, m.op, left, right)
	}
	return l.emitSaturatingOp(block, m.op, left, right, signed, intTy)
}

// emitWrappingOp is modular two's-complement arithmetic — LLVM's plain
// add/sub/mul (no nsw/nuw), which wrap on overflow by definition. This is the raw
// op that plain `+`/`-`/`*` used before checked arithmetic put an overflow trap
// around them.
func (l *lowerer) emitWrappingOp(block *ir.Block, op string, left, right value.Value) (value.Value, *ir.Block, error) {
	switch op {
	case "add":
		return block.NewAdd(left, right), block, nil
	case "sub":
		return block.NewSub(left, right), block, nil
	case "mul":
		return block.NewMul(left, right), block, nil
	}
	return nil, nil, fmt.Errorf("llvm: unknown wrapping op %q", op)
}

// emitSaturatingOp clamps the result to the integer type's range on overflow
// instead of wrapping. add/sub use LLVM's saturating intrinsics
// (`llvm.{s,u}{add,sub}.sat`); mul has no such intrinsic, so it is built from the
// overflow-checked multiply plus a select to the saturation bound.
func (l *lowerer) emitSaturatingOp(block *ir.Block, op string, left, right value.Value, signed bool, intTy *lltypes.IntType) (value.Value, *ir.Block, error) {
	if op == "mul" {
		return l.emitSaturatingMul(block, left, right, signed, intTy)
	}
	fn := l.saturatingIntrinsic(op, signed, intTy)
	return block.NewCall(fn, left, right), block, nil
}

// saturatingIntrinsic lazily declares `llvm.{s,u}{add,sub}.sat.iN` (e.g.
// `llvm.uadd.sat.i8`), caching by name in the same map as the overflow-check
// intrinsics — both are `(iN, iN) -> iN` families keyed by full name.
func (l *lowerer) saturatingIntrinsic(op string, signed bool, intTy *lltypes.IntType) *ir.Func {
	sign := "u"
	if signed {
		sign = "s"
	}
	name := fmt.Sprintf("llvm.%s%s.sat.i%d", sign, op, intTy.BitSize)
	if fn, ok := l.overflowIntrinsics[name]; ok {
		return fn
	}
	fn := l.module.NewFunc(name, intTy, ir.NewParam("", intTy), ir.NewParam("", intTy))
	l.overflowIntrinsics[name] = fn
	return fn
}

// emitSaturatingMul clamps an integer multiply to the type's range. LLVM has no
// plain `llvm.{s,u}mul.sat` intrinsic (only the fixed-point `.fix.sat` variants),
// so it is composed: multiply with overflow detection, and on overflow select the
// saturation bound. Unsigned overflow always saturates to the max (all ones).
// Signed overflow saturates to the max when the true product is positive and the
// min when negative; the product's sign is the XOR of the operands' signs.
func (l *lowerer) emitSaturatingMul(block *ir.Block, left, right value.Value, signed bool, intTy *lltypes.IntType) (value.Value, *ir.Block, error) {
	intrinsic, err := l.overflowIntrinsic("mul", signed, intTy)
	if err != nil {
		return nil, nil, err
	}
	agg := block.NewCall(intrinsic, left, right)
	product := block.NewExtractValue(agg, 0)
	overflowed := block.NewExtractValue(agg, 1)

	if !signed {
		allOnes := constant.NewInt(intTy, -1) // all bits set = unsigned max
		return block.NewSelect(overflowed, allOnes, product), block, nil
	}

	// Signed: bound = (a < 0) ^ (b < 0) ? INT_MIN : INT_MAX.
	zero := constant.NewInt(intTy, 0)
	aNeg := block.NewICmp(enum.IPredSLT, left, zero)
	bNeg := block.NewICmp(enum.IPredSLT, right, zero)
	productNeg := block.NewXor(aNeg, bNeg)
	bound := block.NewSelect(productNeg, intMinConst(intTy), intMaxConst(intTy))
	return block.NewSelect(overflowed, bound, product), block, nil
}
