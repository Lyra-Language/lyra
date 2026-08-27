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
		// **A function type is a C function pointer, and only in parameter position.**
		// Returning one would hand back a value Lyra has no way to call — a bare code
		// address is not a closure — so the type is admitted exactly where a callback is
		// passed *in*, which is what every callback API does.
		if fp, ok := types.StripNewtype(p.Type).(*types.LambdaType); ok {
			tc.requireCallbackIsFFISafe(fp, decl, i+1)
			tc.requireNoBorrow(p, decl, i+1)
			continue
		}
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
			// Kept current with the representation: a string *is* NUL-terminated past
			// its bytes as of 08/26, so the crossing no longer needs a copy and the
			// advice is the scoped lender rather than a builder.
			return ". A Lyra string is a fat pointer, not a `char*`: take `^u8` and hand " +
				"the bytes over with `std.ffi`'s `with_cstring`, which needs no copy"
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
	case types.NamedStructType, types.AnonymousStructType, types.TupleType, types.DataType:
		// **The layouts already agree** — `TestExec_FFIFixture_StructLayoutMatchesC` proves
		// Lyra's `{i32, u8, f64, i64}` matches C's `sizeof` and every `offsetof` — so a
		// pointer is not a workaround for a representational mismatch. What is missing is
		// the *calling convention*, which is a different thing and a per-target one.
		return ". A struct crosses by pointer: take `^T` and pass `&value` — no copy, and " +
			"the layouts already match C's. By *value* is a per-target calling convention " +
			"rather than a missing spelling (aarch64 and x86-64 classify the same struct " +
			"differently, and x86-64 can change the parameter count), which this compiler " +
			"does not implement"
	}
	return ""
}

// checkVariadicArguments checks a call to a variadic `extern`, and records the type each
// argument in the `...` part is actually passed at.
//
// # The named half is an ordinary call
//
// Everything up to the declared parameter count is checked exactly as a fixed-arity call's
// arguments are — same assignability, same literal-range rule, same contextual narrowing.
// `...` widens the arity ceiling and nothing else.
//
// # The variadic half is C's, and the compiler owes the promotions
//
// An argument in the `...` part undergoes C's **default argument promotions**: an integer
// narrower than `int` widens to `int` (sign-extended if signed, zero-extended if not), and
// a `float` widens to `double`. That is not a convention a caller can be asked to remember
// — a `u8` passed unpromoted occupies a different slot from the `i32` the callee reads, and
// the result is the silent garbage this whole feature exists to stop. The promoted type is
// recorded on the argument node so the backend emits the widening without re-deriving the
// rule.
//
// # What is not checked, and cannot be
//
// The *format string*. `printf("%d", x)` with the wrong `x` is undetectable by any compiler
// that does not parse format strings, and parsing them would be a second language embedded
// in this one. `unsafe` already covers the claim; this makes the ABI right, not the call
// correct.
func (tc *TypeChecker) checkVariadicArguments(calleeName string, lambda *ast.LambdaExpr, call *ast.FunctionCallExpr) {
	named := len(lambda.Parameters)
	tc.elaborateLambdaArgsFromParams(lambda.Parameters, call.Arguments)
	for i, arg := range call.Arguments {
		if i < named {
			tc.checkNamedArgument(calleeName, lambda.Parameters[i], i, arg)
			continue
		}
		argType := tc.inferExprType(arg)
		if argType == nil {
			continue
		}
		// An untyped literal settles to its **own default** here, not to C's `int`. There
		// is nothing to adopt from — a variadic parameter has no declared type — and
		// making one position of the language default differently from every other would
		// be a rule with no way to see it at the call site. So `printf(fmt, 42)` passes an
		// i64, and `%ld` is its format; `i32(42)` is how `%d` is spelled.
		argType = promoteToDefault(argType)
		tc.typeTable.Set(arg, argType)

		promoted := promoteForVariadic(argType)
		if !isFFISafe(tc.resolveTypeIfKnown(promoted, arg.GetLocation())) {
			tc.addErrorCode(arg.GetLocation(), SeverityError, diag.CodeNotFFISafe,
				"%s: argument %d is %s, which has no C spelling. A variadic argument takes the "+
					"integer and float widths, `rune`, and a raw pointer `^T`%s",
				calleeName, i+1, argType, ffiHint(argType))
			continue
		}
		if !types.TypesEqual(promoted, argType) {
			// The widening C would have applied, recorded where the backend reads it.
			// Recording the *promoted* type rather than emitting a conversion node keeps
			// this a property of the call rather than a rewrite of the author's argument,
			// which is what lets a diagnostic still name what was written.
			tc.typeTable.SetVariadicPromotion(arg, promoted)
		}
	}
}

// promoteForVariadic is C's default argument promotion: anything narrower than `int`
// becomes `int`, `float` becomes `double`, everything else is passed as it is.
//
// Verified against clang rather than recalled — it emits `sext i8 → i32`, `zext i16 → i32`
// and `fpext float → double` ahead of the varargs call, which is exactly this table.
//
// **`bool` cannot reach here** (lyra-E063 refuses it at every boundary, a bit against a
// byte), and a newtype is looked through for the same reason it is in `isFFISafe`: it is
// nominal only, so it is passed as its base and promoted as its base.
func promoteForVariadic(t types.Type) types.Type {
	p, ok := types.StripNewtype(t).(types.PrimitiveType)
	if !ok {
		return t
	}
	switch p.Name {
	case types.Int8, types.Int16:
		return types.PrimitiveType{Name: types.Int32}
	case types.UInt8, types.UInt16:
		return types.PrimitiveType{Name: types.UInt32}
	case types.Float32:
		return types.PrimitiveType{Name: types.Float64}
	}
	return t
}

