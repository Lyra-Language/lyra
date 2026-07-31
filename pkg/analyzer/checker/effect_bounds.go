package checker

import (
	"fmt"

	"github.com/Lyra-Language/lyra/pkg/ast"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// EffectBoundsError reports an invalid combination of effect-bound modifiers
// (`pure`, `det`, `noalloc`) on a function or trait method.
type EffectBoundsError struct {
	Code     string
	Message  string
	Location ast.Location
}

func (e EffectBoundsError) Error() string {
	return fmt.Sprintf("%s: %s", e.Location.Pretty(), e.Message)
}

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
func CheckEffectBounds(program *ast.Program) []EffectBoundsError {
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
				c.checkBorrowModes(&td.Methods[i], td.GetLocation())
			}
		}
	}
	return c.errors
}

type effectBoundsChecker struct {
	errors []EffectBoundsError
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

// checkBorrowModes rejects `own` on a trait method's parameter.
//
// `ref` and `mut` are supported: a borrow is retained and released by nobody, so the
// ownership pass needs to know nothing about the method to stay correct. `own` transfers,
// making the callee's parameter an owning binding — and the ownership pass does not analyze
// trait-method bodies, so nothing records that a returned `own` parameter was transferred
// rather than dropped. That combination is a heap-use-after-free, confirmed under ASan, not
// a diagnostic gap: the program type-checks and miscompiles.
//
// Rejecting is therefore the honest state until `pkg/analyzer/ownership` learns about method
// bodies. The message says which modes do work, since the reader's next move is to pick one.
func (c *effectBoundsChecker) checkBorrowModes(m *ast.TraitMethod, loc ast.Location) {
	if m.Signature == nil {
		return
	}
	for _, p := range m.Signature.Parameters {
		if p.Borrow != types.Own {
			continue
		}
		c.errors = append(c.errors, EffectBoundsError{
			Code: diag.CodeUnsupportedTraitBorrow,
			Message: "`own` on a trait method parameter is not supported yet: ownership analysis does " +
				"not cover trait-method bodies, so a transferred value would be dropped by the callee " +
				"and still used by the caller — use `ref` or `mut` (borrows), or take the value by " +
				"default (also a borrow)",
			Location: loc,
		})
		return
	}
}

// check flags the single mutually exclusive correctness-axis combination:
// a callable marked both `pure` and `det`.
func (c *effectBoundsChecker) check(isPure, isDet bool, loc ast.Location) {
	if isPure && isDet {
		c.errors = append(c.errors, EffectBoundsError{
			Code:     diag.CodeConflictingEffectBounds,
			Message:  "`pure` and `det` are mutually exclusive: `pure` already implies determinism (it is the stronger bound) — use one, not both",
			Location: loc,
		})
	}
}
