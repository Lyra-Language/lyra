package collector

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func (c *Collector) collectIntegerLiteralExpr(node *sitter.Node) *ast.IntegerLiteralExpr {
	loc := c.nodeLocation(node)
	base := ast.IntegerBase10
	value := int64(0)
	err := error(nil)

	for i := uint(0); i < node.ChildCount(); i++ {
		valueString := c.nodeText(node.Child(i))
		valueStringWithoutUnderscores := strings.ReplaceAll(valueString, "_", "")
		value, err = strconv.ParseInt(valueStringWithoutUnderscores, 0, 64)
		if err != nil {
			c.errors = append(c.errors, fmt.Errorf("failed to parse integer literal: %w", err))
			return nil
		}
		switch node.Child(i).Kind() {
		case "binary_int":
			base = ast.IntegerBase2
		case "octal_int":
			base = ast.IntegerBase8
		case "decimal_int":
			base = ast.IntegerBase10
		case "hexadecimal_int":
			base = ast.IntegerBase16
		}
	}
	return &ast.IntegerLiteralExpr{
		ExprBase: ast.ExprBase{
			AstBase: ast.AstBase{Location: loc},
			Type:    nil, // Type will be resolved during type checking
		},
		Value: value,
		Base:  base,
	}
}

func (c *Collector) collectFloatLiteralExpr(node *sitter.Node) *ast.FloatLiteralExpr {
	loc := c.nodeLocation(node)
	valueString := c.nodeText(node)
	valueStringWithoutUnderscores := strings.ReplaceAll(valueString, "_", "")
	value, err := strconv.ParseFloat(valueStringWithoutUnderscores, 64)
	if err != nil {
		c.errors = append(c.errors, fmt.Errorf("failed to parse float literal: %w", err))
		return nil
	}
	return &ast.FloatLiteralExpr{
		ExprBase: ast.ExprBase{
			AstBase: ast.AstBase{Location: loc},
			Type:    types.PrimitiveType{Name: "float"},
		},
		Value: value,
	}
}

func (c *Collector) collectStringLiteralExpr(node *sitter.Node, loc ast.Location) *ast.StringLiteralExpr {
	value, err := strconv.Unquote(c.nodeText(node))
	if err != nil {
		c.errors = append(c.errors, fmt.Errorf("invalid string literal: %v", err))
		return nil
	}
	return &ast.StringLiteralExpr{
		ExprBase: ast.ExprBase{
			AstBase: ast.AstBase{Location: loc},
			Type:    types.PrimitiveType{Name: types.String},
		},
		Value: value,
	}
}

func (c *Collector) collectBooleanLiteralExpr(node *sitter.Node, loc ast.Location) *ast.BooleanLiteralExpr {
	value := c.nodeText(node) == "true"
	return &ast.BooleanLiteralExpr{
		ExprBase: ast.ExprBase{
			AstBase: ast.AstBase{Location: loc},
			Type:    types.PrimitiveType{Name: types.Bool},
		},
		Value: value,
	}
}
