package collector

import (
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func (c *Collector) collectTypeDeclaration(node *sitter.Node) *ast.TypeDeclarationStmt {
	// type_declaration contains struct_type, data_type, trait_declaration, etc.
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		switch child.Kind() {
		case "struct_type":
			return c.collectStructType(child)
		case "data_type":
			return c.collectDataType(child)
		}
	}
	return nil
}

func (c *Collector) collectStructType(node *sitter.Node) *ast.TypeDeclarationStmt {
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

	astNode := &ast.TypeDeclarationStmt{
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

func (c *Collector) collectDataType(node *sitter.Node) *ast.TypeDeclarationStmt {
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

	astNode := &ast.TypeDeclarationStmt{
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
