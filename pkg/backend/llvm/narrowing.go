package llvm

import (
	"fmt"
	"math/big"

	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/constant"
	"github.com/llir/llvm/ir/enum"
	lltypes "github.com/llir/llvm/ir/types"
	"github.com/llir/llvm/ir/value"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
)

func isNarrowingMethod(name string) bool {
	return name == "truncate" || name == "saturate" || name == "narrow"
}

// lowerNarrowingMethod lowers `x.truncate()`, `x.saturate()` and `x.narrow()`, whose target is
// the call's recorded type (typechecker/narrowing.go).
//
// **Compared in the receiver's type**, against the part of the target's range the receiver's
// can reach, so the bounds are always representable and a side the receiver cannot pass needs
// no test at all: an `i8` saturated to a `u16` has no top to clamp.
func (l *lowerer) lowerNarrowingMethod(block *ir.Block, call *ast.FunctionCallExpr, member *ast.MemberExpr) (value.Value, *ir.Block, error) {
	name := member.Property.Name
	recv, block, err := l.lowerExpr(block, member.Object)
	if err != nil {
		return nil, nil, err
	}
	if diverged(recv, block) {
		return nil, block, nil
	}
	source, target, err := l.narrowingTypes(call, member)
	if err != nil {
		return nil, nil, err
	}
	sourceTy, ok := recv.Type().(*lltypes.IntType)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: %s() on a non-integer receiver (%s)", name, recv.Type())
	}
	targetLL, ok := LLVMPrimitive(target)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: %s() has no representation for its target %s", name, target)
	}
	targetTy, ok := targetLL.(*lltypes.IntType)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: %s() has a non-integer target %s", name, target)
	}
	if sourceFormat, ok := types.FixedFormatOf(source); ok {
		return l.lowerFixedNarrowing(block, call, name, recv, sourceFormat, target)
	}
	signed := IsSignedInt(source)
	if name == "truncate" {
		return coerceIntWidth(block, recv, signed, targetTy), block, nil
	}

	fit, err := fitBounds(source, target, sourceTy)
	if err != nil {
		return nil, nil, fmt.Errorf("llvm: %s(): %w", name, err)
	}
	if name == "saturate" {
		return coerceIntWidth(block, fit.clamp(block, recv, signed), signed, targetTy), block, nil
	}
	if !l.res.RangeSafety.NoNarrowTrap(call) {
		if outside := fit.outside(block, recv, signed); outside != nil {
			block = l.emitTrapIf(block, outside, l.narrowOverflowFunc())
		}
	}
	return coerceIntWidth(block, recv, signed, targetTy), block, nil
}

func (l *lowerer) narrowingTypes(call *ast.FunctionCallExpr, member *ast.MemberExpr) (source, target types.PrimitiveTypeName, err error) {
	name := member.Property.Name
	sourceT, ok := l.recordedType(member.Object)
	if !ok {
		return "", "", fmt.Errorf("llvm: no type recorded for the receiver of %s()", name)
	}
	targetT, ok := l.recordedType(call)
	if !ok {
		return "", "", fmt.Errorf("llvm: no type recorded for the result of %s()", name)
	}
	sourceP, ok := sourceT.(types.PrimitiveType)
	if !ok {
		return "", "", fmt.Errorf("llvm: %s() on a non-integer receiver (%s)", name, sourceT)
	}
	targetP, ok := targetT.(types.PrimitiveType)
	if !ok {
		return "", "", fmt.Errorf("llvm: %s() with a non-integer target (%s)", name, targetT)
	}
	return sourceP.Name, targetP.Name, nil
}

// bounds is the part of a target's range a source can reach, in the source's own type. A nil
// bound is one the source cannot pass.
type bounds struct {
	low, high *constant.Int
}

func fitBounds(source, target types.PrimitiveTypeName, sourceTy *lltypes.IntType) (bounds, error) {
	sourceLo, sourceHi, ok := types.IntegerRange(source)
	if !ok {
		return bounds{}, fmt.Errorf("no range for %s", source)
	}
	targetLo, targetHi, ok := types.IntegerRange(target)
	if !ok {
		return bounds{}, fmt.Errorf("no range for %s", target)
	}
	var fit bounds
	if targetLo.Cmp(sourceLo) > 0 {
		fit.low = &constant.Int{Typ: sourceTy, X: new(big.Int).Set(targetLo)}
	}
	if targetHi.Cmp(sourceHi) < 0 {
		fit.high = &constant.Int{Typ: sourceTy, X: new(big.Int).Set(targetHi)}
	}
	return fit, nil
}

func (b bounds) clamp(block *ir.Block, v value.Value, signed bool) value.Value {
	less, greater := predicates(signed)
	if b.low != nil {
		v = block.NewSelect(block.NewICmp(less, v, b.low), b.low, v)
	}
	if b.high != nil {
		v = block.NewSelect(block.NewICmp(greater, v, b.high), b.high, v)
	}
	return v
}

// outside answers whether v is past either bound, or nil when it cannot be.
func (b bounds) outside(block *ir.Block, v value.Value, signed bool) value.Value {
	less, greater := predicates(signed)
	var below, above value.Value
	if b.low != nil {
		below = block.NewICmp(less, v, b.low)
	}
	if b.high != nil {
		above = block.NewICmp(greater, v, b.high)
	}
	switch {
	case below != nil && above != nil:
		return block.NewOr(below, above)
	case below != nil:
		return below
	}
	return above
}

func predicates(signed bool) (less, greater enum.IPred) {
	if signed {
		return enum.IPredSLT, enum.IPredSGT
	}
	return enum.IPredULT, enum.IPredUGT
}
