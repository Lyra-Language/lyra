package expressions

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func collectStringLiteralExpr(node *sitter.Node, ctx *collector_ctx.Ctx, loc ast.Location) ast.Expression {
	// A string_literal spans `"…"` and has named children that are either
	// `string_content` (literal text chunks) or `string_interpolation` (`${expr}`).
	// We reconstruct the literal text from the *raw source between* the
	// interpolations rather than from the `string_content` node text: tree-sitter,
	// with `/\s/` in `extras`, strips a content chunk's leading whitespace as token
	// padding, so a `string_content` node that begins with a space (a plain `"  x"`,
	// or the text right after a `${…}`) loses it. Slicing the source directly — the
	// interpolation nodes' byte ranges are exact and whitespace-safe (they start at
	// `$`) — recovers every byte. When there are no interpolations we return a plain
	// StringLiteralExpr; otherwise an InterpolatedStringExpr whose segments
	// alternate literal chunks and the embedded expressions.
	innerStart := node.StartByte() + 1 // just past the opening quote
	innerEnd := node.EndByte() - 1     // just before the closing quote

	// On any error the whole literal becomes an empty string, never nil (hazard 3): the
	// escape is validated here, after a successful parse, so this path is live on any
	// program — and a nil call argument, array element or struct field value crashes
	// the typechecker. The diagnostic keeps the program from compiling.
	placeholder := func() ast.Expression {
		return &ast.StringLiteralExpr{ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: loc}}}
	}

	newLiteralChunk := func(start, end uint) (*ast.StringLiteralExpr, bool) {
		content, err := ast.UnescapeString(string(ctx.Source[start:end]))
		if err != nil {
			ctx.AddError(node, diag.SeverityError, "invalid string literal: %v", err)
			return nil, false
		}
		return &ast.StringLiteralExpr{
			ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
			Value:    content,
		}, true
	}

	// Gather the interpolation children in source order.
	var interps []*sitter.Node
	for i := range node.NamedChildCount() {
		if child := node.NamedChild(i); child.Kind() == "string_interpolation" {
			// **An interpolation that runs past its line is the unescaped `${`**, not an
			// expression somebody wrapped: `${` has no escape in an ordinary string, so a
			// program meaning the two characters literally opens one, and it swallows
			// source to the next `}` — declarations included — while still parsing. The
			// author then sees an undefined name far below and nothing near the string.
			// Refused here, where the span is known, rather than left to the cascade.
			if child.StartPosition().Row != child.EndPosition().Row {
				ctx.AddErrorCoded(child, diag.SeverityError, diag.CodeInterpolationSpansLines,
					"this `${` opens an interpolation that runs past the end of its line, "+
						"swallowing the source up to the next `}`. An interpolated expression is "+
						"written where it is read, so this is almost always a literal `${` — there "+
						"is no escape for it, so write the string raw: `${`")
				return placeholder()
			}
			interps = append(interps, child)
		}
	}

	if len(interps) == 0 {
		lit, ok := newLiteralChunk(innerStart, innerEnd)
		if !ok {
			return placeholder()
		}
		return lit
	}

	segments := []ast.Expression{}
	cursor := innerStart
	for _, interp := range interps {
		// The literal chunk is the raw source from the cursor up to this `${`.
		if start := interp.StartByte(); start > cursor {
			lit, ok := newLiteralChunk(cursor, start)
			if !ok {
				return placeholder()
			}
			segments = append(segments, lit)
		}
		exprNode := interp.NamedChild(0)
		if exprNode == nil {
			ctx.AddError(interp, diag.SeverityError, "empty string interpolation")
			return placeholder()
		}
		expr := CollectExpression(exprNode, ctx)
		if expr == nil {
			return placeholder()
		}
		segments = append(segments, expr)
		cursor = interp.EndByte() // resume just past the closing `}`
	}
	// Trailing literal chunk after the last interpolation.
	if innerEnd > cursor {
		lit, ok := newLiteralChunk(cursor, innerEnd)
		if !ok {
			return placeholder()
		}
		segments = append(segments, lit)
	}

	return &ast.InterpolatedStringExpr{
		ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
		Segments: segments,
	}
}
