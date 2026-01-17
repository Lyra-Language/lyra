package checker

import (
	"fmt"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/ast/symbols"
	"github.com/Lyra-Language/lyra/pkg/types"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

type Checker struct {
	table  *symbols.SymbolTable
	scope  *symbols.Scope
	errors []TypeError
	ast    *ast.Program
}

type TypeError struct {
	Message  string
	Location ast.Location
	Expected types.Type
	Actual   types.Type
}

func (e TypeError) Error() string {
	return fmt.Sprintf("%d:%d: %s", e.Location.StartLine, e.Location.StartCol, e.Message)
}

func NewChecker(ast *ast.Program, table *symbols.SymbolTable) *Checker {
	return &Checker{
		ast:    ast,
		table:  table,
		scope:  table.GlobalScope,
		errors: make([]TypeError, 0),
	}
}

// Check runs type checking on the entire program
func (c *Checker) Check() []TypeError {
	c.checkProgram(c.ast)
	return c.errors
}

func (c *Checker) checkProgram(ast *program.Program) {
	for i := uint(0); i < program.ChildCount(); i++ {
		child := program.Child(i)
		switch child.Kind() {
		case "function_definition":
			c.checkFunctionDef(child)
		case "const_declaration":
			//c.checkConstDeclaration(child)
		case "declaration":
			c.checkDeclaration(child)
		case "var_reassignment":
			//c.checkVarReassignment(child)
		case "return_statement":
			//c.checkReturnStatement(child)
		case "type_declaration":
			// already collected, but could verify here
		}
	}
}

func (c *Checker) checkFunctionDef(node *sitter.Node) {
	// node is function_definition

	functionSignatureNode := node.ChildByFieldName("signature")
	funcName := c.nodeText(functionSignatureNode.ChildByFieldName("name"))

	// Create function scope
	funcScope := symbols.NewScope(c.scope, symbols.ScopeFunction)
	oldScope := c.scope
	c.scope = funcScope

	if funcSym, ok := c.table.Functions[funcName]; ok && funcSym.Signature != nil {
		// For each param in each function clause, create VariableSymbol and add to funcScope
		for _, clause := range funcSym.Clauses {
			for pattern_idx, pattern := range clause.ParameterPatterns {
				paramType := funcSym.Signature.ParameterTypes[pattern_idx]
				if paramType.Type == nil {
					c.typeError(node, nil, nil,
						"function %s has no parameter type for pattern %s",
						funcName, pattern.GetName())
					continue
				}
				switch p := pattern.(type) {
				case ast.IdentifierPattern:
					paramSym := &symbols.VariableSymbol{
						Name:     p.Name,
						Type:     paramType.Type,
						Location: c.nodeLocation(node),
					}
					funcScope.Define(paramSym)
				case ast.LiteralPattern:
					paramSym := &symbols.VariableSymbol{
						Name:     fmt.Sprintf("%v", p.Value),
						Type:     paramType.Type,
						Location: c.nodeLocation(node),
					}
					funcScope.Define(paramSym)
				}
			}
		}

		// check that return type of each function clause matches the function signature
		for _, clause := range funcSym.Clauses {
			if clause.Body != nil {

			}
		}
	}

	c.scope = oldScope
}

func (c *Checker) checkDeclaration(node *sitter.Node) {
	var varName string
	var declaredType types.Type
	var initExpr *sitter.Node

	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		switch child.Kind() {
		case "identifier":
			varName = c.nodeText(child)
		case "type_annotation":
			// Extract the type from annotation
			for j := uint(0); j < child.ChildCount(); j++ {
				if child.Child(j).IsNamed() {
					declaredType = c.parseTypeNode(child.Child(j))
				}
			}
		default:
			if child.IsNamed() && isExpression(child.Kind()) {
				initExpr = child
			}
		}
	}

	if initExpr != nil {
		exprType := c.CheckExpression(initExpr)
		if declaredType != nil && exprType != nil {
			if !types.TypesEqual(declaredType, exprType) {
				c.typeError(initExpr, declaredType, exprType,
					"cannot assign %s to variable %s of type %s",
					c.typeString(exprType), varName, c.typeString(declaredType))
			}
		}
		// If no declared type, infer from expression
		if declaredType == nil {
			declaredType = exprType
		}
	}

	// Add variable to current scope
	sym := &symbols.VariableSymbol{
		Name:     varName,
		Type:     declaredType,
		Location: c.nodeLocation(node),
	}
	c.scope.Define(sym)
}

func (c *Checker) checkBlock(node *sitter.Node) types.Type {
	var lastType types.Type
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.IsNamed() {
			lastType = c.CheckExpression(child)
		}
	}
	// In expression-oriented languages, block returns last expression's type
	return lastType
}

