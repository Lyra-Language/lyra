package checker

import (
	"fmt"
	"strings"

	"github.com/Lyra-Language/lyra/pkg/ast"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
)

// CheckUnusedParameters walks the program AST and reports lambda parameters
// that are never referenced within their lambda body. Top-level lambda
// parameters are still checked. The returned diagnostics carry TagUnnecessary.
func CheckUnusedParameters(program *ast.Program) []diag.Diagnostic {
	c := &unusedParamChecker{}
	for _, node := range program.Statements {
		if stmt, ok := node.(ast.Statement); ok {
			c.walkStmt(stmt)
		}
	}
	return c.warnings
}

type unusedParamChecker struct {
	warnings []diag.Diagnostic
}

func (c *unusedParamChecker) walkStmt(stmt ast.Statement) {
	if stmt == nil {
		return
	}
	ast.WalkStmt(stmt, nil, func(e ast.Expression) bool {
		if lambda, ok := e.(*ast.LambdaExpr); ok {
			c.checkLambda(lambda)
			return false
		}
		return true
	})
}

func (c *unusedParamChecker) checkLambda(lambda *ast.LambdaExpr) {
	// Check main body params (non-clause form).
	if lambda.Body != nil && len(lambda.Parameters) > 0 {
		refs := collectRefs(lambda.Body)
		for _, param := range lambda.Parameters {
			for _, name := range patternBoundNames(param.Pattern) {
				if strings.HasPrefix(name, "_") {
					continue
				}
				if !refs[name] {
					c.warnings = append(c.warnings, diag.Diagnostic{
						Location: param.GetLocation(),
						Severity: diag.SeverityWarning,
						Code:     diag.CodeUnusedParameter,
						Message:  fmt.Sprintf("parameter %q is never used", name),
						Tags:     []diag.Tag{diag.TagUnnecessary},
					})
				}
			}
		}
	}

	// Check clause-form lambdas: each clause has its own patterns and body.
	for _, clause := range lambda.LambdaClauses {
		if clause.Body == nil {
			continue
		}
		refs := collectRefs(clause.Body)
		for _, pat := range clause.Patterns {
			for _, name := range patternBoundNames(pat) {
				if strings.HasPrefix(name, "_") {
					continue
				}
				if !refs[name] {
					c.warnings = append(c.warnings, diag.Diagnostic{
						Location: clause.GetLocation(),
						Severity: diag.SeverityWarning,
						Code:     diag.CodeUnusedParameter,
						Message:  fmt.Sprintf("parameter %q is never used", name),
						Tags:     []diag.Tag{diag.TagUnnecessary},
					})
				}
			}
		}
	}

	// Recurse into bodies to find nested lambdas.
	if lambda.Body != nil {
		ast.WalkExpr(lambda.Body, nil, func(e ast.Expression) bool {
			if nested, ok := e.(*ast.LambdaExpr); ok {
				c.checkLambda(nested)
				return false
			}
			return true
		})
	}
	for _, clause := range lambda.LambdaClauses {
		if clause.Body != nil {
			ast.WalkExpr(clause.Body, nil, func(e ast.Expression) bool {
				if nested, ok := e.(*ast.LambdaExpr); ok {
					c.checkLambda(nested)
					return false
				}
				return true
			})
		}
	}
}

// collectRefs returns a set of all identifier names referenced within expr,
// including references inside nested lambdas (to avoid false positives on
// parameters closed over by inner functions).
func collectRefs(expr ast.Expression) map[string]bool {
	refs := make(map[string]bool)
	if expr == nil {
		return refs
	}
	ast.WalkExpr(expr,
		func(s ast.Statement) bool {
			if r, ok := s.(*ast.VarReassignmentStmt); ok {
				refs[r.Name] = true
			}
			return true
		},
		func(e ast.Expression) bool {
			switch ex := e.(type) {
			case *ast.IdentifierExpr:
				refs[ex.Name] = true
			case *ast.MathAssignOpExpr:
				refs[rootIdentName(ex.Left)] = true
			}
			return true
		},
	)
	return refs
}
