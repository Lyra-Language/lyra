package llvm

import (
	"fmt"

	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/constant"
	"github.com/llir/llvm/ir/enum"
	lltypes "github.com/llir/llvm/ir/types"
	"github.com/llir/llvm/ir/value"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
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

// checkedIntOps are the `checked_*` family: the operation's result as `Some(v)`, or
// `None` where it would have overflowed. `div` is not an intrinsic op — its two
// failures are a zero divisor and `INT_MIN / -1`, the two cases `/` traps on — so it is
// handled separately below.
var checkedIntOps = map[string]string{
	"checked_add": "add",
	"checked_sub": "sub",
	"checked_mul": "mul",
	"checked_div": "div",
}

// lowerCheckedIntMethod lowers `x.checked_add(y)` and friends to a `Maybe<T>`.
//
// **Branchless**, which is worth the small effort: the with-overflow intrinsic already
// hands back `{ result, overflowed }`, so the union is two `select`s and an insert. A
// branching call site returns a merge block, and while `flushStmtTemps` handles that
// correctly since 08/08, an owned builtin result that behaves exactly like an ordinary
// value is still the cheaper thing to reason about — the rule `read_line` and `<=>`
// both follow.
//
// The payload of the `None` arm is left as the (meaningless) wrapped result rather than
// zeroed. That is not laziness: a nullary variant's payload blob is *undef* by
// DATA_LAYOUT.md, so nothing may read it, and selecting a zero would cost an
// instruction to establish a value no correct program can observe.
func (l *lowerer) lowerCheckedIntMethod(block *ir.Block, call *ast.FunctionCallExpr, member *ast.MemberExpr, op string) (value.Value, *ir.Block, error) {
	if len(call.Arguments) != 1 {
		return nil, nil, fmt.Errorf("llvm: %s() expects 1 argument, got %d", member.Property.Name, len(call.Arguments))
	}
	recorded, ok := l.recordedType(call)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: %s() call has no recorded type", member.Property.Name)
	}
	dt, ok := recorded.(types.DataType)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: %s() must return a Maybe, got %s", member.Property.Name, recorded)
	}
	someC, someTag, hasSome := findConstructor(dt, "Some")
	noneC, noneTag, hasNone := findConstructor(dt, "None")
	if !hasSome || !hasNone {
		return nil, nil, fmt.Errorf("llvm: %s()'s return type %q is not a canonical Maybe", member.Property.Name, dt.Name)
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
	right = coerceIntWidth(block, right, signed, intTy)

	var result value.Value
	var failed value.Value
	if op == "div" {
		result, failed = l.emitCheckedDiv(block, left, right, signed, intTy)
	} else {
		fn, err := l.overflowIntrinsic(op, signed, intTy)
		if err != nil {
			return nil, nil, err
		}
		pair := block.NewCall(fn, left, right)
		result = block.NewExtractValue(pair, 0)
		failed = block.NewExtractValue(pair, 1)
	}

	some, err := l.buildDataValue(block, dt, someTag, someC, []value.Value{result})
	if err != nil {
		return nil, nil, err
	}
	none, err := l.buildDataValue(block, dt, noneTag, noneC, nil)
	if err != nil {
		return nil, nil, err
	}
	return block.NewSelect(failed, none, some), block, nil
}

// emitCheckedDiv computes `left / right` without ever executing an undefined division,
// and reports whether the division was refused.
//
// LLVM's `sdiv`/`udiv` are **undefined behaviour** on a zero divisor, and signed
// division is undefined on `INT_MIN / -1` as well — the same two cases `/` traps on. So
// the divisor is replaced by 1 on those paths and the (meaningless) quotient discarded
// by the caller's select. Substituting rather than branching is what keeps the whole
// lowering branchless; the cost is one extra select on a path that was going to compute
// a division anyway.
func (l *lowerer) emitCheckedDiv(block *ir.Block, left, right value.Value, signed bool, intTy *lltypes.IntType) (result, failed value.Value) {
	zero := constant.NewInt(intTy, 0)
	failed = block.NewICmp(enum.IPredEQ, right, zero)
	if signed {
		// INT_MIN / -1 has no representable quotient: its true value is INT_MAX+1.
		// intMinConst is the shared spelling — this rebuilt the same value by hand
		// twenty lines from a call to it.
		negOne := constant.NewInt(intTy, -1)
		isMin := block.NewICmp(enum.IPredEQ, left, intMinConst(intTy))
		isNegOne := block.NewICmp(enum.IPredEQ, right, negOne)
		failed = block.NewOr(failed, block.NewAnd(isMin, isNegOne))
	}
	safe := block.NewSelect(failed, constant.NewInt(intTy, 1), right)
	if signed {
		return block.NewSDiv(left, safe), failed
	}
	return block.NewUDiv(left, safe), failed
}
