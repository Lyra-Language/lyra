package checker

import (
	"fmt"

	"github.com/Lyra-Language/lyra/pkg/ast"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/target"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// CheckInterruptHandlers reports an `@interrupt(…)` function that cannot be one
// (lyra-E087): a handler is called by the runtime's entry stub with nothing, so it takes
// nothing and returns nothing; it is exported under one fixed name per interrupt, so it
// is top level, not generic, and the only one of its kind; and it exists only on a
// console, where the runtime has vectors to put it in.
func CheckInterruptHandlers(program *ast.Program, tgt target.Target) []diag.Diagnostic {
	var diags []diag.Diagnostic
	report := func(loc ast.Location, format string, args ...any) {
		diags = append(diags, diag.Diagnostic{
			Location: loc,
			Severity: diag.SeverityError,
			Code:     diag.CodeInterruptHandler,
			Message:  fmt.Sprintf(format, args...),
		})
	}
	topLevel := map[*ast.LambdaExpr]*ast.VarDeclStmt{}
	for _, node := range program.Statements {
		if decl, ok := node.(*ast.VarDeclStmt); ok {
			if lambda, isFn := decl.Value.(*ast.LambdaExpr); isFn {
				topLevel[lambda] = decl
			}
		}
	}
	first := map[string]*ast.VarDeclStmt{}
	for _, node := range program.Statements {
		stmt, ok := node.(ast.Statement)
		if !ok {
			continue
		}
		ast.WalkStmt(stmt, nil, func(e ast.Expression) bool {
			lambda, isFn := e.(*ast.LambdaExpr)
			if !isFn || lambda.Interrupt == "" {
				return true
			}
			decl, top := topLevel[lambda]
			switch {
			case !top:
				report(lambda.GetLocation(), "an `@interrupt(%s)` handler is a top-level function; this one is inside another", lambda.Interrupt)
			case tgt.Host:
				report(decl.GetLocation(),
					"`@interrupt(%s)` runs on a console's interrupt, and this program is built for the host; a `lyra.toml` beside it sets the target (`target = \"genesis\"`)",
					lambda.Interrupt)
			case len(lambda.Parameters) != 0 || !returnsVoid(lambda):
				report(decl.GetLocation(),
					"an `@interrupt(%s)` handler takes nothing and returns nothing — `() -> void` — since the interrupt calls it with nothing and ignores what it answers",
					lambda.Interrupt)
			case len(decl.GenericParams) != 0:
				report(decl.GetLocation(), "an `@interrupt(%s)` handler cannot be generic: one function is exported for the interrupt", lambda.Interrupt)
			default:
				if prev, dup := first[lambda.Interrupt]; dup {
					report(decl.GetLocation(), "a second `@interrupt(%s)` handler; %q is already the one, at %s",
						lambda.Interrupt, prev.Name, prev.GetLocation().Pretty())
				} else {
					first[lambda.Interrupt] = decl
				}
			}
			return true
		})
	}
	return diags
}

// returnsVoid reports whether lambda's declared result is void (or unwritten, which a
// block body with no value is).
func returnsVoid(lambda *ast.LambdaExpr) bool {
	t := lambda.ReturnType.Type
	if t == nil {
		return true
	}
	_, isVoid := t.(types.VoidType)
	return isVoid
}
