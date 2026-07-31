package typechecker

import (
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// Contextual typing for lambda literals.
//
// A lambda literal used to get *nothing* from the context it appears in: `(x) => x + 1`
// reported `undefined symbol "x"` because an unannotated parameter never reached
// `tc.paramTypes`, and `() => 7` was rejected against `() -> i64` because the body's untyped
// leaf never learned the expected width. Only a fully annotated lambda worked — which meant
// every call site of every lazy combinator in the standard library had to spell out types
// the signature already stated.
//
// **The fix is elaboration, not a second inference mode.** When a lambda literal meets an
// expected function type, its missing annotations are filled in *on the AST node* from that
// type, before its body is inferred. Everything downstream then sees the lambda the user
// would have had to write by hand: `withParamScope` seeds the parameters because they now
// have types, `checkLambdaBody` checks and width-propagates the body because there is now a
// declared return, and the backend — which reads `ast.Parameter.Type` to lower a parameter —
// needs no changes at all. One mechanism closes both halves of the bug.
//
// Two properties make this safe rather than a hack. It only ever fills what was **left
// blank**, so an explicit annotation always wins and can still be diagnosed as wrong. And it
// runs *before* the body is inferred, which is the ordering the bottom-up default cannot
// give: by the time a mismatch is reported, the lambda's own types are already settled.

// elaborateLambda fills a lambda literal's missing parameter and return annotations from the
// type its context expects. It is a no-op unless expr is a lambda literal and want is a
// function type — every other pairing is left to ordinary assignability, which reports it.
//
// Call it immediately *before* inferring expr, at any site that knows what it wants: an
// annotated binding, an argument position, a return position.
func (tc *TypeChecker) elaborateLambda(expr ast.Expression, want types.Type) {
	lambda, ok := expr.(*ast.LambdaExpr)
	if !ok {
		return
	}
	sig, ok := types.StripNewtype(tc.resolveTypeIfKnown(want)).(*types.LambdaType)
	if !ok || sig == nil {
		return
	}
	// A multi-clause lambda has per-clause patterns rather than one parameter list, so
	// there is no position to match a signature parameter against.
	if len(lambda.LambdaClauses) > 0 {
		return
	}
	// Arity disagreement is a real error the call site reports; filling anything here
	// would only turn one diagnostic into several about types nobody wrote.
	if len(lambda.Parameters) != len(sig.Parameters) {
		return
	}
	for i := range lambda.Parameters {
		if lambda.Parameters[i].Type == nil && isConcreteEnoughToElaborate(sig.Parameters[i].Type) {
			lambda.Parameters[i].Type = sig.Parameters[i].Type
		}
	}
	if lambda.ReturnType.Type == nil && isConcreteEnoughToElaborate(sig.ReturnType.Type) {
		lambda.ReturnType.Type = sig.ReturnType.Type
	}
}

// isConcreteEnoughToElaborate reports whether a type from the expected signature can be
// written onto the lambda as if the programmer had.
//
// A type still mentioning a **type variable** cannot. In `map(m, (x) => x * 2)` against
// `(m: Maybe<t>, f: (t) -> u) -> Maybe<u>`, `t` is solved from `m` but `u` is solved from
// *this lambda's own body* — planting `u` as the declared return type instead leaves it
// unsolved forever, and the call fails downstream with "cannot convert u to u8". Leaving it
// blank is what lets the body infer `i64` and unification then bind `u` to it.
//
// The parameter side is unaffected: by the time the deferred pass runs, `(t) -> u`'s
// parameter has already become `(i64) -> u`, so `x` is filled and only the return is left.
func isConcreteEnoughToElaborate(t types.Type) bool {
	if t == nil {
		return false
	}
	vars := map[string]bool{}
	collectTypeVars(t, vars)
	return len(vars) == 0
}

// elaborateLambdaArgs fills the lambda-literal arguments of a call from the parameter types
// the callee declares, before any of them is inferred.
//
// Separate from the per-argument loops that follow it because the *order* matters: a
// contextually-typed lambda has to be elaborated before the argument walk begins, since that
// walk infers each argument as it goes and a lambda inferred without its parameter types has
// already reported `undefined symbol` by the time the loop reaches the comparison.
func (tc *TypeChecker) elaborateLambdaArgs(params []types.ParameterType, args []ast.Expression) {
	for i, arg := range args {
		if i >= len(params) {
			return
		}
		tc.elaborateLambda(arg, params[i].Type)
	}
}

// elaborateLambdaArgsFromParams is elaborateLambdaArgs for a callee known by its
// *declaration* rather than by a signature — the ordinary direct-call path, where the
// parameter types live on `ast.Parameter` instead of `types.ParameterType`.
func (tc *TypeChecker) elaborateLambdaArgsFromParams(params []ast.Parameter, args []ast.Expression) {
	for i, arg := range args {
		if i >= len(params) {
			return
		}
		tc.elaborateLambda(arg, params[i].Type)
	}
}

// needsContextualTypes reports whether expr is a lambda literal that is missing an
// annotation the context would supply — the case that has to be elaborated before it can be
// inferred at all.
//
// A fully annotated lambda answers false: it needs nothing from context, and treating it as
// contextual would defer it in solveTypeVars and lose its ability to *solve* a type variable
// from its own signature.
func needsContextualTypes(expr ast.Expression) bool {
	lambda, ok := expr.(*ast.LambdaExpr)
	if !ok || len(lambda.LambdaClauses) > 0 {
		return false
	}
	if lambda.ReturnType.Type == nil {
		return true
	}
	for i := range lambda.Parameters {
		if lambda.Parameters[i].Type == nil {
			return true
		}
	}
	return false
}
