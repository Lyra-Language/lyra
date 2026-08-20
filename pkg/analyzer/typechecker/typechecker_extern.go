package typechecker

import (
	"fmt"

	"github.com/Lyra-Language/lyra/pkg/ast"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// `extern` declarations: the two rules that hold at the boundary.
//
// Neither is about the call — a call to an extern is checked against its signature by the
// machinery every call goes through (ExternDeclStmt.Func). These are about the
// *declaration*, and both exist because a foreign function is the one thing in the language
// whose behaviour nothing can verify.

// checkExternDecl enforces the two rules on an `extern` declaration.
func (tc *TypeChecker) checkExternDecl(decl *ast.ExternDeclStmt) {
	tc.checkExternBoundIsAsserted(decl)
	tc.checkExternSignatureIsFFISafe(decl)
}

// checkExternBoundIsAsserted requires `unsafe` on an extern that narrows its effect bound.
//
// An extern with no bound carries every effect — the unresolved-callee default — and is
// safe to declare, because it claims nothing. Writing `pure`, `det` or `noalloc` claims
// something no compiler can check, and a wrong claim does not fail here: it silently
// corrupts the effect analysis of every caller, since `pure` is *believed*. That is a
// declaration-time danger, so the keyword goes on the declaration.
//
// The inverse is deliberately not an error. `unsafe extern` with no bound asserts nothing
// and is merely redundant, and a program mid-edit — the bound removed, the keyword not yet
// — should not stop compiling for it.
func (tc *TypeChecker) checkExternBoundIsAsserted(decl *ast.ExternDeclStmt) {
	if decl.IsUnsafe || !(decl.IsPure || decl.IsDet || decl.IsNoAlloc) {
		return
	}
	tc.addErrorCode(decl.NameLocation, SeverityError, diag.CodeExternBoundNeedsUnsafe,
		"`extern %s` claims an effect bound the compiler cannot check — write `unsafe extern` "+
			"to assert it. Without a bound it carries every effect, which is safe to declare "+
			"because it claims nothing", decl.Name)
}

// checkExternSignatureIsFFISafe refuses a parameter or return type that has no C spelling.
//
// The boundary takes what C has: the integer and float widths, `rune`, `^T` and `void`.
// Everything else — `string`, `[]T`, a closure, a tuple, a `data` type, anything `shared`
// or `weak` — is refused *at the signature*, which is what leaves no room for an implicit
// conversion and therefore no nul-termination policy to get wrong. `std.ffi` builds what
// Lyra wants on top, in ordinary Lyra, where the copy is visible and `noalloc` can see it.
// That is the `read_line`/`parse_i64` division one layer up.
func (tc *TypeChecker) checkExternSignatureIsFFISafe(decl *ast.ExternDeclStmt) {
	if decl.Signature == nil {
		return
	}
	for i, p := range decl.Signature.Parameters {
		tc.requireFFISafe(p.Type, decl, "parameter %d of", i+1)
		tc.requireNoBorrow(p, decl, i+1)
	}
	tc.requireFFISafe(decl.Signature.ReturnType.Type, decl, "the return type of", 0)
}

// requireNoBorrow refuses `mut`/`ref` on an extern's parameter.
//
// A borrow modifier is **Lyra's** by-reference passing, and the compiler decides what it
// means: `paramIsByRef` passes a `mut` aggregate as a pointer to its storage and a `mut`
// scalar by value. Neither is a rule a C function was compiled by, so at the boundary the
// modifier is one of two bad things — inert on a scalar, where it says something the call
// does not do, or an outright ABI mismatch on a pointer, where `(mut ^i64)` reads as "a
// pointer" and would pass an `i64**`.
//
// What C has is the pointer itself, which the signature can already say. Refused where the
// types are, because it is the same question: what may cross.
func (tc *TypeChecker) requireNoBorrow(p types.ParameterType, decl *ast.ExternDeclStmt, idx int) {
	if p.Borrow != types.Mut && p.Borrow != types.Ref {
		return
	}
	tc.addErrorCode(decl.NameLocation, SeverityError, diag.CodeNotFFISafe,
		"parameter %d of `extern %s` is `%s`, and a borrow modifier has no C spelling: it is "+
			"Lyra's own by-reference passing, which a foreign callee was not compiled by. "+
			"Drop it, and where C takes a pointer write one — `^T`, passed as `&x` or `&mut x`",
		idx, decl.Name, p.Borrow)
}

func (tc *TypeChecker) requireFFISafe(t types.Type, decl *ast.ExternDeclStmt, what string, idx int) {
	if t == nil {
		return
	}
	resolved := tc.resolveTypeIfKnown(t, decl.GetLocation())
	if isFFISafe(resolved) {
		return
	}
	where := what
	if idx > 0 {
		where = fmt.Sprintf(what, idx)
	}
	tc.addErrorCode(decl.NameLocation, SeverityError, diag.CodeNotFFISafe,
		"%s `extern %s` is %s, which has no C spelling. A foreign signature takes the "+
			"integer and float widths, `rune`, a raw pointer `^T`, and `void`%s",
		where, decl.Name, t, ffiHint(resolved))
}

// isFFISafe reports whether a type may cross the boundary.
//
// A **newtype is looked through**, because it is nominal only: `newtype Fd = i32` is an
// i32 at run time, and refusing it would refuse the one wrapper that makes a foreign
// signature readable.
func isFFISafe(t types.Type) bool {
	switch v := types.StripNewtype(t).(type) {
	case types.VoidType:
		return true
	case types.RawPointerType:
		return true
	case types.PrimitiveType:
		return isAnyConcreteInt(v.Name) || isAnyConcreteFloat(v.Name) || v.Name == types.Rune
	}
	return false
}

// ffiHint names the fix for the types a reader is most likely to have reached for.
func ffiHint(t types.Type) string {
	switch v := types.StripNewtype(t).(type) {
	case types.PrimitiveType:
		switch {
		case types.IsString(v):
			return ". A Lyra string is a fat pointer and is not NUL-terminated, so it needs " +
				"a copy: take `^u8` and build one with `std.ffi`'s CString"
		case v.Name == types.Boolean:
			return ". `bool` is deliberately excluded: Lyra's is one bit and C's `_Bool` is a " +
				"byte, so passing one silently disagrees about the ABI — take `i8` and compare it"
		}
	case types.DynamicArrayType, types.StaticArrayType:
		return ". An array's elements already sit behind a contiguous buffer, so take `^T` " +
			"and a length, and get the pointer from `xs.data()`"
	case *types.LambdaType:
		return ". A Lyra closure is a code pointer plus a ref-counted environment, which is " +
			"not a C function pointer; passing callbacks to C is not designed yet"
	}
	return ""
}
