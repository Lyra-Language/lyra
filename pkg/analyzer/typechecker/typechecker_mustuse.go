package typechecker

import (
	"github.com/Lyra-Language/lyra/pkg/ast"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// checkMustUseResult enforces the "must-use" rule for Result and Maybe: a
// statement that produces one and then drops it — no binding, no `match`, no `?`
// propagation — is almost always a bug, because the Err/None channel that makes
// these types worth using is being silently thrown away.
//
// It is called from checkExpressionStmt for the value-producing statement forms
// (a bare function/method call, or a `?` whose unwrapped payload is itself a
// Result/Maybe). t is the already-inferred type of the statement expression.
//
// Opting out is deliberate and explicit: bind the value to a throwaway name
// (e.g. `let _ = expr` or `let _err = expr`). Any binding form makes the
// statement a declaration rather than an ExpressionStmt, so it never reaches
// this check — there is no separate suppression to maintain.
//
// Recognition is via resultOrMaybeKind, which reads the canonical identity
// stamped at collection time (an `@builtin` marker or the name-plus-shape
// fallback) — the same source of truth the `?` checks use.
func (tc *TypeChecker) checkMustUseResult(expr ast.Expression, t types.Type) {
	if t == nil {
		return
	}
	kind, _, _, ok := tc.resultOrMaybeKind(t, expr.GetLocation())
	if !ok {
		return
	}
	tc.addErrorCode(expr.GetLocation(), SeverityWarning, diag.CodeUnusedResult,
		"unused %s: the value is discarded without handling its error/absence; "+
			"match on it, propagate it with `?`, or bind it (`let _ = ...`) to discard it intentionally",
		kind)
}
