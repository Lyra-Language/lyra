package ast

import (
	"fmt"

	"github.com/Lyra-Language/lyra/pkg/types"
)

type DestructuringDeclStmt struct {
	AstBase
	Keyword string
	Pattern Pattern
	Type    types.Type
	Value   Expression
}

func (d *DestructuringDeclStmt) statementNode() {}

func (d *DestructuringDeclStmt) GetName() string { return d.Pattern.GetName() }

type IfDestructuringStmt struct {
	AstBase
	DestructuringStatement DestructuringDeclStmt
	Then                   BlockExpr
	Else                   *BlockExpr // nil when the source omits `else { ... }`
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
	Else                   BlockExpr
}

func (e *ElseDestructuringStmt) statementNode() {}

func (e *ElseDestructuringStmt) GetName() string {
	return fmt.Sprintf("else %s { %s }", e.DestructuringStatement.GetName(), e.Else.GetName())
}
