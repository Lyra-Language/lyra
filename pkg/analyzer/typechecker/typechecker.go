package typechecker

import (
	"fmt"
	"reflect"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/ast/symbols"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/regex"
	"github.com/Lyra-Language/lyra/pkg/types"
	"github.com/Lyra-Language/lyra/pkg/typetable"
)

type TypeChecker struct {
	symTable      *symbols.SymbolTable
	scopeTable    *symbols.ScopeTable
	typeTable     *typetable.TypeTable
	scope         *symbols.Scope
	errors        []TypeError
	paramTypes    map[string]types.Type // non-nil only while checking a function body
	resolvedTypes map[string]types.Type // cache for resolveType to avoid duplicate "unknown type" errors
}

func New(symTable *symbols.SymbolTable, scopeTable *symbols.ScopeTable, typeTable *typetable.TypeTable) *TypeChecker {
	return &TypeChecker{
		symTable:      symTable,
		scopeTable:    scopeTable,
		typeTable:     typeTable,
		scope:         symTable.GlobalScope,
		resolvedTypes: make(map[string]types.Type),
	}
}

// enterScope temporarily sets tc.scope to the scope recorded for node,
// then restores the previous scope. If node has no recorded scope (e.g. it
// was collected before ScopeTable was introduced) the call is a no-op.
func (tc *TypeChecker) enterScope(node ast.AstNode, fn func()) {
	scope, ok := tc.scopeTable.Get(node)
	if !ok {
		fn()
		return
	}
	old := tc.scope
	tc.scope = scope
	fn()
	tc.scope = old
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
	case *types.ConstrainedType:
		tc.checkConstrainedTypeDecl(decl)
	}
}

// checkConstrainedTypeDecl validates the constraints on a constrained-type
// declaration. Currently this means compiling every PatternConstraint regex
// at type-declaration time so users see syntax errors immediately.
func (tc *TypeChecker) checkConstrainedTypeDecl(decl *ast.TypeDeclStmt) {
	ct := decl.Type.(*types.ConstrainedType)
	for _, c := range ct.Constraints {
		pc, ok := c.(*types.PatternConstraint)
		if !ok {
			continue
		}
		body := regexPatternBody(pc.Pattern)
		if _, err := regex.Compile(body); err != nil {
			tc.addError(decl.GetLocation(), SeverityError,
				"type %s: invalid pattern constraint %s: %s",
				ct.Name, pc.Pattern, err)
		}
	}
}

// regexPatternBody strips the r/…/ delimiters from a PatternConstraint.Pattern
// value.  The grammar stores the full regex-literal text (e.g. r/[0-9]+/);
// regex.Compile expects just the inner body ([0-9]+).
func regexPatternBody(p string) string {
	if len(p) >= 3 && p[:2] == "r/" && p[len(p)-1] == '/' {
		return p[2 : len(p)-1]
	}
	return p // already stripped or bare pattern string
}

func (tc *TypeChecker) checkStructDecl(decl *ast.TypeDeclStmt) {

}

