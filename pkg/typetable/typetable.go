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
	// unresolvedCallees marks calls whose callee the typechecker refused; see
	// calleetable.go for why the later passes must be told rather than guess.
	unresolvedCallees map[*ast.FunctionCallExpr]bool
	// baseReadouts marks the calls the typechecker resolved as the builtin newtype
	// read-out `base(v)` — recognizable only here, since a user binding named `base`
	// shadows the builtin. See calleetable.go.
	baseReadouts map[*ast.FunctionCallExpr]bool
	// variadicPromotions records the type an argument in a C variadic call is actually
	// *passed* at, where C's default argument promotions widen it — an integer narrower
	// than `int` to `int`, a `float` to `double`.
	//
	// It is published rather than re-derived because the rule belongs to the boundary and
	// not to the backend: the typechecker is what knows the argument reached a `...`
	// position, and a second copy of the promotion table in codegen is a table that can
	// disagree with the one the diagnostics were written against. Only a *promoted*
	// argument is recorded, so a miss means "passed as it is".
	variadicPromotions map[ast.Expression]types.Type
}

func New() *TypeTable {
	return &TypeTable{entries: make(map[ast.Expression]types.Type)}
}

// SetVariadicPromotion records that arg is passed to a C variadic parameter at `promoted`
// rather than at its own type. See the field's note; only a widening is recorded.
func (t *TypeTable) SetVariadicPromotion(arg ast.Expression, promoted types.Type) {
	if t == nil {
		return
	}
	if t.variadicPromotions == nil {
		t.variadicPromotions = make(map[ast.Expression]types.Type)
	}
	t.variadicPromotions[arg] = promoted
}

// VariadicPromotion returns the type arg must be widened to before it is passed, and false
// when it is passed unchanged — which is every argument that is not in a `...` position, and
// most of those that are.
func (t *TypeTable) VariadicPromotion(arg ast.Expression) (types.Type, bool) {
	if t == nil {
		return nil, false
	}
	p, ok := t.variadicPromotions[arg]
	return p, ok
}

func (t *TypeTable) Set(expr ast.Expression, typ types.Type) {
	t.entries[expr] = typ
}

func (t *TypeTable) Get(expr ast.Expression) (types.Type, bool) {
	typ, ok := t.entries[expr]
	return typ, ok
}
