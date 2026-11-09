package llvm

import (
	"fmt"
	"math"
	"math/big"

	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/constant"
	"github.com/llir/llvm/ir/enum"
	lltypes "github.com/llir/llvm/ir/types"
	"github.com/llir/llvm/ir/value"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// A fixed-point value lowers as its storage integer, and `+`, `-`, `%`, comparison and negation
// are that integer's: both operands have one scale. What differs is what a scale changes.

func isFixedName(name types.PrimitiveTypeName) bool {
	_, ok := types.FixedFormatOf(name)
	return ok
}

// fixedOf answers the format of e's recorded type.
func (l *lowerer) fixedOf(e ast.Expression) (types.FixedFormat, bool) {
	t, ok := l.recordedType(e)
	if !ok {
		return types.FixedFormat{}, false
	}
	return types.AsFixed(t)
}

// fixedLiteral lowers a literal recorded at a fixed type to its constant, negated if asked,
// rounding to nearest. isFixed is false for any other expression. A literal the typechecker
// should have refused is an error here rather than a wrapped constant.
func (l *lowerer) fixedLiteral(e ast.Expression, negate bool) (c value.Value, isFixed bool, err error) {
	format, ok := l.fixedOf(e)
	if !ok {
		return nil, false, nil
	}
	var magnitude *big.Rat
	switch v := e.(type) {
	case *ast.IntegerLiteralExpr:
		if v.IsWide() {
			magnitude = new(big.Rat).SetInt(v.BigValue())
		} else if v.Unsigned {
			magnitude = new(big.Rat).SetInt(new(big.Int).SetUint64(uint64(v.Value)))
		} else {
			magnitude = new(big.Rat).SetInt64(v.Value)
		}
	case *ast.FloatLiteralExpr:
		magnitude = new(big.Rat).SetFloat64(v.Value)
	default:
		return nil, false, nil
	}
	if negate {
		magnitude.Neg(magnitude)
	}
	raw, fits := format.LiteralRaw(magnitude)
	if !fits {
		return nil, true, fmt.Errorf("llvm: literal %s does not fit %s", magnitude.FloatString(4), types.Fixed(format))
	}
	return &constant.Int{Typ: storageType(format), X: raw}, true, nil
}

func storageType(format types.FixedFormat) *lltypes.IntType {
	ll, _ := LLVMPrimitive(format.Storage())
	return ll.(*lltypes.IntType)
}

func isFixedScaledOp(op ast.MathBinaryOp) bool {
	return op == ast.MathBinaryOpMul || op == ast.MathBinaryOpDiv
}

// fixedScaledOp lowers `*` and `/`, which a value's scale takes part in, and the trap both
// come to when the answer doesn't fit.
func (l *lowerer) fixedScaledOp(block *ir.Block, op ast.MathBinaryOp, left, right value.Value, format types.FixedFormat) (value.Value, *ir.Block, error) {
	if op == ast.MathBinaryOpDiv {
		block = l.fixedTrapOnZero(block, right)
	}
	wide := fixedWideResult(block, op, left, right, format)
	narrow := storageType(format)
	block = l.emitTrapIf(block, outsideStorage(block, wide, format), l.panicOverflowFunc())
	return block.NewTrunc(wide, narrow), block, nil
}

func (l *lowerer) fixedTrapOnZero(block *ir.Block, divisor value.Value) *ir.Block {
	isZero := block.NewICmp(enum.IPredEQ, divisor, constant.NewInt(divisor.Type().(*lltypes.IntType), 0))
	return l.emitTrapIf(block, isZero, l.panicDivideByZeroFunc())
}

// doubleWidth is the integer holding any product or scaled dividend of a format's values.
func doubleWidth(format types.FixedFormat) *lltypes.IntType {
	return lltypes.NewInt(uint64(2 * format.Bits()))
}

// fixedWideResult is `*` or `/` at double width, where neither can overflow.
//
// A product **rounds to the nearest stored value, a half going up**: half a unit is added
// before the shift, the shift being arithmetic. A quotient truncates toward zero, as integer
// `/` does.
func fixedWideResult(block *ir.Block, op ast.MathBinaryOp, left, right value.Value, format types.FixedFormat) value.Value {
	wide := doubleWidth(format)
	shift := constant.NewInt(wide, int64(format.Fraction))
	a := block.NewSExt(left, wide)
	b := block.NewSExt(right, wide)
	if op == ast.MathBinaryOpDiv {
		return block.NewSDiv(block.NewShl(a, shift), b)
	}
	half := wideOne(wide, format.Fraction-1)
	return block.NewAShr(block.NewAdd(block.NewMul(a, b), half), shift)
}

func wideOne(ty *lltypes.IntType, shift int) *constant.Int {
	return &constant.Int{Typ: ty, X: new(big.Int).Lsh(big.NewInt(1), uint(shift))}
}

// outsideStorage answers whether a wide value is outside the format's storage. A value
// re-extended from its own low bits is unchanged exactly when they held it.
func outsideStorage(block *ir.Block, wide value.Value, format types.FixedFormat) value.Value {
	narrow := storageType(format)
	return block.NewICmp(enum.IPredNE, wide, block.NewSExt(block.NewTrunc(wide, narrow), wide.Type()))
}

// saturateStorage clamps a wide value to the format's storage range and truncates to it.
func saturateStorage(block *ir.Block, wide value.Value, format types.FixedFormat) value.Value {
	ty := wide.Type().(*lltypes.IntType)
	lo, hi := format.RawRange()
	loC := &constant.Int{Typ: ty, X: lo}
	hiC := &constant.Int{Typ: ty, X: hi}
	clamped := value.Value(block.NewSelect(block.NewICmp(enum.IPredSLT, wide, loC), loC, wide))
	clamped = block.NewSelect(block.NewICmp(enum.IPredSGT, clamped, hiC), hiC, clamped)
	return block.NewTrunc(clamped, storageType(format))
}

// lowerFixedFromBits lowers `fixed<8, 8>.from_bits(n)`: the integer is already the value.
func (l *lowerer) lowerFixedFromBits(block *ir.Block, call *ast.FunctionCallExpr, member *ast.MemberExpr) (value.Value, *ir.Block, bool, error) {
	head, ok := member.Object.(*ast.IdentifierExpr)
	if !ok || !isFixedName(types.PrimitiveTypeName(head.Name)) {
		return nil, nil, false, nil
	}
	v, block, err := l.lowerExpr(block, call.Arguments[0])
	return v, block, true, err
}

// lowerFixedMethod lowers the methods a fixed value has of its own, and the overflow family's
// multiply and divide, which scale. handled is false for anything else, the family's add and
// subtract among them: those are the storage integer's.
func (l *lowerer) lowerFixedMethod(block *ir.Block, call *ast.FunctionCallExpr, member *ast.MemberExpr) (value.Value, *ir.Block, bool, error) {
	format, ok := l.fixedOf(member.Object)
	if !ok {
		return nil, nil, false, nil
	}
	name := member.Property.Name
	switch name {
	case "to_bits", "floor", "ceil", "round":
		recv, block, err := l.lowerExpr(block, member.Object)
		if err != nil || diverged(recv, block) {
			return nil, block, true, err
		}
		return fixedWholePart(block, name, recv, format), block, true, nil
	case "wrapping_mul", "saturating_mul":
		left, right, block, err := l.lowerFixedOperands(block, call, member)
		if err != nil || left == nil {
			return nil, block, true, err
		}
		wide := fixedWideResult(block, ast.MathBinaryOpMul, left, right, format)
		if name == "saturating_mul" {
			return saturateStorage(block, wide, format), block, true, nil
		}
		return block.NewTrunc(wide, storageType(format)), block, true, nil
	case "checked_mul", "checked_div":
		return l.lowerFixedChecked(block, call, member, format)
	}
	return nil, nil, false, nil
}

func (l *lowerer) lowerFixedOperands(block *ir.Block, call *ast.FunctionCallExpr, member *ast.MemberExpr) (value.Value, value.Value, *ir.Block, error) {
	left, block, err := l.lowerExpr(block, member.Object)
	if err != nil || diverged(left, block) {
		return nil, nil, block, err
	}
	right, block, err := l.lowerExpr(block, call.Arguments[0])
	if err != nil || diverged(right, block) {
		return nil, nil, block, err
	}
	return left, right, block, nil
}

// fixedWholePart answers the method's whole number. None can overflow its storage integer:
// the storage has every bit the format's whole part does, and the fraction's bits to spare.
func fixedWholePart(block *ir.Block, name string, recv value.Value, format types.FixedFormat) value.Value {
	if name == "to_bits" {
		return recv
	}
	ty := recv.Type().(*lltypes.IntType)
	fraction := constant.NewInt(ty, int64(format.Fraction))
	floor := block.NewAShr(recv, fraction)
	switch name {
	case "ceil":
		mask := constant.NewInt(ty, (int64(1)<<uint(format.Fraction))-1)
		if format.Fraction == 63 {
			mask = constant.NewInt(ty, math.MaxInt64)
		}
		inexact := block.NewICmp(enum.IPredNE, block.NewAnd(recv, mask), constant.NewInt(ty, 0))
		return block.NewAdd(floor, block.NewZExt(inexact, ty))
	case "round":
		half := constant.NewInt(ty, int64(format.Fraction-1))
		return block.NewAdd(floor, block.NewAnd(block.NewAShr(recv, half), constant.NewInt(ty, 1)))
	}
	return floor
}

func (l *lowerer) lowerFixedChecked(block *ir.Block, call *ast.FunctionCallExpr, member *ast.MemberExpr, format types.FixedFormat) (value.Value, *ir.Block, bool, error) {
	name := member.Property.Name
	recorded, ok := l.recordedType(call)
	if !ok {
		return nil, nil, true, fmt.Errorf("llvm: %s() call has no recorded type", name)
	}
	dt, ok := recorded.(types.DataType)
	if !ok {
		return nil, nil, true, fmt.Errorf("llvm: %s() must return a Maybe, got %s", name, recorded)
	}
	someC, someTag, hasSome := findConstructor(dt, "Some")
	noneC, noneTag, hasNone := findConstructor(dt, "None")
	if !hasSome || !hasNone {
		return nil, nil, true, fmt.Errorf("llvm: %s()'s return type %q is not a canonical Maybe", name, dt.Name)
	}
	left, right, block, err := l.lowerFixedOperands(block, call, member)
	if err != nil || left == nil {
		return nil, block, true, err
	}
	op := ast.MathBinaryOpMul
	var zero value.Value = constant.False
	divisor := right
	if name == "checked_div" {
		op = ast.MathBinaryOpDiv
		ty := right.Type().(*lltypes.IntType)
		zero = block.NewICmp(enum.IPredEQ, right, constant.NewInt(ty, 0))
		divisor = block.NewSelect(zero, constant.NewInt(ty, 1), right)
	}
	wide := fixedWideResult(block, op, left, divisor, format)
	failed := block.NewOr(zero, outsideStorage(block, wide, format))
	some, err := l.buildDataValue(block, dt, someTag, someC, []value.Value{block.NewTrunc(wide, storageType(format))})
	if err != nil {
		return nil, nil, true, err
	}
	none, err := l.buildDataValue(block, dt, noneTag, noneC, nil)
	if err != nil {
		return nil, nil, true, err
	}
	return block.NewSelect(failed, none, some), block, true, nil
}

// lowerFixedConversion lowers a conversion with a fixed side. handled is false when neither is.
func (l *lowerer) lowerFixedConversion(block *ir.Block, arg value.Value, source, target types.PrimitiveTypeName) (value.Value, *ir.Block, bool, error) {
	sourceFormat, fromFixed := types.FixedFormatOf(source)
	targetFormat, toFixed := types.FixedFormatOf(target)
	if !fromFixed && !toFixed {
		return nil, nil, false, nil
	}
	if source == target {
		return arg, block, true, nil
	}
	switch {
	case fromFixed && toFixed:
		wide := rescale(block, arg, sourceFormat, targetFormat)
		block = l.emitTrapIf(block, outsideStorage(block, wide, targetFormat), l.panicOverflowFunc())
		return block.NewTrunc(wide, storageType(targetFormat)), block, true, nil
	case fromFixed:
		ll, ok := LLVMPrimitive(target)
		if !ok {
			return nil, nil, true, fmt.Errorf("llvm: no representation for %s", target)
		}
		ty, ok := ll.(*lltypes.FloatType)
		if !ok {
			return nil, nil, true, fmt.Errorf("llvm: conversion from %s to %s is not one", source, target)
		}
		// Divided as a double, whose scale is exact, and narrowed after, so a float32's
		// rounding happens once.
		asDouble := block.NewSIToFP(arg, lltypes.Double)
		scale := constant.NewFloat(lltypes.Double, math.Ldexp(1, sourceFormat.Fraction))
		return coerceFloatWidth(block, block.NewFDiv(asDouble, scale), ty), block, true, nil
	}
	if floatTy, ok := arg.Type().(*lltypes.FloatType); ok {
		v, b := l.fixedFromFloat(block, arg, floatTy, targetFormat)
		return v, b, true, nil
	}
	v, b := l.fixedFromInt(block, arg, IsSignedInt(source), targetFormat)
	return v, b, true, nil
}

// rescale moves a value from one fraction to another in an integer wide enough for the
// bits a left shift adds and the half a right shift adds first, rounding to nearest.
func rescale(block *ir.Block, v value.Value, from, to types.FixedFormat) value.Value {
	wide := widthHolding(max(from.Bits(), to.Bits()) + max(to.Fraction-from.Fraction, from.Fraction-to.Fraction) + 2)
	var out value.Value = block.NewSExt(v, wide)
	switch {
	case to.Fraction > from.Fraction:
		return block.NewShl(out, constant.NewInt(wide, int64(to.Fraction-from.Fraction)))
	case to.Fraction < from.Fraction:
		drop := from.Fraction - to.Fraction
		return block.NewAShr(block.NewAdd(out, wideOne(wide, drop-1)), constant.NewInt(wide, int64(drop)))
	}
	return out
}

// widthHolding answers the smallest integer type Lyra's targets have of at least `bits`.
func widthHolding(bits int) *lltypes.IntType {
	for _, w := range []int{8, 16, 32, 64} {
		if bits <= w {
			return lltypes.NewInt(uint64(w))
		}
	}
	return lltypes.NewInt(128)
}

func (l *lowerer) fixedFromInt(block *ir.Block, v value.Value, signed bool, format types.FixedFormat) (value.Value, *ir.Block) {
	source := v.Type().(*lltypes.IntType)
	wide := widthHolding(max(int(source.BitSize)+format.Fraction, format.Bits()) + 1)
	ext := coerceIntWidth(block, v, signed, wide)
	shifted := block.NewShl(ext, constant.NewInt(wide, int64(format.Fraction)))
	block = l.emitTrapIf(block, outsideStorage(block, shifted, format), l.panicOverflowFunc())
	return block.NewTrunc(shifted, storageType(format)), block
}

// fixedFromFloat scales and rounds to nearest, a half going up, and traps on what the format
// can't hold, a NaN among it: every comparison with one is false, so the test is for being
// inside.
func (l *lowerer) fixedFromFloat(block *ir.Block, v value.Value, ty *lltypes.FloatType, format types.FixedFormat) (value.Value, *ir.Block) {
	scale := constant.NewFloat(ty, math.Ldexp(1, format.Fraction))
	floorFn := l.floatMathFunc("llvm.floor."+map[*lltypes.FloatType]string{lltypes.Half: "f16", lltypes.Float: "f32", lltypes.Double: "f64"}[ty], ty, 1)
	rounded := block.NewCall(floorFn, block.NewFAdd(block.NewFMul(v, scale), constant.NewFloat(ty, 0.5)))
	top := math.Ldexp(1, format.Bits()-1)
	inside := block.NewAnd(
		block.NewFCmp(enum.FPredOGE, rounded, constant.NewFloat(ty, -top)),
		block.NewFCmp(enum.FPredOLT, rounded, constant.NewFloat(ty, top)),
	)
	block = l.emitTrapIf(block, block.NewXor(inside, constant.True), l.panicOverflowFunc())
	return block.NewFPToSI(rounded, storageType(format)), block
}

// lowerFixedNarrowing is the narrowing methods between formats, on the rescaled value.
func (l *lowerer) lowerFixedNarrowing(block *ir.Block, call *ast.FunctionCallExpr, name string, recv value.Value, from types.FixedFormat, target types.PrimitiveTypeName) (value.Value, *ir.Block, error) {
	to, ok := types.FixedFormatOf(target)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: %s() on a fixed value has the target %s", name, target)
	}
	wide := rescale(block, recv, from, to)
	switch name {
	case "truncate":
		return block.NewTrunc(wide, storageType(to)), block, nil
	case "saturate":
		return saturateStorage(block, wide, to), block, nil
	}
	if !l.res.RangeSafety.NoNarrowTrap(call) {
		block = l.emitTrapIf(block, outsideStorage(block, wide, to), l.narrowOverflowFunc())
	}
	return block.NewTrunc(wide, storageType(to)), block, nil
}