// CheckExpression returns the type of an expression
func (c *Checker) CheckExpression(node *sitter.Node) types.Type {
	switch node.Kind() {
	// Literals
	case "integer_literal":
		return types.PrimitiveType{Name: "Int"}
	case "float_literal":
		return types.PrimitiveType{Name: "Float"}
	case "string_literal", "raw_string_literal":
		return types.PrimitiveType{Name: "Str"}
	case "boolean_literal":
		return types.PrimitiveType{Name: "Bool"}
	case "char_literal":
		return types.PrimitiveType{Name: "Char"}

	// Identifiers
	case "identifier":
		return c.checkIdentifier(node)

	// Compound expressions
	case "call_expression":
		return c.checkFunctionCall(node)
	case "binary_expression":
		return c.checkBinaryExpr(node)
	case "unary_expression":
		return c.checkUnaryExpr(node)
	case "if_then_else":
		return c.checkIfThenElse(node)
	case "array_literal":
		return c.checkArrayLiteral(node)
	case "tuple_literal":
		return c.checkTupleLiteral(node)
	case "lambda":
		return c.checkLambda(node)
	case "member_expression":
		return c.checkMemberAccess(node)
	case "index_expression":
		return c.checkIndexExpr(node)
	case "block":
		return c.checkBlock(node)

	default:
		// Try to recurse into wrapper nodes
		for i := uint(0); i < node.ChildCount(); i++ {
			child := node.Child(i)
			if child.IsNamed() {
				return c.CheckExpression(child)
			}
		}
	}
	return nil
}

func (c *Checker) checkIdentifier(node *sitter.Node) types.Type {
	name := c.nodeText(node)

	// Check local scope first
	if sym, ok := c.scope.Lookup(name); ok {
		return c.typeOfSymbol(sym)
	}

	// Check quick lookup tables
	if fn, ok := c.table.Functions[name]; ok {
		return fn.Signature
	}
	if ty, ok := c.table.Types[name]; ok {
		return ty.Type
	}

	c.error(node, "undefined: %s", name)
	return nil
}

func (c *Checker) checkFunctionCall(node *sitter.Node) types.Type {
	var calleeNode *sitter.Node
	var argNodes []*sitter.Node

	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		switch child.Kind() {
		case "identifier":
			calleeNode = child
		case "argument_list":
			for j := uint(0); j < child.ChildCount(); j++ {
				arg := child.Child(j)
				if arg.IsNamed() {
					argNodes = append(argNodes, arg)
				}
			}
		}
	}

	if calleeNode == nil {
		c.error(node, "invalid function call")
		return nil
	}

	calleeType := c.CheckExpression(calleeNode)
	if calleeType == nil {
		return nil
	}

	fnType, ok := calleeType.(*types.FunctionType)
	if !ok {
		c.error(calleeNode, "cannot call non-function type %s", c.typeString(calleeType))
		return nil
	}

	// Check argument count
	if len(argNodes) != len(fnType.ParameterTypes) {
		c.error(node, "expected %d arguments but got %d", len(fnType.ParameterTypes), len(argNodes))
		return fnType.ReturnType
	}

	// Check each argument type
	for i, argNode := range argNodes {
		argType := c.CheckExpression(argNode)
		expectedType := fnType.ParameterTypes[i].Type
		if argType != nil && expectedType != nil {
			if !types.TypesEqual(expectedType, argType) {
				c.typeError(argNode, expectedType, argType,
					"argument %d: expected %s but got %s",
					i+1, c.typeString(expectedType), c.typeString(argType))
			}
		}
	}

	return fnType.ReturnType
}

