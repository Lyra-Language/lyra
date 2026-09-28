package typechecker

import (
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// The last statement of a block whose value is used — a function body with a return
// type, the block of a value-position `if` branch or `let` initializer — and what it
// means when that statement is not an expression. Both walks that treat a tail as a
// value ask here: checkBlock (inferBlockType) and its twin checkBlockReturn.
//
// Until 09/28 neither did: a tail that was a statement typed as "unknown", which every
// consumer skips, so `-> u8 => { let x = 1 }` and `if let Some(v) = m { v } else { 0 }`
// in tail position type-checked and the backend refused them ("block has no value").

// valueTail rewrites a block's trailing `if let p = v { A } else { B }` into the
// expression it means in value position, `match v { p => A, _ => B }`, and answers the
// tail as an expression statement — or nil when the tail is not one.
//
// **A rewrite rather than a second value-producing construct.** `if let` is a statement
// in every pass (its branches are checked for effect), and teaching each of them — the
// typechecker's join, ownership, purity, use-after-move, must-release, both lowerings — a
// value form is the sibling-construct drift rule 8 warns about. The `match` is already a
// value everywhere, and the two mean the same thing. Its arm binds through the pattern
// overlay (withPatternBindings), so the scope the collector recorded for the `if let` is
// not consulted, and the `then` block keeps its own.
//
// Left a statement: no `else` (there is no value when the pattern fails), a `weak`
// scrutinee (an `if let` over one *upgrades*, which no `match` does), an annotated or
// `mut` pattern (a match arm has neither), and a keyword other than `let`.
func (tc *TypeChecker) valueTail(block *ast.BlockExpr) *ast.ExpressionStmt {
	if block == nil || len(block.Statements) == 0 {
		return nil
	}
	last := len(block.Statements) - 1
	switch s := block.Statements[last].(type) {
	case *ast.ExpressionStmt:
		return s
	case *ast.IfDestructuringStmt:
		d := &s.DestructuringStatement
		if s.Else == nil || d.Value == nil || d.Type != nil || d.IsMut || d.Keyword != "let" {
			return nil
		}
		if t := tc.inferExprType(d.Value); t != nil {
			if _, isWeak := types.StripNewtype(t).(types.WeakType); isWeak {
				return nil
			}
		}
		loc := s.GetLocation()
		match := &ast.MatchExpr{
			ExprBase:  ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
			Scrutinee: d.Value,
			MatchArms: []ast.MatchArm{
				{Pattern: d.Pattern, Body: s.Then},
				{
					Pattern: &ast.WildcardPattern{PatternBase: ast.PatternBase{AstBase: ast.AstBase{Location: s.Else.GetLocation()}}},
					Body:    s.Else,
				},
			},
		}
		stmt := &ast.ExpressionStmt{AstBase: ast.AstBase{Location: loc}, Expression: match}
		block.Statements[last] = stmt
		return stmt
	}
	return nil
}

// tailWithoutValue is the type of a block whose last statement is not an expression:
// `never` when the block cannot fall off its end (a `return`, `break` or `continue`, or a
// statement whose every path leaves), `void` otherwise — which the consumer's own check
// then refuses wherever a value was wanted, naming what it wanted.
func (tc *TypeChecker) tailWithoutValue(block *ast.BlockExpr) types.Type {
	if tc.blockCannotFallThrough(block) {
		return types.NeverType{}
	}
	return types.VoidType{}
}

// blockCannotFallThrough reports whether control can leave block only by a jump: some
// statement in it leaves every time. The shape of checker.letElseDiverges, which asks the
// same question of an `else`; it reads the TypeTable for `never`, so a `panic(…)` or a
// `for { … }` with no `break` counts without being named.
func (tc *TypeChecker) blockCannotFallThrough(block *ast.BlockExpr) bool {
	if block == nil {
		return false
	}
	for _, s := range block.Statements {
		if tc.stmtCannotFallThrough(s) {
			return true
		}
	}
	return false
}

func (tc *TypeChecker) stmtCannotFallThrough(s ast.Statement) bool {
	switch v := s.(type) {
	case *ast.ReturnStmt, *ast.BreakStmt, *ast.ContinueStmt:
		return true
	case *ast.ExpressionStmt:
		return tc.exprCannotFallThrough(v.Expression)
	case *ast.IfDestructuringStmt:
		return v.Else != nil && tc.blockCannotFallThrough(v.Then) && tc.blockCannotFallThrough(v.Else)
	}
	return false
}

func (tc *TypeChecker) exprCannotFallThrough(e ast.Expression) bool {
	if e == nil {
		return false
	}
	if t, ok := tc.typeTable.Get(e); ok {
		if _, isNever := t.(types.NeverType); isNever {
			return true
		}
	}
	switch v := e.(type) {
	case *ast.BlockExpr:
		return tc.blockCannotFallThrough(v)
	case *ast.UnsafeBlockExpr:
		return tc.blockCannotFallThrough(v.Body)
	case *ast.IfExpr:
		return v.Else != nil && tc.exprCannotFallThrough(v.Then) && tc.exprCannotFallThrough(v.Else)
	case *ast.MatchExpr:
		if len(v.MatchArms) == 0 {
			return false
		}
		for i := range v.MatchArms {
			if !tc.exprCannotFallThrough(v.MatchArms[i].Body) {
				return false
			}
		}
		return true
	}
	return false
}

// describeTailStatement names the statement a value-less body ends in, for the message.
func describeTailStatement(s ast.Statement) string {
	switch v := s.(type) {
	case *ast.IfDestructuringStmt:
		if v.Else == nil {
			return "an `if let` with no `else`"
		}
		return "an `if let`"
	case *ast.ElseDestructuringStmt:
		return "a `let … else`"
	case *ast.VarDeclStmt, *ast.DestructuringDeclStmt:
		return "a declaration"
	}
	return "a statement"
}

// tailStatementHint is the fix for the shapes that have one beyond "end with a value".
func (tc *TypeChecker) tailStatementHint(s ast.Statement) string {
	v, ok := s.(*ast.IfDestructuringStmt)
	if !ok {
		return ""
	}
	if v.Else == nil {
		return " — with an `else`, an `if let` ending the body is its value"
	}
	if t, known := tc.typeTable.Get(v.DestructuringStatement.Value); known {
		if _, isWeak := types.StripNewtype(t).(types.WeakType); isWeak {
			return " — an `if let` over a `weak` value upgrades it and has no value of its own: " +
				"assign in its branches and end with the result (`if let p = w { out = p.n } else { out = 0 }; out`)"
		}
	}
	return ""
}

// isVoidType reports whether a declared return is `void`, which a statement tail satisfies.
func isVoidType(t types.Type) bool {
	_, ok := t.(types.VoidType)
	return ok
}
