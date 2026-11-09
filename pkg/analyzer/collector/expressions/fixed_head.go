package expressions

import (
	"fmt"

	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/cst"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

// collectFixedHead collects the `fixed<8, 8>` of `fixed<8, 8>(x)` and `fixed<8, 8>.from_bits(n)`
// into a name, its format's canonical spelling, as the `u8` of `u8(x)` is the name `u8`: the
// conversion path finds its target by that name. A format that can't exist has been reported
// by ParseType, and keeps its name, so the conversion it heads is checked as any other is
// rather than being reported again as something else.
func collectFixedHead(node *sitter.Node, ctx *collector_ctx.Ctx, loc ast.Location) ast.Expression {
	ctx.ParseType(node)
	name := fmt.Sprintf("fixed<%s,%s>", ctx.NodeText(cst.Field(node, "integer_bits")), ctx.NodeText(cst.Field(node, "fractional_bits")))
	return &ast.IdentifierExpr{ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: loc}}, Name: name}
}