// requireCallbackIsFFISafe checks a **C function pointer** parameter: every type in the
// callback's own signature must itself cross.
//
// `int (*)(const void*, const void*)` is `(^u8, ^u8) -> i32` here, and it is admitted
// because a Lyra **top-level function** already emits exactly that symbol — `declareFunction`
// lowers its parameters directly, with no environment word, so `@lyra.main.compare(i8*, i8*)`
// *is* the C signature rather than something convertible to it.
//
// What is refused is at the *call*, not here (checkCallbackArgument): only a top-level
// function may be passed, because a closure is `{code, env}` and its lifted body takes the
// environment as a leading parameter. The type says "a C function pointer"; the argument
// rule says which values are one.
//
// A callback's own signature is checked with the same predicate the outer one uses, so
// `(string) -> void` is refused for the reason `extern f: (string)` is, and the nesting
// terminates because a callback parameter that is itself a function type is refused here
// rather than recursed into — C's `void (*)(void (*)(void))` exists but nothing wants it,
// and admitting it would need the argument rule to reach a second level.
func (tc *TypeChecker) requireCallbackIsFFISafe(fp *types.LambdaType, decl *ast.ExternDeclStmt, idx int) {
	bad := func(t types.Type, what string) {
		tc.addErrorCode(decl.NameLocation, SeverityError, diag.CodeNotFFISafe,
			"parameter %d of `extern %s` is a callback whose %s is %s, which has no C spelling. "+
				"Every type in a callback's signature must cross too%s",
			idx, decl.Name, what, t, ffiHint(tc.resolveTypeIfKnown(t, decl.GetLocation())))
	}
	for i, p := range fp.Parameters {
		if p.Type == nil {
			continue
		}
		if _, nested := types.StripNewtype(p.Type).(*types.LambdaType); nested {
			bad(p.Type, fmt.Sprintf("parameter %d", i+1))
			continue
		}
		if !isFFISafe(tc.resolveTypeIfKnown(p.Type, decl.GetLocation())) {
			bad(p.Type, fmt.Sprintf("parameter %d", i+1))
		}
	}
	if rt := fp.ReturnType.Type; rt != nil && !isFFISafe(tc.resolveTypeIfKnown(rt, decl.GetLocation())) {
		bad(rt, "return type")
	}
}

// checkCallbackArguments refuses anything but a **top-level function** in a C callback
// slot, at every argument of a call to an extern whose parameter is a function type.
//
// See lyra-E066 for why the restriction is representational. The rule here is exact rather
// than approximate, and the awkward case is the reason: the argument must be an identifier
// that resolves *in scope* to the same lambda `LookupFunctionFrom` names. A **local binding
// shadowing a top-level function** passes the first test and fails the second — and without
// the second the backend, which resolves by name through `l.funcs`, would emit the
// top-level function's symbol for a program that means the local. That is a wrong call
// rather than a diagnostic.
func (tc *TypeChecker) checkCallbackArguments(calleeName string, lambda *ast.LambdaExpr, call *ast.FunctionCallExpr) {
	if !lambda.IsExtern {
		return
	}
	for i, param := range lambda.Parameters {
		if i >= len(call.Arguments) {
			return
		}
		if _, isFn := types.StripNewtype(param.Type).(*types.LambdaType); !isFn {
			continue
		}
		arg := call.Arguments[i]
		if tc.isTopLevelFunctionRef(arg) {
			continue
		}
		tc.addErrorCode(arg.GetLocation(), SeverityError, diag.CodeCallbackMustBeTopLevel,
			"%s: argument %d is a C callback, so it must be a top-level function — a "+
				"closure is a code pointer plus an environment, and C takes only the "+
				"code pointer. Give it a name at the top level; anything it needs to "+
				"capture travels through the callback's own `^u8` context parameter",
			calleeName, i+1)
	}
}

// isTopLevelFunctionRef reports whether expr names a top-level function — the one kind of
// value whose emitted symbol has a C signature.
func (tc *TypeChecker) isTopLevelFunctionRef(expr ast.Expression) bool {
	id, ok := expr.(*ast.IdentifierExpr)
	if !ok {
		return false
	}
	fn, ok := tc.symTable.LookupFunctionFrom(id.Name, expr.GetLocation())
	if !ok || fn == nil {
		return false
	}
	// And the name must still *mean* that function here. A local binding of the same name
	// resolves through the scope chain to a different lambda, and the backend would
	// nonetheless emit the top-level symbol.
	sym, found := tc.scope.Lookup(id.Name)
	if !found {
		return false
	}
	switch v := sym.(type) {
	case *ast.LambdaExpr:
		return v == fn
	case *ast.VarDeclStmt:
		lam, isLambda := v.Value.(*ast.LambdaExpr)
		return isLambda && lam == fn
	}
	return false
}
