package typechecker

import (
	"github.com/Lyra-Language/lyra/pkg/ast"
)

// Multi-clause functions.
//
//	let fib = (n: i64, a: i64, b: i64) -> i64 {
//	  (0, a, _) => a,
//	  (n, a, b) => fib(n - 1, b, a + b),
//	}
//
// A multi-clause function *is* a match on its parameters, so it is desugared into one rather
// than given a lowering of its own: the body becomes `match (n, a, b) { … }`, built from the
// same parameter names the head declares. Everything the backend already does for a match —
// the if-else ladder, tuple destructuring, guards, the trapping fall-through — then applies
// unchanged, which is why this is a front-end elaboration and not a codegen feature.
//
// **It has to happen in the front end**, before the body is checked. The backend reads every
// type by AST-node identity (`recordedType`), so a match synthesized *there* would have no
// entry for any of its nodes; synthesizing it here means the typechecker types it like any
// other match and the ownership, capture and lowering passes need no changes at all.
//
// What falls out for free, and is worth knowing rather than rediscovering:
//   - **A call matching no clause traps** (`sealMatchFallthrough`): `lyra: match not
//     exhaustive`, exit 101, rather than undefined behaviour. That is the right semantics for
//     a function-clause error and matches Erlang/Elixir.
//   - **Generic multi-clause functions work**, because the body is now ordinary. The
//     backend's "a multi-clause generic function is not implemented yet" is unreachable once
//     every multi-clause lambda arrives desugared.

// desugarClauses rewrites a multi-clause lambda into a single body that matches on its
// parameters. It is a no-op for an ordinary lambda, and idempotent — a lambda checked twice
// (the typechecker revisits nodes) desugars once, since the clauses are consumed.
//
// Returns false when the clauses cannot be desugared, in which case a diagnostic has been
// reported and the lambda is left alone rather than half-rewritten.
func (tc *TypeChecker) desugarClauses(funcName string, lambda *ast.LambdaExpr) bool {
	if lambda == nil || len(lambda.LambdaClauses) == 0 {
		return false
	}
	// A clause list binds the parameters positionally, so each clause must have exactly as
	// many patterns as the head declares. Checked here, with the counts named: left to the
	// synthesized match it would surface as a tuple-shape mismatch about a tuple the
	// programmer never wrote.
	for i := range lambda.LambdaClauses {
		clause := &lambda.LambdaClauses[i]
		if len(clause.Patterns) != len(lambda.Parameters) {
			tc.addError(clause.GetLocation(), SeverityError,
				"%s: clause %d binds %d parameter(s), but the function declares %d",
				funcName, i+1, len(clause.Patterns), len(lambda.Parameters))
			return false
		}
	}
	scrutinee, ok := tc.clauseScrutinee(funcName, lambda)
	if !ok {
		return false
	}

	arms := make([]ast.MatchArm, len(lambda.LambdaClauses))
	for i := range lambda.LambdaClauses {
		clause := &lambda.LambdaClauses[i]
		arms[i] = ast.MatchArm{
			Pattern: clausePattern(clause),
			Guard:   clause.Guard,
			Body:    clause.Body,
		}
	}
	lambda.Body = &ast.MatchExpr{
		ExprBase:  ast.ExprBase{AstBase: ast.AstBase{Location: lambda.GetLocation()}},
		Scrutinee: scrutinee,
		MatchArms: arms,
	}
	// Consumed: the clauses now live as match arms, and leaving them in place would make
	// checkLambdaBody check every body a second time — one mistake, two diagnostics.
	lambda.LambdaClauses = nil
	return true
}

// clauseScrutinee builds the value the clauses match against: the parameter itself when
// there is one, a tuple of them otherwise.
//
// A one-parameter function deliberately does *not* get a one-element tuple. `(n: i64) { 0 =>
// …, n => … }` should match on the integer directly, which is what makes it lower to the
// scalar ladder rather than through an aggregate that exists only in the desugaring.
func (tc *TypeChecker) clauseScrutinee(funcName string, lambda *ast.LambdaExpr) (ast.Expression, bool) {
	refs := make([]ast.Expression, len(lambda.Parameters))
	for i := range lambda.Parameters {
		name := lambda.Parameters[i].Pattern.GetName()
		if name == "" {
			// A destructured parameter in the *head* has no single name to scrutinize.
			// Clauses already destructure, so the head should bind plainly.
			tc.addError(lambda.Parameters[i].GetLocation(), SeverityError,
				"%s: a multi-clause function's parameters must be plain names — the clauses do the destructuring",
				funcName)
			return nil, false
		}
		refs[i] = &ast.IdentifierExpr{
			ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: lambda.Parameters[i].GetLocation()}},
			Name:     name,
		}
	}
	if len(refs) == 1 {
		return refs[0], true
	}
	return &ast.TupleLiteralExpr{
		ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: lambda.GetLocation()}},
		Elements: refs,
	}, true
}

// clausePattern is the arm pattern for one clause: its single pattern when the function takes
// one parameter, a tuple pattern over them otherwise — mirroring clauseScrutinee, since the
// two have to agree on shape.
func clausePattern(clause *ast.LambdaClause) ast.Pattern {
	if len(clause.Patterns) == 1 {
		return clause.Patterns[0]
	}
	return &ast.TuplePattern{
		PatternBase: ast.PatternBase{AstBase: ast.AstBase{Location: clause.GetLocation()}},
		Elements:    clause.Patterns,
	}
}
