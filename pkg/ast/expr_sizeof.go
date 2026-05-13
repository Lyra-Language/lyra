package ast

import "github.com/Lyra-Language/lyra/pkg/types"

type SizeofExpr struct {
	ExprBase
	Type types.Type
}

func (s *SizeofExpr) GetName() string { return "sizeof_expr" }
