package typechecker

import (
	"github.com/Lyra-Language/lyra/pkg/ast"
)

// Default parameter values.
//
//	let fib = (n: i64, a: i64 = 0, b: i64 = 1) -> i64 { … }
//	fib(10)   // a and b come from the declaration
//
// The grammar, collector and arity check already understood defaults — what was missing is
// that the *call site* never received them, so the backend saw a call with fewer arguments
// than the function had parameters and refused the function outright.
//
// They are filled in here, in the front end, by appending the declaration's default
// expressions to the call's argument list before anything else looks at it. The rest of the
// pipeline then sees a call that passes every argument explicitly: the arguments are
// type-checked against their parameters like any other, the widths propagate, and the
// backend needs no notion of defaults at all — the same reasoning as the multi-clause
// desugaring and contextual lambdas.
//
// **Filling is idempotent**, which matters because the typechecker revisits nodes: after the
// first pass the argument count equals the parameter count, so a second pass adds nothing.
//
// One deliberate sharing: the appended expression is the *same AST node* as the declaration's
// default, not a copy. Two call sites that both omit an argument therefore share it. That is
// sound for everything keyed by node — the recorded type is the parameter's type at every
// site, and the width propagated is the same — because a default is evaluated against the
// parameter's declared type and cannot vary by caller. Cloning would need a deep AST copy,
// which this compiler deliberately avoids everywhere else (see the monomorphizer's note on
// substitution over cloning).

// applyDefaultArguments appends the callee's default expressions for any trailing arguments
// the call omits. It stops at the first parameter with no default, leaving the arity check to
// report a genuinely missing argument.
func (tc *TypeChecker) applyDefaultArguments(lambda *ast.LambdaExpr, call *ast.FunctionCallExpr) {
	if lambda == nil || call == nil {
		return
	}
	for i := len(call.Arguments); i < len(lambda.Parameters); i++ {
		def := lambda.Parameters[i].DefaultValue
		if def == nil {
			return
		}
		call.Arguments = append(call.Arguments, def)
	}
}

// checkDefaultsAreTrailing reports a parameter with a default value followed by one without.
//
// Positional arguments fill left to right, so `(a: i64 = 1, b: i64)` cannot be called with
// one argument in any useful way: the value would bind to `a`, and `b` — which has no default
// — would be left unfilled. Worse than useless, it was silently *accepted*: the arity check
// counts required parameters rather than checking their order, so `f(5)` bound 5 to `a` and
// then had nothing for `b`, which is a call the programmer clearly did not mean.
//
// Every language with positional defaults imposes this rule for the same reason (Python, C++,
// TypeScript). Named arguments would lift it; Lyra has none.
func (tc *TypeChecker) checkDefaultsAreTrailing(funcName string, lambda *ast.LambdaExpr) {
	seenDefault := false
	for i := range lambda.Parameters {
		p := &lambda.Parameters[i]
		if p.DefaultValue != nil {
			seenDefault = true
			continue
		}
		if seenDefault {
			tc.addError(p.GetLocation(), SeverityError,
				"%s: parameter %q has no default but follows one that does; "+
					"parameters with defaults must come last, since arguments fill left to right",
				funcName, p.Pattern.GetName())
			return
		}
	}
}
