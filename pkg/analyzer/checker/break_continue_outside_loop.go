package checker

import (
	"fmt"

	"github.com/Lyra-Language/lyra/pkg/ast"
)

// BreakContinueOutsideLoopError reports a break or continue that appears outside
// any loop body, or a labeled break/continue whose label is not in scope.
type BreakContinueOutsideLoopError struct {
	Message  string
	Location ast.Location
}

func (e BreakContinueOutsideLoopError) Error() string {
	return fmt.Sprintf("%s: %s", e.Location.Pretty(), e.Message)
}

// CheckBreakContinueOutsideLoop walks the program AST and reports any break or
// continue statement that appears outside a loop body, as well as labeled
// break/continue statements whose label does not name an enclosing loop.
func CheckBreakContinueOutsideLoop(program *ast.Program) []BreakContinueOutsideLoopError {
	c := &bclChecker{}
	for _, node := range program.Statements {
		if stmt, ok := node.(ast.Statement); ok {
			ast.WalkStmt(stmt, c.stmtVisitor(0, nil), c.exprVisitor(0, nil))
		}
	}
	return c.errors
}

type bclChecker struct {
	errors []BreakContinueOutsideLoopError
}

func (c *bclChecker) reportBreak(loc ast.Location, label string) {
	var msg string
	if label != "" {
		msg = fmt.Sprintf("break with unknown label %q", label)
	} else {
		msg = "break statement outside of a loop"
	}
	c.errors = append(c.errors, BreakContinueOutsideLoopError{Message: msg, Location: loc})
}

func (c *bclChecker) reportContinue(loc ast.Location, label string) {
	var msg string
	if label != "" {
		msg = fmt.Sprintf("continue with unknown label %q", label)
	} else {
		msg = "continue statement outside of a loop"
	}
	c.errors = append(c.errors, BreakContinueOutsideLoopError{Message: msg, Location: loc})
}

func hasLabel(labels []string, label string) bool {
	for _, l := range labels {
		if l == label {
			return true
		}
	}
	return false
}

func (c *bclChecker) stmtVisitor(loopDepth int, labels []string) func(ast.Statement) bool {
	return func(stmt ast.Statement) bool {
		switch s := stmt.(type) {
		case *ast.BreakStmt:
			if s.Label == "" {
				if loopDepth == 0 {
					c.reportBreak(stmt.GetLocation(), "")
				}
			} else if !hasLabel(labels, s.Label) {
				c.reportBreak(stmt.GetLocation(), s.Label)
			}
		case *ast.ContinueStmt:
			if s.Label == "" {
				if loopDepth == 0 {
					c.reportContinue(stmt.GetLocation(), "")
				}
			} else if !hasLabel(labels, s.Label) {
				c.reportContinue(stmt.GetLocation(), s.Label)
			}
		}
		return true
	}
}

func (c *bclChecker) exprVisitor(loopDepth int, labels []string) func(ast.Expression) bool {
	return func(expr ast.Expression) bool {
		switch e := expr.(type) {
		case *ast.LambdaExpr:
			// Lambda body is a new function scope — loop context resets.
			for i := range e.Parameters {
				ast.WalkExpr(e.Parameters[i].DefaultValue, c.stmtVisitor(0, nil), c.exprVisitor(0, nil))
			}
			ast.WalkExpr(e.Body, c.stmtVisitor(0, nil), c.exprVisitor(0, nil))
			for _, clause := range e.LambdaClauses {
				ast.WalkExpr(clause.Body, c.stmtVisitor(0, nil), c.exprVisitor(0, nil))
			}
			return false

		case *ast.ForLoopExpr:
			newDepth := loopDepth + 1
			newLabels := labels
			if e.Label != "" {
				newLabels = append(append([]string{}, labels...), e.Label)
			}
			// Init/condition/post are outside the loop body proper.
			if e.Init != nil {
				ast.WalkExpr(e.Init.Value, c.stmtVisitor(loopDepth, labels), c.exprVisitor(loopDepth, labels))
			}
			if e.Condition != nil {
				ast.WalkExpr(*e.Condition, c.stmtVisitor(loopDepth, labels), c.exprVisitor(loopDepth, labels))
			}
			if e.Post != nil {
				ast.WalkExpr(*e.Post, c.stmtVisitor(loopDepth, labels), c.exprVisitor(loopDepth, labels))
			}
			for _, stmt := range e.Body.Statements {
				ast.WalkStmt(stmt, c.stmtVisitor(newDepth, newLabels), c.exprVisitor(newDepth, newLabels))
			}
			return false

		case *ast.ForInLoopExpr:
			newDepth := loopDepth + 1
			newLabels := labels
			if e.Label != "" {
				newLabels = append(append([]string{}, labels...), e.Label)
			}
			ast.WalkExpr(e.Iterable, c.stmtVisitor(loopDepth, labels), c.exprVisitor(loopDepth, labels))
			for _, stmt := range e.Body.Statements {
				ast.WalkStmt(stmt, c.stmtVisitor(newDepth, newLabels), c.exprVisitor(newDepth, newLabels))
			}
			return false
		}
		return true
	}
}
