package typetable

import "github.com/Lyra-Language/lyra/pkg/ast"

// MethodTable maps a call-site expression — one whose Function is a
// MemberExpr or TraitMethodPathExpr that the type-checker resolved to a
// trait-impl method — to the specific *ast.TraitMethodImpl it dispatches to.
// It is populated by the type-checker during call resolution and consulted by
// later passes (e.g. the purity checker) that need to know which method body
// a given call actually invokes, without re-deriving dispatch themselves.
type MethodTable struct {
	entries map[*ast.FunctionCallExpr]*ast.TraitMethodImpl
}

func NewMethodTable() *MethodTable {
	return &MethodTable{entries: make(map[*ast.FunctionCallExpr]*ast.TraitMethodImpl)}
}

func (t *MethodTable) Set(call *ast.FunctionCallExpr, method *ast.TraitMethodImpl) {
	t.entries[call] = method
}

// Get is nil-receiver-safe (returns no match) so callers that don't have a
// MethodTable available — e.g. tests checking purity without running the
// typechecker first — can pass nil instead of needing a special case.
func (t *MethodTable) Get(call *ast.FunctionCallExpr) (*ast.TraitMethodImpl, bool) {
	if t == nil {
		return nil, false
	}
	method, ok := t.entries[call]
	return method, ok
}
