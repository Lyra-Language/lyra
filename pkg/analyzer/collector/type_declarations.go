package collector

import (
	"fmt"
	"strconv"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func (c *Collector) collectTypeDeclaration(node *sitter.Node) *ast.TypeDeclStmt {
	// type_declaration contains struct_type, data_type, trait_declaration, etc.
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		switch child.Kind() {
		case "struct_type":
			return c.collectStructType(child)
		case "data_type":
			return c.collectDataType(child)
		case "constrained_type":
			return c.collectConstrainedType(child)
		}
	}
	return nil
}

func (c *Collector) collectStructType(node *sitter.Node) *ast.TypeDeclStmt {
	var name string
	var genericParams []string
	fields := make(map[string]types.StructField)
	isPublic := false

	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		switch child.Kind() {
		case "visibility":
			isPublic = true
		case "struct_name":
			name = c.nodeText(child)
		case "generic_parameters":
			genericParams = c.collectGenericParams(child)
		case "struct_type_body":
			fields = c.collectStructFields(child)
		}
	}

	astNode := &ast.TypeDeclStmt{
		AstBase:       ast.AstBase{Location: c.nodeLocation(node)},
		Name:          name,
		GenericParams: genericParams,
		Type: types.StructType{
			Name:   name,
			Fields: fields,
		},
		IsPublic: isPublic,
	}

	if err := c.table.RegisterType(astNode); err != nil {
		c.errors = append(c.errors, err)
	}

	return astNode
}

func (c *Collector) collectDataType(node *sitter.Node) *ast.TypeDeclStmt {
	var name string
	var genericParams []string
	constructors := make(map[string]types.DataTypeConstructor)
	isPublic := false

	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		switch child.Kind() {
		case "visibility":
			isPublic = true
		case "data_type_name":
			name = c.nodeText(child)
		case "generic_parameters":
			genericParams = c.collectGenericParams(child)
		case "data_type_constructor":
			ctorName, ctor := c.collectDataConstructor(child)
			constructors[ctorName] = ctor
		}
	}

	astNode := &ast.TypeDeclStmt{
		AstBase:       ast.AstBase{Location: c.nodeLocation(node)},
		Name:          name,
		GenericParams: genericParams,
		Type: types.DataType{
			Name:         name,
			Constructors: constructors,
		},
		IsPublic: isPublic,
	}

	if err := c.table.RegisterType(astNode); err != nil {
		c.errors = append(c.errors, err)
	}

	return astNode
}

func (c *Collector) collectConstrainedType(node *sitter.Node) *ast.TypeDeclStmt {
	nameNode := node.ChildByFieldName("name")
	typeNode := node.ChildByFieldName("type")
	constraintsNode := node.ChildByFieldName("constraints")

	name := c.nodeText(nameNode)
	typeType := c.parseType(typeNode)

	astNode := &ast.TypeDeclStmt{
		AstBase: ast.AstBase{Location: c.nodeLocation(node)},
		Name:    name,
		Type: types.ConstrainedType{
			Name:        name,
			Type:        typeType,
			Constraints: c.collectConstraints(constraintsNode),
		},
	}

	if err := c.table.RegisterType(astNode); err != nil {
		c.errors = append(c.errors, err)
	}

	return astNode
}

func (c *Collector) collectConstraints(node *sitter.Node) []types.Constraint {
	constraints := make([]types.Constraint, 0)
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		switch child.Kind() {
		case "range_constraint":
			constraints = append(constraints, c.collectRangeConstraint(child))
		}
	}
	return constraints
}

func (c *Collector) collectRangeConstraint(node *sitter.Node) types.RangeConstraint {
	var start types.MathConstraintExpr
	var comparator string
	var end types.MathConstraintExpr
	startNode := node.ChildByFieldName("start")
	comparatorNode := node.ChildByFieldName("comparator")
	endNode := node.ChildByFieldName("end")

	if startNode != nil {
		start = c.collectMathConstraintExpr(startNode)
	}
	if comparatorNode != nil {
		comparator = c.nodeText(comparatorNode)
	}
	if endNode != nil {
		end = c.collectMathConstraintExpr(endNode)
	}

	if start == nil && end == nil {
		c.errors = append(c.errors, fmt.Errorf("range constraint must have a start or end"))
	}
	return types.RangeConstraint{
		Start:      start,
		Comparator: comparator,
		End:        end,
	}
}

func (c *Collector) collectMathConstraintExpr(node *sitter.Node) types.MathConstraintExpr {
	if node == nil {
		return nil
	}
	switch node.Kind() {
	case "constraint_math_expr":
		return c.collectMathConstraintExpr(node.Child(0))
	case "integer":
		value, _ := strconv.ParseInt(c.nodeText(node), 10, 64)
		return &types.MathConstraintLiteralExpr{
			Value: value,
			Type:  c.parseType(node.ChildByFieldName("type")),
		}
	case "float":
		value, _ := strconv.ParseFloat(c.nodeText(node), 64)
		return &types.MathConstraintLiteralExpr{
			Value: value,
			Type:  c.parseType(node.ChildByFieldName("type")),
		}
	case "identifier", "const_identifier":
		return &types.MathConstraintIdentifierExpr{
			Name:    c.nodeText(node),
			Type:    c.parseType(node.ChildByFieldName("type")),
			IsConst: node.Kind() == "const_identifier",
		}
	case "constraint_multiplication", "constraint_division", "constraint_addition", "constraint_subtraction":
		return c.collectMathConstraintBinaryOpExpr(node)
		// case "constraint_negation":
		// 	return c.collectMathConstraintNegation(node)
	}
	return nil
}

func (c *Collector) collectMathConstraintBinaryOpExpr(node *sitter.Node) types.MathConstraintExpr {
	leftNode := node.ChildByFieldName("left")
	operatorNode := node.ChildByFieldName("operator")
	rightNode := node.ChildByFieldName("right")
	operator := types.MathConstraintBinaryOp(c.nodeText(operatorNode))
	left := c.collectMathConstraintExpr(leftNode)
	right := c.collectMathConstraintExpr(rightNode)
	if left == nil || right == nil {
		c.errors = append(c.errors, fmt.Errorf("math constraint binary operator must have a left and right operand"))
		return nil
	}
	binaryOperator := types.MathConstraintBinaryOp("")
	switch operator {
	case types.MathConstraintBinaryOpMul:
		binaryOperator = types.MathConstraintBinaryOpMul
	case types.MathConstraintBinaryOpDiv:
		binaryOperator = types.MathConstraintBinaryOpDiv
	case types.MathConstraintBinaryOpAdd:
		binaryOperator = types.MathConstraintBinaryOpAdd
	case types.MathConstraintBinaryOpSub:
		binaryOperator = types.MathConstraintBinaryOpSub
	}
	if binaryOperator == "" {
		c.errors = append(c.errors, fmt.Errorf("invalid binary operator: %s", operator))
		return nil
	}
	return &types.MathConstraintBinaryOpExpr{
		Left:     left,
		Operator: operator,
		Right:    right,
	}
}
