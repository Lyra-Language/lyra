package ast

import (
	"fmt"

	"github.com/Lyra-Language/lyra/pkg/types"
)

type FunctionCallExpr struct {
	ExprBase
	Function         Expression
	GenericArguments []types.Type
	Arguments        []Expression
}

func (f *FunctionCallExpr) GetName() string {
	return "function_call_expr"
}

type MemberExpr struct {
	ExprBase
	Object   Expression
	Property IdentifierExpr
	Optional bool
}

func (m *MemberExpr) GetName() string {
	return fmt.Sprintf("%s.%s", m.Object.GetName(), m.Property.GetName())
}

type IndexExpr struct {
	ExprBase
	Object   Expression
	Index    Expression
	Optional bool
}

func (i *IndexExpr) GetName() string {
	return fmt.Sprintf("%s[%s]", i.Object.GetName(), i.Index.GetName())
}

// TupleIndexExpr is positional tuple element access (`pair.0`, `pair.1`) — the
// numeric-index counterpart to MemberExpr's named field access. Index is the
// parsed, zero-based element position.
type TupleIndexExpr struct {
	ExprBase
	Object Expression
	Index  int
}

func (t *TupleIndexExpr) GetName() string {
	return fmt.Sprintf("%s.%d", t.Object.GetName(), t.Index)
}

type TryExpr struct {
	ExprBase
	Operand Expression
}

func (t *TryExpr) GetName() string {
	return fmt.Sprintf("%s?", t.Operand.GetName())
}

// TraitMethodPathExpr is the fully-qualified `TraitName::method` call form —
// disambiguates which trait's implementation to call when a plain
// `obj.method(args)` would be ambiguous (two traits implementing a
// same-named method for the same type). It always appears as the Function
// of a FunctionCallExpr; the receiver is then an ordinary first argument
// (`Show::show(n)`), not an implicit object the way MemberExpr's is.
type TraitMethodPathExpr struct {
	ExprBase
	TraitName string
	Method    IdentifierExpr
}

func (t *TraitMethodPathExpr) GetName() string {
	return fmt.Sprintf("%s::%s", t.TraitName, t.Method.GetName())
}
