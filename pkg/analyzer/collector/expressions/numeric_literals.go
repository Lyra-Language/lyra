package expressions

import (
	"errors"
	"strconv"
	"strings"

	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func collectIntegerLiteralExpr(node *sitter.Node, ctx *collector_ctx.Ctx, loc ast.Location) *ast.IntegerLiteralExpr {
	base := ast.IntegerBase10
	value := int64(0)
	var err error

	child := node.NamedChild(0)
	switch child.Kind() {
	case "binary_int":
		base = ast.IntegerBase2
	case "octal_int":
		base = ast.IntegerBase8
	case "decimal_int":
		base = ast.IntegerBase10
	case "hexadecimal_int":
		base = ast.IntegerBase16
	}

	valueString := ctx.NodeText(child)
	valueStringWithoutUnderscores := strings.ReplaceAll(valueString, "_", "")
	valueStringToParse := valueStringWithoutUnderscores
	switch base {
	case ast.IntegerBase2:
		valueStringToParse = strings.TrimPrefix(valueStringToParse, "0b")
	case ast.IntegerBase8:
		valueStringToParse = strings.TrimPrefix(valueStringToParse, "0o")
	case ast.IntegerBase16:
		valueStringToParse = strings.TrimPrefix(valueStringToParse, "0x")
	}
	value, err = strconv.ParseInt(valueStringToParse, int(base), 64)
	if err != nil {
		// Emit a clear diagnostic and fall back to a placeholder literal (Value 0),
		// never nil: a nil child would enter the AST as a typed-nil expression and
		// crash a later pass (e.g. propagateLiteralType) that dereferences it. The
		// error keeps the program from compiling, so the placeholder value is inert.
		if _, uerr := strconv.ParseUint(valueStringToParse, int(base), 64); uerr == nil {
			// A valid unsigned 64-bit value that overflows int64. IntegerLiteralExpr.Value
			// is an int64, so a large u64 literal isn't representable yet.
			ctx.AddError(node, diag.SeverityError,
				"integer literal %s exceeds the range of i64; unsigned literals above 9223372036854775807 are not yet supported",
				valueStringWithoutUnderscores)
		} else if errors.Is(err, strconv.ErrRange) {
			ctx.AddError(node, diag.SeverityError,
				"integer literal %s is too large to represent (exceeds the 64-bit range)", valueStringWithoutUnderscores)
		} else {
			ctx.AddError(node, diag.SeverityError, "invalid integer literal %s", valueStringWithoutUnderscores)
		}
		return &ast.IntegerLiteralExpr{
			ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
			Value:    0,
			Base:     base,
		}
	}

	return &ast.IntegerLiteralExpr{
		ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
		Value:    value,
		Base:     base,
	}
}

func collectFloatLiteralExpr(node *sitter.Node, ctx *collector_ctx.Ctx, loc ast.Location) *ast.FloatLiteralExpr {
	valueString := ctx.NodeText(node)
	valueStringWithoutUnderscores := strings.ReplaceAll(valueString, "_", "")
	value, err := strconv.ParseFloat(valueStringWithoutUnderscores, 64)
	if err != nil {
		// Placeholder (never nil), for the same typed-nil-crash reason as the
		// integer case above.
		if errors.Is(err, strconv.ErrRange) {
			ctx.AddError(node, diag.SeverityError,
				"float literal %s is out of range for f64", valueStringWithoutUnderscores)
		} else {
			ctx.AddError(node, diag.SeverityError, "invalid float literal %s", valueStringWithoutUnderscores)
		}
		return &ast.FloatLiteralExpr{
			ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
			Value:    0,
		}
	}
	return &ast.FloatLiteralExpr{
		ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
		Value:    value,
	}
}
