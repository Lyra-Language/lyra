package checker

import (
	"fmt"

	"github.com/Lyra-Language/lyra/pkg/ast"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// CheckInertBorrowModifiers warns about an `own`/`ref` modifier on a parameter
// whose type is a copied scalar primitive (`lyra-W010`).
//
// `own` transfers ownership (the callee adopts and releases the value, and may reuse
// its box) and `ref` lends it read-only. A scalar — a numeric primitive, `bool`, or
// `rune` — has no interior to borrow into and no reference to transfer, so both
// collapse to a plain parameter. **`mut` is not inert on a scalar** (09/27): it is
// passed by reference and the callee's writes reach the caller (types.IsByRefParam). The modifier does nothing, but it
// tells a reader the value is moved or borrowed, so it is worse than absent: it
// misleads. This is the same class of inert/misleading annotation Lyra already
// flags (an unused parameter, `pure` + `det` together).
//
// Deliberately *not* flagged:
//
//   - `string` — a `PrimitiveType` in the grammar, but a managed fat pointer, so
//     `own string` is a genuine move (see the use-after-move check). Excluded via
//     types.IsString.
//   - a generic type parameter (`own t`) — not a scalar; at monomorphization it may
//     become a managed type, and Perceus needs `own` to be uniform across type
//     variables. A `GenericType` is not a `PrimitiveType`, so it is naturally skipped.
//   - a struct/tuple/`data` type — an aggregate can own managed fields, so `own`
//     there is meaningful (even where the drop of those fields is still a WIP).
//
// A warning, not an error: the code is correct as written, and forbidding the
// modifier would be a hard wall with real edge cases (the two exclusions above),
// so this steers rather than blocks. Scope is parameters written on a lambda
// (every free function and nested lambda); a trait method's declared signature
// carries its parameter conventions in a `types.LambdaType` without a written
// modifier per parameter, so there is nothing to flag there.
func CheckInertBorrowModifiers(program *ast.Program) []diag.Diagnostic {
	var out []diag.Diagnostic
	onExpr := func(e ast.Expression) bool {
		lam, ok := e.(*ast.LambdaExpr)
		if !ok {
			return true
		}
		for i := range lam.Parameters {
			if d := inertModifierDiagnostic(&lam.Parameters[i]); d != nil {
				out = append(out, *d)
			}
		}
		return true
	}
	for _, s := range program.Statements {
		if stmt, ok := s.(ast.Statement); ok {
			ast.WalkStmt(stmt, nil, onExpr)
		}
	}
	return out
}

// inertModifierDiagnostic returns a warning if p carries a borrow/ownership
// modifier on a scalar primitive type, or nil otherwise.
func inertModifierDiagnostic(p *ast.Parameter) *diag.Diagnostic {
	if p.TypeModifier == "" {
		return nil // a bare parameter — nothing declared to be inert
	}
	if p.TypeModifier == types.Mut || !types.IsCopiedScalar(p.Type) {
		return nil // `mut` writes through; a non-scalar (or the managed `string`) may own or borrow
	}
	prim := p.Type.(types.PrimitiveType)
	name := "the parameter"
	if ip, ok := p.Pattern.(*ast.IdentifierPattern); ok {
		name = fmt.Sprintf("parameter %q", ip.Name)
	}
	return &diag.Diagnostic{
		Location: p.GetLocation(),
		Severity: diag.SeverityWarning,
		Code:     diag.CodeInertBorrowModifier,
		Message: fmt.Sprintf(
			"%s: the `%s` modifier has no effect on `%s` — a scalar is passed by value, with no interior to borrow or reference to transfer, so `own`/`ref` are equivalent to a plain `%s` here. Drop the modifier (it otherwise implies move/borrow semantics that don't apply), or write `mut` if the callee should update the caller's value",
			name, p.TypeModifier, prim.Name, prim.Name),
	}
}