// fixedStrBufBytes holds a sign, 20 digits, the point and 63 of them, with room to spare.
const fixedStrBufBytes = 96

// fixedToStrFunc lazily defines `i64 @lyra_fixed_to_str(i64 raw, i64 fraction, i8* out)`,
// writing the exact decimal: a binary fraction ends, so the digits run out, one at a time,
// each the whole part of the rest times ten. There is at least one after the point, so a
// whole value reads `2.0`, as a float's does.
func (l *lowerer) fixedToStrFunc() *ir.Func {
	const name = "lyra_fixed_to_str"
	for _, f := range l.module.Funcs {
		if f.Name() == name {
			return f
		}
	}
	raw := ir.NewParam("raw", lltypes.I64)
	fraction := ir.NewParam("fraction", lltypes.I64)
	out := ir.NewParam("out", lltypes.NewPointer(lltypes.I8))
	fn := l.module.NewFunc(name, lltypes.I64, raw, fraction, out)
	entry := fn.NewBlock("")
	loop := fn.NewBlock("digit")
	done := fn.NewBlock("done")

	wide := lltypes.I128
	// The magnitude as unsigned: the two's complement of the minimum is itself, and read
	// unsigned that is its magnitude.
	negative := entry.NewICmp(enum.IPredSLT, raw, i64c(0))
	magnitude := entry.NewSelect(negative, entry.NewSub(i64c(0), raw), raw)
	start := i64c(0)
	var at value.Value = entry.NewSelect(negative, i64c(1), start)
	entry.NewStore(constant.NewInt(lltypes.I8, '-'), out)
	whole := entry.NewLShr(magnitude, fraction)
	digits := entry.NewCall(l.snprintfFunc(),
		entry.NewGetElementPtr(lltypes.I8, out, at), i64c(24), l.cString("%llu"), whole)
	at = entry.NewAdd(at, entry.NewSExt(digits, lltypes.I64))
	entry.NewStore(constant.NewInt(lltypes.I8, '.'), entry.NewGetElementPtr(lltypes.I8, out, at))
	at = entry.NewAdd(at, i64c(1))
	mask := entry.NewSub(entry.NewShl(constant.NewInt(wide, 1), entry.NewZExt(fraction, wide)), constant.NewInt(wide, 1))
	rest := entry.NewAnd(entry.NewZExt(magnitude, wide), mask)
	entry.NewBr(loop)

	position := loop.NewPhi(ir.NewIncoming(at, entry))
	remaining := loop.NewPhi(ir.NewIncoming(rest, entry))
	timesTen := loop.NewMul(remaining, constant.NewInt(wide, 10))
	digit := loop.NewTrunc(loop.NewLShr(timesTen, loop.NewZExt(fraction, wide)), lltypes.I8)
	loop.NewStore(loop.NewAdd(digit, constant.NewInt(lltypes.I8, '0')), loop.NewGetElementPtr(lltypes.I8, out, position))
	next := loop.NewAdd(position, i64c(1))
	left := loop.NewAnd(timesTen, mask)
	position.Incs = append(position.Incs, ir.NewIncoming(next, loop))
	remaining.Incs = append(remaining.Incs, ir.NewIncoming(left, loop))
	loop.NewCondBr(loop.NewICmp(enum.IPredNE, left, constant.NewInt(wide, 0)), loop, done)

	done.NewRet(next)
	return fn
}