func (tc *TypeChecker) checkExpressionStmt(n *ast.ExpressionStmt) {
	switch e := n.Expression.(type) {
	case *ast.MathAssignOpExpr:
		tc.checkMathAssignOp(e)
	case *ast.BooleanLiteralExpr:
		tc.checkBooleanLiteralExpr(e)
	case *ast.BooleanBinaryOpExpr:
		tc.checkBooleanBinaryOpExpr(e)
	case *ast.NotBooleanExpr:
		tc.checkNotBooleanExpr(e)
	case *ast.StringConcatExpr:
		tc.inferStringConcatExpr(e)
	case *ast.FunctionCallExpr:
		tc.inferFunctionCallExpr(e)
	case *ast.IfExpr:
		tc.checkIfExpr(e)
	case *ast.MatchExpr:
		tc.checkMatchExpr(e)
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

	// Resolve user-defined type names (e.g. UnresolvedType{"Hex"} → *ConstrainedType)
	// so that assignability and constraint checks operate on the concrete type.
	resolvedDeclType := tc.resolveType(decl.Type, decl.Location)

	if !isAssignable(inferredType, resolvedDeclType) {
		tc.typeTable.Set(decl.Value, inferredType)
		tc.addError(decl.GetLocation(), SeverityError,
			"%s: cannot assign %s to %s", decl.Name, inferredType, decl.Type)
		return
	}

	// Check that the literal value fits within the annotated integer type's range.
	tc.checkIntegerLiteralRange(decl.Name, decl.Value, resolvedDeclType)

	// Validate string literals against any pattern constraints on the declared type.
	tc.checkPatternConstraints(decl.Name, decl.Value, resolvedDeclType)

	// Store the annotation type — this is the effective type the expression is used as.
	// e.g. literal 42 annotated as i32 should be recorded as i32, not the untyped int.
	tc.typeTable.Set(decl.Value, resolvedDeclType)
}

// checkPatternConstraints tests a string-literal value against every
// PatternConstraint on the declared type.  Non-string values and non-pattern
// constraints are silently skipped — this is purely an extra check layered on
// top of the ordinary type-assignability check.
func (tc *TypeChecker) checkPatternConstraints(name string, value ast.Expression, declType types.Type) {
	ct, ok := declType.(*types.ConstrainedType)
	if !ok {
		return
	}
	strLit, ok := value.(*ast.StringLiteralExpr)
	if !ok {
		return // only checkable at compile time for string literals
	}
	for _, c := range ct.Constraints {
		pc, ok := c.(*types.PatternConstraint)
		if !ok {
			continue
		}
		re, err := regex.Compile(regexPatternBody(pc.Pattern))
		if err != nil {
			// The broken regex is already reported at the type declaration site;
			// don't double-report here.
			continue
		}
		matched, err := re.MatchString(strLit.Value)
		if err != nil {
			continue // DFA capacity exceeded — don't block the user
		}
		if !matched {
			tc.addError(value.GetLocation(), SeverityError,
				"%s: value %q does not satisfy pattern constraint r/%s/",
				name, strLit.Value, pc.Pattern)
		}
	}
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
		return
	}
	// Check that the literal value fits within the variable's integer type's range.
	tc.checkIntegerLiteralRange(stmt.Name, stmt.Value, effective)
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
// present (resolved through the symbol table), or the TypeTable entry recorded
// when the initializer was checked.
func (tc *TypeChecker) effectiveType(decl *ast.VarDeclStmt) types.Type {
	if decl.Type != nil {
		return tc.resolveType(decl.Type, decl.Location)
	}
	if decl.Value != nil {
		if t, ok := tc.typeTable.Get(decl.Value); ok {
			return t
		}
	}
	return nil
}

// resolveType looks up an UnresolvedType name in the symbol table and returns
// the concrete declared type (e.g. *ConstrainedType, NamedStructType, DataType).
// All other type values are returned unchanged.
//
// Results are cached so that repeated resolutions of the same name only emit
// "unknown type" once per Check run.
func (tc *TypeChecker) resolveType(t types.Type, loc ast.Location) types.Type {
	ut, ok := t.(types.UnresolvedType)
	if !ok {
		return t
	}
	if cached, ok := tc.resolvedTypes[ut.Name]; ok {
		return cached
	}
	decl, ok := tc.symTable.Types[ut.Name]
	if !ok {
		tc.addError(loc, SeverityError, "unknown type %q", t)
		tc.resolvedTypes[ut.Name] = t // cache unresolved itself so the error fires only once
		return t
	}
	tc.resolvedTypes[ut.Name] = decl.Type
	return decl.Type
}

// inferExprType returns the type of expr, or nil if it cannot be determined yet.
func (tc *TypeChecker) inferExprType(expr ast.Expression) types.Type {
	if expr == nil {
		return nil
	}
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
	case *ast.ArrayLiteralExpr:
		return tc.inferArrayLiteralType(e)
	case *ast.FunctionCallExpr:
		if t := tc.inferTypeConversion(e); t != nil {
			return t
		}
		return tc.inferFunctionCallExpr(e)
	case *ast.LambdaExpr:
		return tc.inferLambdaExprType(e)
	case *ast.MemberExpr:
		return tc.inferMemberExprType(e)
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
	case *ast.MatchExpr:
		return tc.checkMatchExpr(e)
	case *ast.MathBinaryOpExpr:
		return tc.inferMathBinaryExpr(e)
	case *ast.StringConcatExpr:
		return tc.inferStringConcatExpr(e)
	case *ast.RegexLiteralExpr:
		// Validate regex syntax at compile time; the type of a regex literal
		// is the built-in `regex` type.
		if _, err := regex.Compile(e.Pattern); err != nil {
			tc.addError(e.GetLocation(), SeverityError,
				"invalid regex literal r/%s/: %s", e.Pattern, err)
		}
		return types.PrimitiveType{Name: types.Regex}
	case *ast.InterpolatedStringExpr:
		return types.PrimitiveType{Name: types.String}
	case *ast.DataConstructorExpr:
		// Resolve the data type that owns this constructor so that the type of
		// a data-constructor expression (e.g. `Some 42`) is the enclosing
		// DataType (e.g. `Maybe`), not nil.
		if dt, ok := tc.findDataTypeByConstructor(e.Constructor); ok {
			return dt
		}
		return nil
	case *ast.TupleLiteralExpr:
		return tc.inferTupleLiteralExpr(e)
	case *ast.IndexExpr:
		return tc.inferIndexExpr(e)
	case *ast.IdentifierExpr:
		// Consult the parameter scope installed by withParamScope while
		// type-checking a function body.
		if tc.paramTypes != nil {
			if t, ok := tc.paramTypes[e.Name]; ok {
				tc.typeTable.Set(e, t)
				return t
			}
		}
		sym, ok := tc.scope.Lookup(e.Name)
		if !ok {
			tc.addError(e.GetLocation(), SeverityError, "undefined identifier %q", e.Name)
			return nil
		}
		if v, ok := sym.(*ast.VarDeclStmt); ok {
			var t types.Type
			if v.Value != nil {
				if cached, ok := tc.typeTable.Get(v.Value); ok {
					t = cached
				}
			}
			if t == nil {
				t = v.Type
			}
			if t != nil {
				tc.typeTable.Set(e, t)
			}
			return t
		}
		tc.addError(e.GetLocation(), SeverityError, "undefined symbol %q", e.Name)
		return nil
	}
	tc.addError(expr.GetLocation(), SeverityError, "unknown expression type %q", expr.GetName())
	return nil
}

