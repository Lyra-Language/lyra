package typechecker

import (
	"github.com/Lyra-Language/lyra/pkg/ast"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// `pointer_at(address)` is a raw pointer to a fixed address — a console's hardware
// registers (the Genesis's VDP control port is `0xC00004`), which have no Lyra storage to
// take `&` of. It is the third way to make a pointer, after `&` and `nullptr`, and it is
// typed exactly as `nullptr` is: **an untyped pointer whose context supplies the
// pointee** —
//
//	let control: ^mut u16 = unsafe { pointer_at(0xC00004) }
//
// — so a return type, a parameter or an annotation pins it, and one nothing pins is
// lyra-E069 (checkUnpinnedNullPtrs). No new inference: the call's recorded type starts as
// the untyped placeholder and propagateExpected overwrites it, which is also how the
// backend knows the pointer type to emit (lowerPointerAt).
//
// It needs `unsafe` (lyra-E011): a pointer to an arbitrary address is memory nothing can
// vouch for. Reading and writing through it are the usual derefs, or
// `read_volatile`/`write_volatile` for a hardware register, which LLVM must not merge or
// drop.

// isBuiltinPointerAtFn reports whether name is the compiler-provided `pointer_at`,
// resolved by name after scope resolution misses, like every builtin free function.
func isBuiltinPointerAtFn(name string) bool {
	return name == "pointer_at"
}

// inferPointerAtCall types `pointer_at(address)`: one integer argument, the untyped
// pointer as the result.
func (tc *TypeChecker) inferPointerAtCall(call *ast.FunctionCallExpr) types.Type {
	if !tc.inUnsafe {
		tc.addErrorCode(call.GetLocation(), SeverityError, diag.CodeUnsafeOutsideUnsafe,
			"`pointer_at` makes a pointer to any address, so it requires an `unsafe` block or function")
	}
	if len(call.Arguments) != 1 {
		tc.addError(call.GetLocation(), SeverityError,
			"pointer_at: expected 1 argument (the address), got %d", len(call.Arguments))
		return types.PrimitiveType{Name: types.UntypedNullPtr}
	}
	arg := call.Arguments[0]
	argType := tc.inferExprType(arg)
	if argType != nil {
		if !isIntegerOperand(argType) {
			tc.addError(arg.GetLocation(), SeverityError,
				"pointer_at: the address must be an integer, got %s", argType)
		} else if p, ok := argType.(types.PrimitiveType); ok &&
			(p.Name == types.UntypedInt || p.Name == types.UntypedSignedInt) {
			// A literal address is a u64 — wide enough for any machine's, and unsigned,
			// as an address is. A typed one keeps its own width.
			tc.propagateExpected(arg, types.PrimitiveType{Name: types.UInt64}, false)
		}
	}
	return types.PrimitiveType{Name: types.UntypedNullPtr}
}

// isUntypedPointerCall reports whether expr is a `pointer_at` call still waiting for its
// pointee: the only call whose recorded type is the untyped pointer.
func (tc *TypeChecker) isUntypedPointerCall(expr ast.Expression) bool {
	call, ok := expr.(*ast.FunctionCallExpr)
	if !ok {
		return false
	}
	t, recorded := tc.typeTable.Get(call)
	return recorded && isUntypedNullPtr(t)
}
