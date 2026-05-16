package typechecker

import (
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// inferBlockType returns the type of a block expression — the type of its last
// expression statement. Returns nil for an empty block or one whose last
// statement is not an ExpressionStmt (e.g. a declaration or return).
func (tc *TypeChecker) inferBlockType(block *ast.BlockExpr) types.Type {
	if len(block.Statements) == 0 {
		return nil
	}
	last := block.Statements[len(block.Statements)-1]
	if exprStmt, ok := last.(*ast.ExpressionStmt); ok {
		return tc.inferExprType(exprStmt.Expression)
	}
	return nil
}

// checkIfExpr type-checks an if(/else) expression and returns its inferred
// type. Two invariants are enforced:
//
//  1. The condition must be bool (when its type is inferable).
//  2. When an else branch is present and both branch types are inferable,
//     the branches must have mutually assignable types.
//
// One-armed ifs (no else) are not required to have a meaningful type: the
// result value is discarded when the expression is used as a statement, and
// requiring an else would break the extremely common pattern
// `if cond { do_something() }`.
func (tc *TypeChecker) checkIfExpr(expr *ast.IfExpr) types.Type {
	// ── 1. condition must be bool ────────────────────────────────────────────
	if expr.Condition != nil {
		condType := tc.inferExprType(expr.Condition)
		if condType != nil && !types.IsBoolean(condType) {
			tc.addError(expr.Condition.GetLocation(), SeverityError,
				"if condition must be boolean, got %s", condType)
		}
	}

	// ── 2. infer branch types ────────────────────────────────────────────────
	var thenType, elseType types.Type
	if expr.Then != nil {
		thenType = tc.inferExprType(expr.Then)
	}
	if expr.Else != nil {
		elseType = tc.inferExprType(expr.Else)
	}

	// ── 3. branch compatibility (only when both branches exist) ──────────────
	if expr.Else != nil && thenType != nil && elseType != nil {
		common, ok := branchCommonType(thenType, elseType)
		if !ok {
			tc.addError(expr.GetLocation(), SeverityError,
				"if/else branches have incompatible types: then is %s, else is %s",
				thenType, elseType)
			return nil
		}
		return common
	}

	// One-armed if, or at least one branch type is unresolvable.
	return thenType
}

// branchCommonType returns the common type for two if/else branches and
// whether they are compatible. Exact equality wins first; then untyped→concrete
// widening (e.g. untyped int + i32 → i32); otherwise the types are incompatible.
func branchCommonType(a, b types.Type) (types.Type, bool) {
	if types.TypesEqual(a, b) {
		return a, true
	}
	// Untyped widening: if a is assignable to b, b is the more concrete type.
	if isAssignable(a, b) {
		return b, true
	}
	// Symmetric case.
	if isAssignable(b, a) {
		return a, true
	}
	return nil, false
}