// promoteToDefault converts an untyped literal type to its default concrete type:
//   - UntypedInt / UntypedSignedInt → int
//   - UntypedFloat                 → f64
//   - StaticArrayType              → promote element type recursively
//
// All other types are returned unchanged.
func promoteToDefault(t types.Type) types.Type {
	switch v := t.(type) {
	case types.PrimitiveType:
		switch v.Name {
		case types.UntypedInt, types.UntypedSignedInt:
			return types.PrimitiveType{Name: types.Int}
		case types.UntypedFloat:
			return types.PrimitiveType{Name: types.Float64}
		}
	case types.StaticArrayType:
		// Promote the element type so that e.g. [1, 2, 3] (UntypedInt elements)
		// becomes StaticArrayType{int, 3} when there is no annotation.
		v.ElementType = promoteToDefault(v.ElementType)
		return v
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
		// Return targetType so the caller knows this is a type-conversion expression
		// and doesn't fall through to inferFunctionCallExpr (which would emit a
		// spurious "undefined function" error for the type name).
		return targetType
	}
	if isFloatType(argType) && isIntType(targetType) {
		tc.addError(call.GetLocation(), SeverityError,
			"cannot convert %s to %s: use floor(), ceil(), or round() to convert explicitly", argType, ident.Name)
		return targetType
	}
	if srcPrec, dstPrec := floatPrecision(argType), floatPrecision(targetType); srcPrec > dstPrec && dstPrec > 0 {
		tc.addError(call.GetLocation(), SeverityError,
			"cannot convert %s to %s: narrowing conversion may lose precision", argType, ident.Name)
		return targetType
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

// inferArrayLiteralType infers the type of an array literal expression.
// An array literal always produces a StaticArrayType — its length is known at
// compile time from the number of elements. The element type is the common type
// of all elements (via branchCommonType). When the elements are empty the
// element type is nil, which signals an unresolved/empty array.
//
// Whether the containing variable is static or dynamic is determined by the
// annotation type on the VarDeclStmt, not by the literal itself. isAssignable
// allows a StaticArrayType to widen into a DynamicArrayType so that:
//
//	let xs: []int = [1, 2, 3]   // OK — StaticArrayType{int,3} → DynamicArrayType{int}
//	let xs: [3]int = [1, 2, 3]  // OK — exact match
func (tc *TypeChecker) inferArrayLiteralType(expr *ast.ArrayLiteralExpr) types.Type {
	var elemType types.Type
	for _, el := range expr.Elements {
		t := tc.inferExprType(el) // keep untyped (UntypedInt, etc.) so the annotation can widen
		if t == nil {
			continue
		}
		if elemType == nil {
			elemType = t
			continue
		}
		common, ok := branchCommonType(elemType, t)
		if !ok {
			tc.addError(el.GetLocation(), SeverityError,
				"array literal: element type %s is not compatible with preceding element type %s",
				t, elemType)
			return nil
		}
		elemType = common
	}
	return types.StaticArrayType{ElementType: elemType, Size: len(expr.Elements)}
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

func (tc *TypeChecker) inferTupleLiteralExpr(expr *ast.TupleLiteralExpr) types.Type {
	elements := make([]types.Type, len(expr.Elements))
	for i, elem := range expr.Elements {
		t := tc.inferExprType(elem)
		if t == nil {
			return nil
		}
		elements[i] = promoteToDefault(t)
	}
	name := expr.Name
	if name == "" {
		name = "?"
	}
	return types.TupleType{Name: name, Elements: elements}
}

// resolveConstantInt returns the compile-time integer value of expr, if
// determinable. It looks through let-bound identifiers whose initializer is
// itself a constant integer expression.
func (tc *TypeChecker) resolveConstantInt(expr ast.Expression) (int64, bool) {
	switch e := expr.(type) {
	case *ast.IntegerLiteralExpr:
		return e.Value, true
	case *ast.IdentifierExpr:
		sym, ok := tc.scope.Lookup(e.Name)
		if !ok {
			return 0, false
		}
		v, ok := sym.(*ast.VarDeclStmt)
		if !ok || v.Value == nil {
			return 0, false
		}
		return tc.resolveConstantInt(v.Value)
	}
	return 0, false
}

func (tc *TypeChecker) inferIndexExpr(expr *ast.IndexExpr) types.Type {
	objectType := tc.inferExprType(expr.Object)
	indexType := tc.inferExprType(expr.Index)

	if objectType == nil {
		return nil
	}

	if indexType != nil && !isIntType(indexType) {
		tc.addError(expr.GetLocation(), SeverityError,
			"index must be an integer, got %s", indexType)
		return nil
	}

	switch t := objectType.(type) {
	case types.StaticArrayType:
		if idx, ok := tc.resolveConstantInt(expr.Index); ok {
			if idx < 0 || int(idx) >= t.Size {
				tc.addError(expr.GetLocation(), SeverityError,
					"index %d out of range for array of size %d", idx, t.Size)
				return nil
			}
		}
		return t.ElementType
	case types.DynamicArrayType:
		return t.ElementType
	case types.PrimitiveType:
		if t.Name == types.String {
			return types.PrimitiveType{Name: types.Char}
		}
		tc.addError(expr.GetLocation(), SeverityError,
			"cannot index into type %s", objectType)
		return nil
	case types.TupleType:
		idxVal, ok := tc.resolveConstantInt(expr.Index)
		if !ok {
			tc.addError(expr.GetLocation(), SeverityError,
				"tuple index must be an integer literal")
			return nil
		}
		idx := int(idxVal)
		if idx < 0 || idx >= len(t.Elements) {
			tc.addError(expr.GetLocation(), SeverityError,
				"tuple index %d out of range for tuple with %d elements", idx, len(t.Elements))
			return nil
		}
		return t.Elements[idx]
	default:
		tc.addError(expr.GetLocation(), SeverityError,
			"cannot index into type %s", objectType)
		return nil
	}
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

// inferLambdaExprType returns a LambdaType for a bare lambda expression,
// recording it in the type table so subsequent uses of the same AST node
// are handled via the cache (first line of inferExprType).
func (tc *TypeChecker) inferLambdaExprType(lambda *ast.LambdaExpr) types.Type {
	t := &types.LambdaType{
		ReturnType: types.ReturnType{Type: lambda.ReturnType.Type},
	}
	for _, p := range lambda.Parameters {
		t.Parameters = append(t.Parameters, types.ParameterType{
			Type:         tc.resolveType(p.Type, p.GetLocation()),
			DefaultValue: p.DefaultValue,
		})
	}
	tc.typeTable.Set(lambda, t)
	return t
}

// inferMemberExprType resolves member access (e.g. obj.field, obj.method())
// on struct types. It checks that the object is a struct, the field exists,
// and returns the field's type.
func (tc *TypeChecker) inferMemberExprType(m *ast.MemberExpr) types.Type {
	objType := tc.inferExprType(m.Object)
	fieldName := m.Property.Name

	switch t := objType.(type) {
	case types.NamedStructType:
		for _, f := range t.Fields {
			if f.Name == fieldName {
				tc.typeTable.Set(m, f.Type)
				return f.Type
			}
		}
		tc.addError(m.GetLocation(), SeverityError,
			"%s has no field %q", t.Name, fieldName)
	case types.AnonymousStructType:
		for _, f := range t.Fields {
			if f.Name == fieldName {
				tc.typeTable.Set(m, f.Type)
				return f.Type
			}
		}
		tc.addError(m.GetLocation(), SeverityError,
			"anonymous struct has no field %q", fieldName)
	default:
		// When the object type is nil (e.g. undefined identifier), don't
		// report a second error here — the undefined-identifier diagnostic
		// already explains the problem. inferMemberCall handles call-specific
		// errors for call sites where the object resolves but the field isn't callable.
		if objType != nil {
			tc.addError(m.GetLocation(), SeverityError,
				"member access on non-struct type %s", objType)
		}
	}
	return nil
}

func (tc *TypeChecker) addError(loc ast.Location, sev Severity, format string, args ...any) {
	tc.errors = append(tc.errors, TypeError{
		Location: loc,
		Severity: sev,
		Code:     diag.CodeTypeError,
		Message:  fmt.Sprintf(format, args...),
	})
}

func (tc *TypeChecker) addErrorRelated(loc ast.Location, sev Severity, related []diag.RelatedInformation, format string, args ...any) {
	tc.errors = append(tc.errors, TypeError{
		Location:           loc,
		Severity:           sev,
		Code:               diag.CodeTypeError,
		Message:            fmt.Sprintf(format, args...),
		RelatedInformation: related,
	})
}
