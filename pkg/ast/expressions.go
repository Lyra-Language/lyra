package ast

import (
	"fmt"
)

type Expression interface {
	AstNode
	exprNode()
	GetName() string
}

// Base struct to embed in all expression types
type ExprBase struct {
	AstBase
}

func (e *ExprBase) exprNode()       {}
func (e *ExprBase) GetName() string { return "" }

type IdentifierExpr struct {
	ExprBase
	Name    string
	IsConst bool
	// FromMethodCall marks the callee the typechecker builds from a method-form call
	// (`b.get(k)` → `get(b, k)`, desugarUFCSCall). It names a module-level function the
	// file can see — never a local binding — so a pass resolving names lexically must not
	// read it as one: a local closure also called `get` was taken for a capture of itself
	// (10/13, "captured binding "get" is not in scope").
	FromMethodCall bool
}

func (i *IdentifierExpr) GetName() string {
	return i.Name
}

type GuardExpr struct {
	ExprBase
	Condition Expression
}

func (g *GuardExpr) GetName() string {
	return fmt.Sprintf("guard %s", g.Condition.GetName())
}
