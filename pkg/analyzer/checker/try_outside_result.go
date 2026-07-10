package checker

import (
	"fmt"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/ast/symbols"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// TryOutsideResultError reports a `?` (try) postfix operator that appears in a
// function whose declared return type is neither Result nor Maybe — there is no
// channel for the operator to short-circuit the Err/None into.
type TryOutsideResultError struct {
	Code     string
	Message  string
	Location ast.Location
}

func (e TryOutsideResultError) Error() string {
	return fmt.Sprintf("%s: %s", e.Location.Pretty(), e.Message)
}

// CheckTryOutsideResult walks the program AST and reports any `?` operator whose
// nearest enclosing lambda does not declare a Result/Maybe return type (or which
// appears at the top level, outside any function).
//
// This is the structural ("context") half of the try-operator checks, mirroring
// CheckAwaitOutsideAsync. Whether the operand's *kind* matches the enclosing
// return kind (e.g. propagating a Maybe out of a Result-returning function) is
// checked in the typechecker, which has access to inferred types.
func CheckTryOutsideResult(program *ast.Program, symTable *symbols.SymbolTable) []TryOutsideResultError {
	c := &tryChecker{symTable: symTable}
	for _, node := range program.Statements {
		if stmt, ok := node.(ast.Statement); ok {
			// nil enclosing return => not inside any function (top level).
			ast.WalkStmt(stmt, c.stmtVisitor(), c.exprVisitor(nil))
		}
	}
	return c.errors
}

type tryChecker struct {
	errors   []TryOutsideResultError
	symTable *symbols.SymbolTable
}

func (c *tryChecker) stmtVisitor() func(ast.Statement) bool {
	return func(ast.Statement) bool { return true }
}

func (c *tryChecker) exprVisitor(enclosing *types.ReturnType) func(ast.Expression) bool {
	return func(expr ast.Expression) bool {
		switch e := expr.(type) {
		case *ast.TryExpr:
			if enclosing == nil || c.canonicalKindOfType(enclosing.Type) == "" {
				c.errors = append(c.errors, TryOutsideResultError{
					Code:     diag.CodeTryOutsideResult,
					Message:  "`?` can only be used inside a function returning Result or Maybe",
					Location: e.GetLocation(),
				})
			}
			return true

		case *ast.LambdaExpr:
			// Parameter default values run outside the lambda body, so they see
			// the *outer* enclosing return type (mirror the await checker).
			for i := range e.Parameters {
				ast.WalkExpr(e.Parameters[i].DefaultValue, c.stmtVisitor(), c.exprVisitor(enclosing))
			}
			// The nearest enclosing lambda's declared return type governs `?`
			// inside its body and clauses.
			ret := e.ReturnType
			walkLambdaBodies(e, c.stmtVisitor(), c.exprVisitor(&ret))
			return false
		}
		return true
	}
}

// canonicalKindOfType returns the canonical kind ("Result"/"Maybe") of t's type
// name, or "" if t is not a canonical Result/Maybe. It reads the same stamped
// identity the typechecker uses (via canonicalKindOfName), so the `?`-context
// check here and the operand/kind checks there agree: a function returning a
// same-named-but-differently-shaped `data Result` is not a valid `?` context.
func (c *tryChecker) canonicalKindOfType(t types.Type) string {
	var name string
	switch tt := t.(type) {
	case types.ParameterizedType:
		name = tt.Name
	case types.UnresolvedType:
		name = tt.Name
	case types.DataType:
		name = tt.Name
	default:
		return ""
	}
	return canonicalKindOfName(c.symTable, name)
}

// canonicalKindOfName resolves a type name to its canonical kind using the
// declaration stamped at collection time. A declared type is authoritative (its
// CanonicalKind, possibly ""); only an undeclared bare "Result"/"Maybe" falls
// back to the ambient legacy name recognition. Shared shape/identity resolution
// lives in the collector (resolveCanonicalTypes); this is the read side.
func canonicalKindOfName(symTable *symbols.SymbolTable, name string) string {
	if symTable != nil {
		if decl, ok := symTable.Types[name]; ok {
			return decl.CanonicalKind
		}
	}
	if name == "Result" || name == "Maybe" {
		return name
	}
	return ""
}
