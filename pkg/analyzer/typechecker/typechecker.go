package typechecker

import (
	"fmt"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/ast/symbols"
	"github.com/Lyra-Language/lyra/pkg/typetable"
	"github.com/Lyra-Language/lyra/pkg/types"
)

type TypeChecker struct {
	symTable  *symbols.SymbolTable
	typeTable *typetable.TypeTable
	scope     *symbols.Scope
	errors    []TypeError
}

func New(symTable *symbols.SymbolTable, typeTable *typetable.TypeTable) *TypeChecker {
	return &TypeChecker{
		symTable:  symTable,
		typeTable: typeTable,
		scope:     symTable.GlobalScope,
	}
}

func (tc *TypeChecker) Check(program *ast.Program) []TypeError {
	for _, stmt := range program.Statements {
		tc.checkNode(stmt)
	}
	return tc.errors
}

func (tc *TypeChecker) checkNode(node ast.AstNode) {
	switch n := node.(type) {
	case *ast.VarDeclStmt:
		tc.checkVarDecl(n)
	}
}

func (tc *TypeChecker) checkVarDecl(decl *ast.VarDeclStmt) {
	if decl.Value == nil {
		return
	}

	inferredType := tc.inferExprType(decl.Value)
	if inferredType == nil {
		return
	}

	if decl.Type == nil {
		tc.typeTable.Set(decl.Value, promoteToDefault(inferredType))
		return
	}

	if !isAssignable(inferredType, decl.Type) {
		tc.typeTable.Set(decl.Value, inferredType)
		tc.addError(decl.GetLocation(), SeverityError,
			"%s: cannot assign %s to %s", decl.Name, inferredType, decl.Type)
		return
	}

	// Store the annotation type — this is the effective type the expression is used as.
	// e.g. literal 42 annotated as i32 should be recorded as i32, not the untyped int.
	tc.typeTable.Set(decl.Value, decl.Type)
}

// inferExprType returns the type of expr, or nil if it cannot be determined yet.
func (tc *TypeChecker) inferExprType(expr ast.Expression) types.Type {
	// Check the side table first — a prior check may have already resolved this.
	if t, ok := tc.typeTable.Get(expr); ok {
		return t
	}

	switch e := expr.(type) {
	case *ast.IntegerLiteralExpr:
		return e.GetType()
	case *ast.FloatLiteralExpr:
		return e.GetType()
	case *ast.StringLiteralExpr:
		return e.GetType()
	case *ast.BooleanLiteralExpr:
		return e.GetType()
	case *ast.CharacterLiteralExpr:
		return e.GetType()
	case *ast.MathBinaryOpExpr:
		return tc.inferBinaryExpr(e)
	case *ast.IdentifierExpr:
		sym, ok := tc.scope.Lookup(e.Name)
		if !ok {
			return nil
		}
		if v, ok := sym.(*ast.VarDeclStmt); ok {
			if v.Value != nil {
				if t, ok := tc.typeTable.Get(v.Value); ok {
					return t
				}
			}
			return v.Type
		}
		return nil
	}
	return nil
}

// promoteToDefault converts an untyped literal type to its default concrete type.
// UntypedInt → int (natural register-width signed integer)
// UntypedFloat → f64
func promoteToDefault(t types.Type) types.Type {
	p, ok := t.(types.PrimitiveType)
	if !ok {
		return t
	}
	switch p.Name {
	case types.UntypedInt:
		return types.PrimitiveType{Name: types.Int}
	case types.UntypedFloat:
		return types.PrimitiveType{Name: types.Float64}
	}
	return t
}

func (tc *TypeChecker) inferBinaryExpr(expr *ast.MathBinaryOpExpr) types.Type {
	left := tc.inferExprType(expr.Left)
	right := tc.inferExprType(expr.Right)

	if left == nil || right == nil {
		return nil
	}

	if !types.IsNumeric(left) || !types.IsNumeric(right) {
		tc.addError(expr.GetLocation(), SeverityError,
			"operator %s: operands must be numeric, got %s and %s", expr.Operator, left, right)
		return nil
	}

	result := numericResultType(left, right)
	if result == nil {
		tc.addError(expr.GetLocation(), SeverityError,
			"operator %s: incompatible types %s and %s", expr.Operator, left, right)
		return nil
	}

	return result
}

func (tc *TypeChecker) addError(loc ast.Location, sev Severity, format string, args ...any) {
	tc.errors = append(tc.errors, TypeError{
		Location: loc,
		Severity: sev,
		Message:  fmt.Sprintf(format, args...),
	})
}
