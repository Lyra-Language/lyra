package expressions

import (
	"errors"
	"math/big"
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
		// A value that overflows int64 but fits u64 is a valid large-unsigned
		// literal: store its bit pattern and mark it Unsigned so the typechecker
		// infers it as u64 (its only valid type).
		if uv, uerr := strconv.ParseUint(valueStringToParse, int(base), 64); uerr == nil {
			return &ast.IntegerLiteralExpr{
				ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
				Value:    int64(uv),
				Base:     base,
				Unsigned: true,
			}
		}
		// Beyond u64 but within 128 bits: a **wide** literal, whose only valid types
		// are `i128`/`u128`. Stored as a big.Int because neither 64-bit field can
		// hold it; every consumer that must be right about the magnitude reads
		// BigValue, and Int64 answers ok=false so one that cannot be does not
		// silently read 0.
		if errors.Is(err, strconv.ErrRange) {
			if wide, ok := new(big.Int).SetString(valueStringToParse, int(base)); ok &&
				wide.Sign() >= 0 && wide.Cmp(ast.Uint128Max) <= 0 {
				return &ast.IntegerLiteralExpr{
					ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
					Base:     base,
					Wide:     wide,
				}
			}
		}
		// Beyond 128 bits (or otherwise unparseable): emit a clear diagnostic and fall
		// back to a placeholder literal (Value 0), never nil — a nil child would
		// enter the AST as a typed-nil expression and crash a later pass (e.g.
		// propagateExpectedType) that dereferences it. The error keeps the program
		// from compiling, so the placeholder value is inert.
		if errors.Is(err, strconv.ErrRange) {
			ctx.AddError(node, diag.SeverityError,
				"integer literal %s is too large to represent (exceeds the 128-bit range)", valueStringWithoutUnderscores)
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
