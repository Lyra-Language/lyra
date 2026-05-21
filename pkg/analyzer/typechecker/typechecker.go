package typechecker

import (
	"fmt"
	"reflect"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/ast/symbols"
	"github.com/Lyra-Language/lyra/pkg/types"
	"github.com/Lyra-Language/lyra/pkg/typetable"
)

type TypeChecker struct {
	symTable   *symbols.SymbolTable
	typeTable  *typetable.TypeTable
	scope      *symbols.Scope
	errors     []TypeError
	paramTypes map[string]types.Type // non-nil only while checking a function body
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
	// Guard against both untyped nils and typed nils (e.g. (*ast.ExpressionStmt)(nil)
	// stored in an ast.AstNode interface — a common Go gotcha when sub-functions return
	// a concrete nil pointer that gets wrapped in the interface).
	if node == nil {
		return
	}
	if rv := reflect.ValueOf(node); rv.Kind() == reflect.Ptr && rv.IsNil() {
		return
	}
	switch n := node.(type) {
	case *ast.TypeDeclStmt:
		tc.checkTypeDecl(n)
	case *ast.VarDeclStmt:
		tc.checkVarDecl(n)
	case *ast.VarReassignmentStmt:
		tc.checkVarReassignment(n)
	case *ast.ExpressionStmt:
		tc.checkExpressionStmt(n)
	case *ast.DerefAssignmentStmt:
		tc.checkDerefAssignment(n)
	case *ast.BooleanBinaryOpExpr:
		tc.checkBooleanBinaryOpExpr(n)
	}
}

func (tc *TypeChecker) checkTypeDecl(decl *ast.TypeDeclStmt) {
	switch decl.Type.(type) {
	case types.NamedStructType:
		tc.checkStructDecl(decl)
	}
}

func (tc *TypeChecker) checkStructDecl(decl *ast.TypeDeclStmt) {

}

func (tc *TypeChecker) checkExpressionStmt(n *ast.ExpressionStmt) {
	if e, ok := n.Expression.(*ast.MathAssignOpExpr); ok {
		tc.checkMathAssignOp(e)
	} else if e, ok := n.Expression.(*ast.BooleanLiteralExpr); ok {
		tc.checkBooleanLiteralExpr(e)
	} else if e, ok := n.Expression.(*ast.BooleanBinaryOpExpr); ok {
		tc.checkBooleanBinaryOpExpr(e)
	} else if e, ok := n.Expression.(*ast.NotBooleanExpr); ok {
		tc.checkNotBooleanExpr(e)
	} else if e, ok := n.Expression.(*ast.StringConcatExpr); ok {
		tc.inferStringConcatExpr(e)
	} else if e, ok := n.Expression.(*ast.FunctionCallExpr); ok {
		tc.inferFunctionCallExpr(e)
	} else if e, ok := n.Expression.(*ast.IfExpr); ok {
		tc.checkIfExpr(e)
	}
}

