package expressions

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collctx"
	"github.com/Lyra-Language/lyra/pkg/types"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

// collectGenericArgs collects type arguments from a generic_arguments node,
// delegating type parsing to ctx.ParseType.
func collectGenericArgs(node *sitter.Node, ctx *collctx.Ctx) []types.Type {
	args := make([]types.Type, 0)
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		if child.IsNamed() {
			args = append(args, ctx.ParseType(child))
		}
	}
	return args
}
