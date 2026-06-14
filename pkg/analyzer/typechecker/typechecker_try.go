package typechecker

import (
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// inferTryExpr type-checks a `?` (try) postfix operator and returns the
// unwrapped success payload (the T in Result<T, E> / Maybe<T>).
//
// Three things are enforced here, the type-aware half of the try checks:
//   - the operand must be a Result or Maybe;
//   - propagation is same-kind only — a Result may only be `?`-propagated from a
//     Result-returning function, a Maybe only from a Maybe-returning function.
//     Crossing kinds requires an explicit conversion (e.g. maybe.ok_or(err) /
//     result.ok()), which keeps the lossy step visible.
//
// The "used outside a Result/Maybe-returning function" context error is reported
// by checker.CheckTryOutsideResult, so when the enclosing return kind is unknown
// (top level, or a non-Result/Maybe return) this function stays silent to avoid
// duplicate diagnostics and simply yields the unwrapped payload.
func (tc *TypeChecker) inferTryExpr(e *ast.TryExpr) types.Type {
	operandT := tc.inferExprType(e.Operand)
	if operandT == nil {
		return nil // operand already failed to infer; it was reported elsewhere
	}

	kind, payload, ok := resultOrMaybeKind(operandT)
	if !ok {
		tc.addError(e.GetLocation(), SeverityError,
			"`?` operand must be a Result or Maybe, got %s", operandT.GetName())
		return nil
	}

	if enclKind, found := tc.enclosingReturnKind(); found && enclKind != kind {
		tc.addError(e.GetLocation(), SeverityError,
			"cannot propagate %s with `?` from a %s-returning function; convert it explicitly",
			kind, enclKind)
		return nil
	}
	// TODO(Result error conversion): when the operand's E differs from the
	// enclosing function's Err type, check convertibility (à la Rust's From)
	// rather than requiring identical E, once a conversion trait exists.

	tc.typeTable.Set(e, payload)
	return payload
}

// resultOrMaybeKind reports whether t is a Result<T, E> or Maybe<T> and, if so,
// its kind ("Result"/"Maybe") and success payload type T. Recognition is by
// name, matching checker.isResultOrMaybeName (see that note on the stop-gap).
func resultOrMaybeKind(t types.Type) (kind string, payload types.Type, ok bool) {
	p, isParam := t.(types.ParameterizedType)
	if !isParam {
		return "", nil, false
	}
	switch p.Name {
	case "Result":
		if len(p.TypeArguments) == 2 {
			return "Result", p.TypeArguments[0], true
		}
	case "Maybe":
		if len(p.TypeArguments) == 1 {
			return "Maybe", p.TypeArguments[0], true
		}
	}
	return "", nil, false
}

// enclosingReturnKind returns the kind ("Result"/"Maybe") of the return type of
// the lambda body currently being checked, or found=false when there is no
// enclosing function or its return type is neither Result nor Maybe.
func (tc *TypeChecker) enclosingReturnKind() (kind string, found bool) {
	if tc.enclosingRet == nil {
		return "", false
	}
	k, _, ok := resultOrMaybeKind(tc.enclosingRet.Type)
	return k, ok
}