func (tc *TypeChecker) checkVarDecl(decl *ast.VarDeclStmt) {
	if decl.Value == nil {
		return
	}

	// Lambda values (function declarations) are handled separately.
	// Full lambda type inference is not yet implemented, so the regular
	// annotation check is skipped for them.
	if lambda, ok := decl.Value.(*ast.LambdaExpr); ok {
		tc.checkLambdaBody(decl.Name, lambda)
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
		tc.addImmutableBindingError(stmt.GetLocation(), stmt.Name, decl.BindingKind)
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
	tc.addImmutableBindingError(stmt.Target.Operand.GetLocation(), ident.Name, ast.BindingConst)
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
		tc.addImmutableBindingError(expr.GetLocation(), expr.Left.Name, decl.BindingKind)
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

func (tc *TypeChecker) checkBooleanLiteralExpr(expr *ast.BooleanLiteralExpr) {
	exprType := tc.inferExprType(expr)
	if exprType == nil {
		return
	}
	if !types.IsBoolean(exprType) {
		tc.addExpectedTypeError(expr, types.PrimitiveType{Name: types.Boolean}, exprType)
	}
}

func (tc *TypeChecker) checkNotBooleanExpr(expr *ast.NotBooleanExpr) {
	exprType := tc.inferExprType(expr.Expression)
	if exprType == nil {
		return
	}
	if !types.IsBoolean(exprType) {
		tc.addError(expr.GetLocation(), SeverityError,
			"'!' operator: operand must be boolean, got %s", exprType)
	}
}

func (tc *TypeChecker) checkBooleanBinaryOpExpr(expr *ast.BooleanBinaryOpExpr) {
	leftType := tc.inferExprType(expr.Left)
	rightType := tc.inferExprType(expr.Right)

	if leftType == nil || rightType == nil {
		return
	}

	switch expr.Operator {
	case ast.BooleanBinaryOpAnd, ast.BooleanBinaryOpOr:
		if !types.IsBoolean(leftType) || !types.IsBoolean(rightType) {
			tc.addError(expr.GetLocation(), SeverityError,
				"operator %s: operands must both be boolean, got %s and %s", expr.Operator, leftType, rightType)
		}
	case ast.BooleanBinaryOpEq, ast.BooleanBinaryOpNEq:
		if !areEqualityCompatible(leftType, rightType) {
			tc.addIncompatibleTypesError(expr, string(expr.Operator), leftType, rightType)
		}
	case ast.BooleanBinaryOpLT, ast.BooleanBinaryOpLTE, ast.BooleanBinaryOpGT, ast.BooleanBinaryOpGTE:
		if !types.IsNumeric(leftType) || !types.IsNumeric(rightType) {
			tc.addError(expr.GetLocation(), SeverityError,
				"operator %s: operands must be numeric, got %s and %s", expr.Operator, leftType, rightType)
			return
		}
		if numericResultType(leftType, rightType) == nil {
			tc.addIncompatibleTypesError(expr, string(expr.Operator), leftType, rightType)
		}
	}
}

func (tc *TypeChecker) addImmutableBindingError(loc ast.Location, name string, kind ast.BindingKind) {
	switch kind {
	case ast.BindingConst:
		tc.addError(loc, SeverityError,
			"%s: 'const' binding is immutable and cannot be reassigned", name)
	default: // BindingLet
		tc.addError(loc, SeverityError,
			"%s: 'let' binding is immutable; use 'var' to allow reassignment", name)
	}
}

func (tc *TypeChecker) addExpectedTypeError(expr ast.Expression, expected, actual types.Type) {
	tc.addError(expr.GetLocation(), SeverityError,
		"%s: expected %s, got %s instead", expr.GetName(), expected, actual)
}

func (tc *TypeChecker) addIncompatibleTypesError(expr ast.Expression, operator string, leftType, rightType types.Type) {
	tc.addError(expr.GetLocation(), SeverityError,
		"operator %s: incompatible types: %s and %s", operator, leftType, rightType)
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
		if t := tc.inferTypeConversion(e); t != nil {
			return t
		}
		return tc.inferFunctionCallExpr(e)
	case *ast.NegationExpr:
		return tc.inferNegationExpr(e)
	case *ast.StructInstanceExpr:
		return tc.inferStructInstanceExpr(e)
	case *ast.AnonymousStructInstanceExpr:
		return tc.inferAnonymousStructInstanceExpr(e)
	case *ast.NotBooleanExpr:
		tc.checkNotBooleanExpr(e)
		return types.PrimitiveType{Name: types.Boolean}
	case *ast.BooleanBinaryOpExpr:
		tc.checkBooleanBinaryOpExpr(e)
		return types.PrimitiveType{Name: types.Boolean}
	case *ast.BlockExpr:
		return tc.inferBlockType(e)
	case *ast.IfExpr:
		return tc.checkIfExpr(e)
	case *ast.MathBinaryOpExpr:
		return tc.inferMathBinaryExpr(e)
	case *ast.StringConcatExpr:
		return tc.inferStringConcatExpr(e)
	case *ast.InterpolatedStringExpr:
		return types.PrimitiveType{Name: types.String}
	case *ast.IdentifierExpr:
		// Consult the parameter scope installed by withParamScope while
		// type-checking a function body.
		if tc.paramTypes != nil {
			if t, ok := tc.paramTypes[e.Name]; ok {
				return t
			}
		}
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
			"cannot convert %s to %s: use floor(), ceil(), or round() to convert explicitly", argType, ident.Name)
		return nil
	}
	if srcPrec, dstPrec := floatPrecision(argType), floatPrecision(targetType); srcPrec > dstPrec && dstPrec > 0 {
		tc.addError(call.GetLocation(), SeverityError,
			"cannot convert %s to %s: narrowing conversion may lose precision", argType, ident.Name)
		return nil
	}
	return targetType
}

func (tc *TypeChecker) inferMathBinaryExpr(expr *ast.MathBinaryOpExpr) types.Type {
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
			"operator %s: incompatible types: %s and %s", expr.Operator, left, right)
		return nil
	}

	return result
}