func (c *Checker) checkBinaryExpr(node *sitter.Node) types.Type {
	var left, right *sitter.Node
	var operator string

	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if !child.IsNamed() {
			// Operator is typically an anonymous node
			operator = c.nodeText(child)
		} else if left == nil {
			left = child
		} else {
			right = child
		}
	}

	leftType := c.CheckExpression(left)
	rightType := c.CheckExpression(right)

	// Comparison operators always return Bool
	switch operator {
	case "==", "!=", ">", "<", ">=", "<=":
		if leftType != nil && rightType != nil && !types.TypesEqual(leftType, rightType) {
			c.typeError(node, leftType, rightType,
				"cannot compare %s with %s", c.typeString(leftType), c.typeString(rightType))
			return nil
		}
		return types.PrimitiveType{Name: "Bool"}

	case "&&", "||":
		boolType := types.PrimitiveType{Name: "Bool"}
		if leftType != nil && !types.TypesEqual(boolType, leftType) {
			c.typeError(left, boolType, leftType, "expected Bool for logical operator")
			return nil
		}
		if rightType != nil && !types.TypesEqual(boolType, rightType) {
			c.typeError(right, boolType, rightType, "expected Bool for logical operator")
			return nil
		}
		return boolType

	case "+", "-", "*", "/", "%", "**":
		if leftType != nil && rightType != nil {
			// check if leftType and rightType are primitives
			_, leftIsPrimitive := leftType.(types.PrimitiveType)
			_, rightIsPrimitive := rightType.(types.PrimitiveType)
			if !leftIsPrimitive || !rightIsPrimitive {
				c.typeError(node, leftType, rightType,
					"cannot perform arithmetic on %s and %s",
					c.typeString(leftType), c.typeString(rightType))
				return nil
			}
			// check if the primitive types are numeric
			if !leftType.(types.PrimitiveType).IsNumericType() || !rightType.(types.PrimitiveType).IsNumericType() {
				c.typeError(node, leftType, rightType,
					"cannot perform arithmetic on %s and %s",
					c.typeString(leftType), c.typeString(rightType))
				return nil
			}
			// check if the primitive types are the same
			if leftType.(types.PrimitiveType).Name != rightType.(types.PrimitiveType).Name {
				c.typeError(node, leftType, rightType,
					"cannot perform arithmetic on %s and %s",
					c.typeString(leftType), c.typeString(rightType))
				return nil
			}
			if !types.TypesEqual(leftType, rightType) {
				c.typeError(node, leftType, rightType,
					"mismatched types in arithmetic: %s and %s",
					c.typeString(leftType), c.typeString(rightType))
				return nil
			}
		}
		return leftType

	case "<=>": // spaceship operator
		return types.PrimitiveType{Name: "Int"}

	case "++": // array concatenation
		// check if leftType and rightType are arrays
		_, leftIsArray := leftType.(types.ArrayType)
		_, rightIsArray := rightType.(types.ArrayType)
		if !leftIsArray || !rightIsArray {
			c.typeError(node, types.ArrayType{}, leftType,
				"cannot concatenate %s and %s", c.typeString(leftType), c.typeString(rightType))
			return nil
		}

		// Check if the elements of the arrays are the same type
		leftElementType := leftType.(types.ArrayType).ElementType
		rightElementType := rightType.(types.ArrayType).ElementType
		if !types.TypesEqual(leftElementType, rightElementType) {
			c.typeError(node, leftElementType, rightElementType,
				"cannot concatenate arrays with different element types: %s and %s",
				c.typeString(leftElementType), c.typeString(rightElementType))
			return nil
		}

		return leftType
	}
	c.error(node, "unknown binary operator: %s", operator)
	return nil
}

func (c *Checker) checkUnaryExpr(node *sitter.Node) types.Type {
	var operator string
	var operand *sitter.Node

	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if !child.IsNamed() {
			operator = c.nodeText(child)
		} else {
			operand = child
		}
	}

	operandType := c.CheckExpression(operand)

	switch operator {
	case "!":
		boolType := types.PrimitiveType{Name: "Bool"}
		if operandType != nil && !types.TypesEqual(boolType, operandType) {
			c.typeError(operand, boolType, operandType, "! requires Bool operand")
		}
		return boolType
	case "-":
		return operandType // negation preserves numeric type
	}

	return operandType
}

func (c *Checker) checkIfThenElse(node *sitter.Node) types.Type {
	var condNode, thenNode, elseNode *sitter.Node

	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if !child.IsNamed() {
			continue
		}
		if condNode == nil {
			condNode = child
		} else if thenNode == nil {
			thenNode = child
		} else {
			elseNode = child
		}
	}

	// Condition must be Bool
	condType := c.CheckExpression(condNode)
	boolType := types.PrimitiveType{Name: "Bool"}
	if condType != nil && !types.TypesEqual(boolType, condType) {
		c.typeError(condNode, boolType, condType, "if condition must be Bool")
	}

	thenType := c.CheckExpression(thenNode)

	if elseNode != nil {
		elseType := c.CheckExpression(elseNode)
		// Both branches must have same type
		if thenType != nil && elseType != nil && !types.TypesEqual(thenType, elseType) {
			c.typeError(elseNode, thenType, elseType,
				"if branches have different types: %s vs %s",
				c.typeString(thenType), c.typeString(elseType))
		}
		return thenType
	}

	// No else branch - expression returns Unit/Void
	return types.PrimitiveType{Name: "Unit"}
}

func (c *Checker) checkArrayLiteral(node *sitter.Node) types.Type {
	var elemType types.Type

	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if !child.IsNamed() {
			continue
		}
		t := c.CheckExpression(child)
		if elemType == nil {
			elemType = t
		} else if t != nil && !types.TypesEqual(elemType, t) {
			c.typeError(child, elemType, t, "array elements must have same type")
		}
	}

	if elemType == nil {
		return types.ArrayType{} // empty array, type unknown
	}
	return types.ArrayType{ElementType: elemType}
}

