package typetable

import (
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// TypeTable maps AST expression nodes to their resolved types.
// It is populated by the type-checker and consulted by later compiler passes.
type TypeTable struct {
	entries map[ast.Expression]types.Type
	// callees records which declaration an overloaded call resolved to; see
	// calleetable.go for why only those are recorded.
	callees map[*ast.FunctionCallExpr]*ast.LambdaExpr
}

func New() *TypeTable {
	return &TypeTable{entries: make(map[ast.Expression]types.Type)}
}

func (t *TypeTable) Set(expr ast.Expression, typ types.Type) {
	t.entries[expr] = typ
}

func (t *TypeTable) Get(expr ast.Expression) (types.Type, bool) {
	typ, ok := t.entries[expr]
	return typ, ok
}
