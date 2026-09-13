package ast

import (
	"fmt"

	"github.com/Lyra-Language/lyra/pkg/types"
)

type DestructuringDeclStmt struct {
	AstBase
	Keyword string
	IsMut   bool // `let mut (a, b) = ...`: interior of the bound values may be mutated
	Pattern Pattern
	Type    types.Type
	Value   Expression
	// Assigns is set only on the `let` a tuple assignment desugars into: the statements
	// that write its names out to the places, in order. The typechecker reads each one's
	// place type as the context for the matching element of Value, so `(a, b) = (4.0,
	// 5.0)` narrows to f32 places as `a = 4.0` does.
	//
	// Statements rather than their targets, because a rewrite replaces an expression slot
	// in place and a copied target would go stale. They are also the enclosing block's own
	// statements, so no walker descends through this field and none should — nor the
	// golden printer, which would print each assignment twice.
	Assigns []Statement `print:"-"`
}

func (d *DestructuringDeclStmt) statementNode() {}

func (d *DestructuringDeclStmt) GetName() string { return d.Pattern.GetName() }

type IfDestructuringStmt struct {
	AstBase
	DestructuringStatement DestructuringDeclStmt
	// Then and Else are pointers (not BlockExpr values) so the *ast.BlockExpr
	// identity the collector recorded in the ScopeTable survives into this
	// struct — a value field would copy the block, breaking the pointer-keyed
	// scope lookup the typechecker relies on (enterScope).
	Then *BlockExpr
	Else *BlockExpr // nil when the source omits `else { ... }`
}

func (i *IfDestructuringStmt) statementNode() {}

func (i *IfDestructuringStmt) GetName() string {
	if i.Else != nil {
		return fmt.Sprintf("if %s { %s } else { %s }", i.DestructuringStatement.GetName(), i.Then.GetName(), i.Else.GetName())
	}
	return fmt.Sprintf("if %s { %s }", i.DestructuringStatement.GetName(), i.Then.GetName())
}

type ElseDestructuringStmt struct {
	AstBase
	DestructuringStatement DestructuringDeclStmt
	// Else is a pointer for the same reason as IfDestructuringStmt.Then/Else —
	// see that comment.
	Else *BlockExpr
}

func (e *ElseDestructuringStmt) statementNode() {}

func (e *ElseDestructuringStmt) GetName() string {
	return fmt.Sprintf("else %s { %s }", e.DestructuringStatement.GetName(), e.Else.GetName())
}
