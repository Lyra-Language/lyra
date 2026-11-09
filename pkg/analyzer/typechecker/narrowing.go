package typechecker

import (
	"github.com/Lyra-Language/lyra/pkg/ast"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// narrowingMethods are the named narrowing conversions. `u8(x)` truncates and says nothing
// of whether that was meant; each of these says what should happen to a value the target
// cannot hold:
//
//   - `truncate` keeps the low bits, which is what `u8(x)` does;
//   - `saturate` answers the target's nearest value;
//   - `narrow` traps.
//
// **The target is the method's only type input and no argument carries it**, so it comes from
// a turbofish (`x.narrow::<u8>()`) or the context (`let b: u8 = x.narrow()`), the turbofish
// first, as it beats the context on any generic call.
var narrowingMethods = map[string]bool{
	"truncate": true,
	"saturate": true,
	"narrow":   true,
}

// narrowingSignature answers the signature of one of the narrowing methods on an integer
// receiver. handled is false when name is not one, or the receiver is not a type they
// apply to, so the caller's other rungs still run. A nil signature with handled true has
// been refused here.
func (tc *TypeChecker) narrowingSignature(recv types.Type, name string, call *ast.FunctionCallExpr, member *ast.MemberExpr) (sig *types.LambdaType, handled bool) {
	if !narrowingMethods[name] {
		return nil, false
	}
	source, ok := narrowingSource(recv)
	if !ok {
		if isFloatType(recv) {
			tc.addErrorCode(member.GetLocation(), SeverityError, diag.CodeNarrowingTarget,
				"%q narrows an integer, and %s is a float: round it first, with `.floor()`, `.ceil()` or `.round()`, and narrow the integer that gives",
				name, recv)
			return nil, true
		}
		return nil, false
	}
	target, ok := tc.narrowingTarget(name, source, call, member)
	if !ok {
		return nil, true
	}
	if holdsEveryValue(target, source) {
		tc.addErrorCode(member.GetLocation(), SeverityError, diag.CodeNarrowingTarget,
			"%q has nothing to narrow: every %s fits in %s, so write the conversion, `%s(...)`",
			name, source, target, target)
		return nil, true
	}
	return &types.LambdaType{ReturnType: types.ReturnType{Type: target}}, true
}

// narrowingSource answers the integer a narrowing method narrows from, an untyped literal
// at the default its binding would have taken.
func narrowingSource(recv types.Type) (types.PrimitiveType, bool) {
	p, ok := recv.(types.PrimitiveType)
	if !ok {
		return types.PrimitiveType{}, false
	}
	switch p.Name {
	case types.UntypedInt, types.UntypedSignedInt:
		return types.PrimitiveType{Name: types.Int64}, true
	}
	_, fixed := types.FixedFormatOf(p.Name)
	return p, isAnyConcreteInt(p.Name) || fixed
}

// narrowingTarget reads a narrowing method's target from its turbofish or its context, and
// refuses what is not a concrete integer.
func (tc *TypeChecker) narrowingTarget(name string, source types.PrimitiveType, call *ast.FunctionCallExpr, member *ast.MemberExpr) (types.PrimitiveType, bool) {
	var written types.Type
	switch len(call.GenericArguments) {
	case 0:
		written = tc.currentExpectedType()
		if written == nil {
			tc.addErrorCode(member.GetLocation(), SeverityError, diag.CodeNarrowingTarget,
				"%q needs a target type, and nothing here says one: write `.%s::<u8>()`, or use it where a type is wanted (`let b: u8 = x.%s()`)",
				name, name, name)
			return types.PrimitiveType{}, false
		}
	case 1:
		written = tc.resolveType(call.GenericArguments[0], member.GetLocation())
		if written == nil {
			return types.PrimitiveType{}, false
		}
	default:
		tc.addErrorCode(member.GetLocation(), SeverityError, diag.CodeNarrowingTarget,
			"%q takes one type argument, its target, and was given %d", name, len(call.GenericArguments))
		return types.PrimitiveType{}, false
	}
	target, ok := written.(types.PrimitiveType)
	if _, fromFixed := types.FixedFormatOf(source.Name); fromFixed {
		if _, toFixed := types.FixedFormatOf(target.Name); !ok || !toFixed {
			tc.addErrorCode(member.GetLocation(), SeverityError, diag.CodeNarrowingTarget,
				"%q on a %s narrows to another fixed-point format, and %s is not one: an integer comes from `.floor()`, `.ceil()`, `.round()` or `.to_bits()`",
				name, source, written)
			return types.PrimitiveType{}, false
		}
		return target, true
	}
	if !ok || !isAnyConcreteInt(target.Name) {
		tc.addErrorCode(member.GetLocation(), SeverityError, diag.CodeNarrowingTarget,
			"%q narrows to a concrete integer type, and %s is not one", name, written)
		return types.PrimitiveType{}, false
	}
	return target, true
}

// holdsEveryValue reports whether every value of source is a value of target, so a method
// choosing what becomes of the rest would be choosing about nothing.
func holdsEveryValue(target, source types.PrimitiveType) bool {
	if sourceFormat, ok := types.FixedFormatOf(source.Name); ok {
		targetFormat, ok := types.FixedFormatOf(target.Name)
		return ok && targetFormat.HoldsEvery(sourceFormat)
	}
	targetLo, targetHi, ok := types.IntegerRange(target.Name)
	if !ok {
		return false
	}
	sourceLo, sourceHi, ok := types.IntegerRange(source.Name)
	if !ok {
		return false
	}
	return targetLo.Cmp(sourceLo) <= 0 && targetHi.Cmp(sourceHi) >= 0
}
