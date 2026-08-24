package checker

import (
	"github.com/Lyra-Language/lyra/pkg/ast"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
)

// CheckEffectBounds walks the program AST and reports any callable annotated
// with a contradictory pair of effect bounds. The bounds form two axes:
//
//   - correctness: `pure` ⊆ `det` ⊆ unannotated. `pure` and `det` are two rungs
//     of the *same* axis, so annotating both is contradictory — `pure` is the
//     stronger guarantee and already implies determinism.
//   - resource: `noalloc`. Orthogonal — it stacks freely with any correctness
//     rung (`pure noalloc`, `det noalloc`), so it is never part of a conflict.
//
// Today the only conflict is therefore `pure` together with `det`.
func CheckEffectBounds(program *ast.Program) []diag.Diagnostic {
	c := &effectBoundsChecker{}
	for _, node := range program.Statements {
		if stmt, ok := node.(ast.Statement); ok {
			ast.WalkStmt(stmt, c.stmtVisitor(), c.exprVisitor())
		}
		// A trait method carries its bounds on the TraitMethodImpl struct, not
		// on a LambdaExpr (its body is a LambdaClause), so the expression walk
		// above never reaches them — inspect them directly.
		if impl, ok := node.(*ast.TraitImplStmt); ok {
			for i := range impl.Methods {
				m := &impl.Methods[i]
				c.check(m.IsPure, m.IsDet, m.Clause.GetLocation())
			}
		}
		// Bounds declared on a trait method (`trait X { pure show: … }`) are on the
		// TraitMethod struct, likewise unreachable by the expression walk.
		if td, ok := node.(*ast.TraitDeclStmt); ok {
			for i := range td.Methods {
				c.check(td.Methods[i].IsPure, td.Methods[i].IsDet, td.GetLocation())
			}
		}
	}
	return c.errors
}

type effectBoundsChecker struct {
	errors []diag.Diagnostic
}

func (c *effectBoundsChecker) stmtVisitor() func(ast.Statement) bool {
	return func(ast.Statement) bool { return true }
}

func (c *effectBoundsChecker) exprVisitor() func(ast.Expression) bool {
	return func(expr ast.Expression) bool {
		if e, ok := expr.(*ast.LambdaExpr); ok {
			c.check(e.IsPure, e.IsDet, e.GetLocation())
		}
		return true
	}
}

// check flags the single mutually exclusive correctness-axis combination:
// a callable marked both `pure` and `det`.
func (c *effectBoundsChecker) check(isPure, isDet bool, loc ast.Location) {
	if isPure && isDet {
		c.errors = append(c.errors, diag.Diagnostic{Severity: diag.SeverityError,
			Code:     diag.CodeConflictingEffectBounds,
			Message:  "`pure` and `det` are mutually exclusive: `pure` already implies determinism (it is the stronger bound) — use one, not both",
			Location: loc,
		})
	}
}