func (tc *TypeChecker) inferStringConcatExpr(expr *ast.StringConcatExpr) types.Type {
	left := tc.inferExprType(expr.Left)
	right := tc.inferExprType(expr.Right)

	if left == nil || right == nil {
		return nil
	}

	if !types.IsString(left) || !types.IsString(right) {
		tc.addError(expr.GetLocation(), SeverityError,
			"operator ++: operands must be strings, got %s and %s", left, right)
		return nil
	}

	return types.PrimitiveType{Name: types.String}
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

func (tc *TypeChecker) inferStructInstanceExpr(expr *ast.StructInstanceExpr) types.Type {
	decl, ok := tc.symTable.Types[expr.Name]
	if !ok {
		tc.addError(expr.GetLocation(), SeverityError, "undefined struct type %q", expr.Name)
		return nil
	}

	structType, ok := decl.Type.(types.NamedStructType)

	if !ok {
		tc.addError(expr.GetLocation(), SeverityError, "%s: not a struct type", expr.Name)
		return nil
	} else {
		if len(structType.Fields) == 0 {
			tc.addError(expr.GetLocation(), SeverityError, "%s: no fields declared", expr.Name)
			return nil
		}
	}

	typeSubst := make(map[string]types.Type, len(decl.GenericParams))
	if len(decl.GenericParams) != len(expr.GenericArgs) {
		tc.addError(expr.GetLocation(), SeverityError, "%s: expected %d generic arguments, got %d", expr.Name, len(decl.GenericParams), len(expr.GenericArgs))
		return nil
	} else if len(decl.GenericParams) > 0 {
		for i, param := range decl.GenericParams {
			typeSubst[param.Name] = expr.GenericArgs[i]
		}
	}

	// Build a quick name->type lookup for the declared fields.
	fieldTypes := make(map[string]types.Type, len(structType.Fields))
	for _, f := range structType.Fields {
		// Substitute generic type parameters if available, otherwise use the field's declared type.
		if typeSub, ok := typeSubst[f.Type.GetName()]; ok {
			fieldTypes[f.Name] = typeSub
		} else {
			fieldTypes[f.Name] = f.Type
		}
	}

	// Check each field in the instance against the declared type and build a set of field names.
	fieldNames := make(map[string]struct{}, len(expr.Fields))
	for idx, f := range expr.Fields {
		name := f.Name
		if name == "" {
			name = structType.Fields[idx].Name
		}
		fieldNames[name] = struct{}{}
		expected, ok := fieldTypes[name]
		if !ok {
			tc.addError(expr.GetLocation(), SeverityError, "%s: unknown field %q", expr.Name, name)
			continue
		}
		actual := tc.inferExprType(f.Value)
		if actual != nil && !isAssignable(actual, expected) {
			tc.addError(f.Value.GetLocation(), SeverityError, "%s.%s: cannot assign %s to %s", expr.Name, name, actual, expected)
		}
	}

	if expr.BaseStruct != nil {
		// Record update syntax: the base struct supplies any fields not listed in
		// the update, so missing-field errors are suppressed for those.
		// We do verify that the base expression has the same struct type.
		baseType := tc.inferExprType(expr.BaseStruct)
		if baseType != nil && !types.TypesEqual(baseType, structType) {
			tc.addError(expr.BaseStruct.GetLocation(), SeverityError,
				"%s: base struct has type %s, expected %s", expr.Name, baseType, structType)
		}
	} else {
		// Full struct literal: every field without a default must be supplied.
		for _, f := range structType.Fields {
			if _, ok := fieldNames[f.Name]; !ok {
				if f.DefaultValue != nil {
					continue
				}
				tc.addError(expr.GetLocation(), SeverityError, "%s: missing field %q", expr.Name, f.Name)
			}
		}
	}

	return structType
}

func (tc *TypeChecker) inferAnonymousStructInstanceExpr(expr *ast.AnonymousStructInstanceExpr) types.Type {
	structTypeFields := tc.convertAnonymousStructFieldsToTypeFields(expr.Fields)
	structType := types.AnonymousStructType{
		Fields: structTypeFields,
	}

	return structType
}

func (tc *TypeChecker) convertAnonymousStructFieldsToTypeFields(fields []ast.StructField) []types.StructField {
	structTypeFields := make([]types.StructField, len(fields))
	for i, f := range fields {
		structTypeFields[i] = types.StructField{
			Name: f.Name,
			Type: tc.inferExprType(f.Value),
		}
	}
	return structTypeFields
}

func (tc *TypeChecker) addError(loc ast.Location, sev Severity, format string, args ...any) {
	tc.errors = append(tc.errors, TypeError{
		Location: loc,
		Severity: sev,
		Message:  fmt.Sprintf(format, args...),
	})
}
