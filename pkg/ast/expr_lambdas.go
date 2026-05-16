package ast

import (
	"fmt"

	"github.com/Lyra-Language/lyra/pkg/types"
)

type LambdaExpr struct {
	ExprBase
	Parameters    []Parameter
	ReturnType    types.ReturnType
	Body          Expression
	LambdaClauses []LambdaClause
	IsUnsafe      bool
	IsPure        bool
	IsAsync       bool
	IsGenerator   bool
}

func (e *LambdaExpr) exprNode() {}

func (e *LambdaExpr) GetName() string {
	return "lambda"
}

// LambdaClause represents a single clause of a function (pattern matching)
type LambdaClause struct {
	AstBase
	Patterns []Pattern
	Guard    *GuardExpr
	Body     Expression
}

type Parameter struct {
	AstBase
	Pattern      Pattern
	TypeModifier types.TypeModifier
	Type         types.Type
	DefaultValue Expression
}

func (p *Parameter) GetName() string {
	defaultValue := ""
	if p.DefaultValue != nil {
		defaultValue = fmt.Sprintf(" = %v", p.DefaultValue)
	}
	return fmt.Sprintf("%s%s", p.Pattern.GetName(), defaultValue)
}

// ReturnStmt represents a return statement
type ReturnStmt struct {
	AstBase
	Value Expression // nil for bare return
}

func (r *ReturnStmt) statementNode() {}

func (r *ReturnStmt) GetName() string {
	return fmt.Sprintf("return %s", r.Value.GetName())
}
