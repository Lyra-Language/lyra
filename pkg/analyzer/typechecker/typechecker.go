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
	case *ast.VarReassignmentStmt:
		tc.checkVarReassignment(n)
	case *ast.ExpressionStmt:
		if e, ok := n.Expression.(*ast.MathAssignOpExpr); ok {
			tc.checkMathAssignOp(e)
		}
	case *ast.DerefAssignmentStmt:
		tc.checkDerefAssignment(n)
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

func (tc *TypeChecker) checkVarReassignment(stmt *ast.VarReassignmentStmt) {
	sym, ok := tc.scope.Lookup(stmt.Name)
	if !ok {
		return
	}
	decl, ok := sym.(*ast.VarDeclStmt)
	if !ok {
		return
	}
	if !decl.IsMutable() {
		tc.addError(stmt.GetLocation(), SeverityError,
			"%s: cannot assign to immutable binding", stmt.Name)
		return
	}
	effective := tc.effectiveType(decl)
	if effective == nil {
		return
	}
	rhsType := tc.inferExprType(stmt.Value)
	if rhsType == nil {
		return
	}
	if !isAssignable(rhsType, effective) {
		tc.addError(stmt.GetLocation(), SeverityError,
			"%s: cannot assign %s to %s", stmt.Name, rhsType, effective)
	}
}

// checkDerefAssignment handles the grammar's representation of const reassignment.
// When the parser sees `X = val` where X is a const identifier, it emits a
// DerefAssignmentStmt with a DerefExpr wrapping the const IdentifierExpr.
func (tc *TypeChecker) checkDerefAssignment(stmt *ast.DerefAssignmentStmt) {
	ident, ok := stmt.Target.Operand.(*ast.IdentifierExpr)
	if !ok || !ident.IsConst {
		return
	}
	tc.addError(stmt.GetLocation(), SeverityError,
		"%s: cannot assign to immutable binding", ident.Name)
}

func (tc *TypeChecker) checkMathAssignOp(expr *ast.MathAssignOpExpr) {
	sym, ok := tc.scope.Lookup(expr.Left.Name)
	if !ok {
		return
	}
	decl, ok := sym.(*ast.VarDeclStmt)
	if !ok {
		return
	}
	if !decl.IsMutable() {
		tc.addError(expr.GetLocation(), SeverityError,
			"%s: cannot assign to immutable binding", expr.Left.Name)
		return
	}
	effective := tc.effectiveType(decl)
	if effective == nil {
		return
	}
	rhsType := tc.inferExprType(expr.Right)
	if rhsType == nil {
		return
	}
	if !isAssignable(rhsType, effective) {
		tc.addError(expr.GetLocation(), SeverityError,
			"%s: cannot assign %s to %s", expr.Left.Name, rhsType, effective)
	}
}

// effectiveType returns the concrete type of a declaration: the annotation if
// present, or the TypeTable entry recorded when the initializer was checked.
func (tc *TypeChecker) effectiveType(decl *ast.VarDeclStmt) types.Type {
	if decl.Type != nil {
		return decl.Type
	}
	if decl.Value != nil {
		if t, ok := tc.typeTable.Get(decl.Value); ok {
			return t
		}
	}
	return nil
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
	case *ast.FunctionCallExpr:
		return tc.inferTypeConversion(e)
	case *ast.NegationExpr:
		return tc.inferNegationExpr(e)
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
	case types.UntypedInt, types.UntypedSignedInt:
		return types.PrimitiveType{Name: types.Int}
	case types.UntypedFloat:
		return types.PrimitiveType{Name: types.Float64}
	}
	return t
}

// inferTypeConversion handles calls of the form `TypeName(expr)` where TypeName
// is a concrete numeric primitive. Returns nil for ordinary function calls.
func (tc *TypeChecker) inferTypeConversion(call *ast.FunctionCallExpr) types.Type {
	ident, ok := call.Function.(*ast.IdentifierExpr)
	if !ok {
		return nil
	}
	targetType, ok := numericPrimitiveByName(ident.Name)
	if !ok {
		return nil
	}
	if len(call.Arguments) != 1 {
		tc.addError(call.GetLocation(), SeverityError,
			"%s: type conversion requires exactly 1 argument, got %d", ident.Name, len(call.Arguments))
		return targetType
	}
	argType := tc.inferExprType(call.Arguments[0])
	if argType == nil {
		return targetType
	}
	if !types.IsNumeric(argType) {
		tc.addError(call.GetLocation(), SeverityError,
			"cannot convert %s to %s", argType, ident.Name)
		return nil
	}
	if isFloatType(argType) && isIntType(targetType) {
		tc.addError(call.GetLocation(), SeverityError,
			"cannot convert %s to %s: use a rounding function", argType, ident.Name)
		return nil
	}
	if srcPrec, dstPrec := floatPrecision(argType), floatPrecision(targetType); srcPrec > dstPrec && dstPrec > 0 {
		tc.addError(call.GetLocation(), SeverityError,
			"cannot convert %s to %s: use a rounding function", argType, ident.Name)
		return nil
	}
	return targetType
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

func (tc *TypeChecker) inferNegationExpr(expr *ast.NegationExpr) types.Type {
	operandType := tc.inferExprType(expr.Operand)
	if operandType == nil {
		return nil
	}
	if !types.IsNumeric(operandType) {
		tc.addError(expr.GetLocation(), SeverityError,
			"cannot negate non-numeric type %s", operandType)
		return nil
	}
	p, ok := operandType.(types.PrimitiveType)
	if ok && isAnyConcreteUnsignedInt(p.Name) {
		tc.addError(expr.GetLocation(), SeverityError,
			"cannot negate unsigned type %s", operandType)
		return nil
	}
	if ok && (p.Name == types.UntypedInt || p.Name == types.UntypedSignedInt) {
		return types.PrimitiveType{Name: types.UntypedSignedInt}
	}
	return operandType
}

func (tc *TypeChecker) addError(loc ast.Location, sev Severity, format string, args ...any) {
	tc.errors = append(tc.errors, TypeError{
		Location: loc,
		Severity: sev,
		Message:  fmt.Sprintf(format, args...),
	})
}
