package checker

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/captures"
	"github.com/Lyra-Language/lyra/pkg/ast"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
)

// CheckCapturedAssignment (lyra-E024) reports a lambda that assigns to a binding
// it captured from an enclosing scope.
//
// A closure captures **by value**: the copy is taken when the closure is created,
// which is what lets it outlive the frame the original binding lives in. A write
// inside the body therefore reaches the closure's own copy and nothing else —
// `var n = 5; let bump = () -> i64 => { n = n + 1  n }` leaves the outer `n` at 5,
// with the closure's copy incremented per call.
//
// Compiling that silently is the same failure the by-value `mut` parameter had:
// a write that simply vanishes, with no diagnostic either way. So it is rejected.
// The alternative reading — capture by reference, so the write does land — is not
// available without escape analysis to prove the frame outlives the closure, and
// getting it wrong is a dangling pointer rather than a lost update.
//
// It covers all three assignment forms (`n = …`, `n += …`, and a path write like
// `p.x = …`), and only fires for a name the capture pass actually recorded, so a
// lambda writing to its own local or parameter is untouched.
func CheckCapturedAssignment(program *ast.Program, caps *captures.Table) []diag.Diagnostic {
	if caps == nil {
		return nil
	}
	var out []diag.Diagnostic
	onExpr := func(e ast.Expression) bool {
		fn, ok := e.(*ast.LambdaExpr)
		if !ok {
			return true
		}
		captured := map[string]bool{}
		for _, c := range caps.Of(fn) {
			captured[c.Name] = true
		}
		if len(captured) > 0 {
			// Only this lambda's own body is walked for writes; a nested lambda's are
			// reported against *it* (the walk reaches it separately), against its own
			// capture set — which is the right attribution, since it captured from
			// here in turn.
			out = append(out, capturedWrites(fn, captured)...)
		}
		return true
	}
	for _, node := range program.Statements {
		switch n := node.(type) {
		case ast.Statement:
			ast.WalkStmt(n, nil, onExpr)
		case ast.Expression:
			ast.WalkExpr(n, nil, onExpr)
		}
	}
	return out
}

// capturedWrites finds assignments inside a lambda whose target is one of the
// captured names.
func capturedWrites(fn *ast.LambdaExpr, captured map[string]bool) []diag.Diagnostic {
	var out []diag.Diagnostic
	report := func(name string, loc ast.Location) {
		if !captured[name] {
			return
		}
		out = append(out, diag.Diagnostic{
			Severity: diag.SeverityError,
			Code:     diag.CodeCapturedAssignment,
			Location: loc,
			Message: "cannot assign to \"" + name + "\": it is captured from an enclosing scope, and a closure captures by value — " +
				"the write would only change the closure's own copy. Return the new value instead, or pass the state in as a parameter",
		})
	}
	onStmt := func(s ast.Statement) bool {
		switch v := s.(type) {
		case *ast.VarReassignmentStmt:
			report(v.Name, v.GetLocation())
		case *ast.LValueAssignmentStmt:
			if root, ok := lvalueRootName(v.Target); ok {
				report(root, v.GetLocation())
			}
		}
		return true
	}
	onExpr := func(e ast.Expression) bool {
		// Stop at a nested lambda. Its writes are reported against *it* when the outer
		// walk reaches it, against its own capture set — which is the right attribution,
		// since it captured from here in turn.
		//
		// The comment at the call site has always said so; the walk did not do it. Because
		// a name captured by an inner lambda is transitively captured by every enclosing
		// one (the read walk does not stop at a lambda boundary either), the same write was
		// reported once per enclosing lambda that captured the name — two identical
		// diagnostics at one location for one mistake, and more the deeper the nesting.
		if _, isLambda := e.(*ast.LambdaExpr); isLambda {
			return false
		}
		if v, ok := e.(*ast.MathAssignOpExpr); ok {
			report(rootIdentName(v.Left), v.GetLocation())
		}
		return true
	}
	bodies := []ast.Expression{fn.Body}
	for i := range fn.LambdaClauses {
		bodies = append(bodies, fn.LambdaClauses[i].Body)
	}
	for _, b := range bodies {
		ast.WalkExpr(b, onStmt, onExpr)
	}
	return out
}

// lvalueRootName walks an assignment path (`p.arr[i].x`) down to the binding it
// is rooted at.
func lvalueRootName(e ast.Expression) (string, bool) {
	for {
		switch v := e.(type) {
		case *ast.IdentifierExpr:
			return v.Name, true
		case *ast.MemberExpr:
			e = v.Object
		case *ast.IndexExpr:
			e = v.Object
		default:
			return "", false
		}
	}
}