func (c *Checker) checkTupleLiteral(node *sitter.Node) types.Type {
	var elements []types.Type
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.IsNamed() {
			elements = append(elements, c.CheckExpression(child))
		}
	}
	return types.TupleType{Elements: elements}
}

func (c *Checker) checkLambda(node *sitter.Node) types.Type {
	// TODO: Extract parameter types and body, check body in new scope
	return &types.FunctionType{}
}

func (c *Checker) checkMemberAccess(node *sitter.Node) types.Type {
	var objNode *sitter.Node
	var memberName string

	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.Kind() == "identifier" && objNode != nil {
			memberName = c.nodeText(child)
		} else if child.IsNamed() {
			objNode = child
		}
	}

	objType := c.CheckExpression(objNode)
	if objType == nil {
		return nil
	}

	switch t := objType.(type) {
	case types.StructType:
		if fieldType, ok := t.Fields[memberName]; ok {
			return fieldType
		}
		c.error(node, "struct %s has no field %s", t.Name, memberName)
	case types.TupleType:
		// TODO: Handle tuple.0, tuple.1 access
	}

	return nil
}

func (c *Checker) checkIndexExpr(node *sitter.Node) types.Type {
	var objNode, indexNode *sitter.Node

	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.IsNamed() {
			if objNode == nil {
				objNode = child
			} else {
				indexNode = child
			}
		}
	}

	objType := c.CheckExpression(objNode)
	indexType := c.CheckExpression(indexNode)

	switch t := objType.(type) {
	case types.ArrayType:
		// Index should be Int
		intType := types.PrimitiveType{Name: "Int"}
		if indexType != nil && !types.TypesEqual(intType, indexType) {
			c.typeError(indexNode, intType, indexType, "array index must be Int")
		}
		return t.ElementType
	case types.MapType:
		if indexType != nil && !types.TypesEqual(t.KeyType, indexType) {
			c.typeError(indexNode, t.KeyType, indexType, "map key type mismatch")
		}
		return t.ValueType
	}

	c.error(objNode, "cannot index type %s", c.typeString(objType))
	return nil
}

// Helper methods
func (c *Checker) nodeLocation(node *sitter.Node) ast.Location {
	start := node.StartPosition()
	end := node.EndPosition()
	return ast.Location{
		StartLine: int(start.Row) + 1,
		StartCol:  int(start.Column) + 1,
		EndLine:   int(end.Row) + 1,
		EndCol:    int(end.Column) + 1,
	}
}

func (c *Checker) typeOfSymbol(sym symbols.Symbol) types.Type {
	switch s := sym.(type) {
	case *symbols.FunctionSymbol:
		return s.Signature
	case *symbols.VariableSymbol:
		return s.Type
	case *symbols.TypeSymbol:
		return s.Type
	}
	return nil
}

func (c *Checker) error(node *sitter.Node, format string, args ...interface{}) {
	c.errors = append(c.errors, TypeError{
		Message:  fmt.Sprintf(format, args...),
		Location: c.nodeLocation(node),
	})
}

func (c *Checker) typeError(node *sitter.Node, expected, actual types.Type, format string, args ...interface{}) {
	c.errors = append(c.errors, TypeError{
		Message:  fmt.Sprintf(format, args...),
		Location: c.nodeLocation(node),
		Expected: expected,
		Actual:   actual,
	})
}

func (c *Checker) typeString(t types.Type) string {
	if t == nil {
		return "<unknown>"
	}
	switch ty := t.(type) {
	case types.PrimitiveType:
		return ty.GetName()
	case types.ArrayType:
		return "[]" + c.typeString(ty.ElementType)
	case types.MapType:
		return "{" + c.typeString(ty.KeyType) + ": " + c.typeString(ty.ValueType) + "}"
	case types.TupleType:
		s := "("
		for i, el := range ty.Elements {
			if i > 0 {
				s += ", "
			}
			s += c.typeString(el)
		}
		return s + ")"
	case types.StructType:
		return ty.Name
	case types.DataType:
		return ty.Name
	case *types.FunctionType:
		s := "("
		for i, p := range ty.ParameterTypes {
			if i > 0 {
				s += ", "
			}
			s += c.typeString(p.Type)
		}
		return s + ") -> " + c.typeString(ty.ReturnType)
	case types.GenericType:
		return ty.Name
	}
	return fmt.Sprintf("%T", t)
}

func isExpression(kind string) bool {
	switch kind {
	case "integer_literal", "float_literal", "string_literal", "boolean_literal",
		"identifier", "call_expression", "binary_expression", "unary_expression",
		"if_then_else", "array_literal", "tuple_literal", "lambda", "index_expression", "block":
		return true
	}
	return false
}
