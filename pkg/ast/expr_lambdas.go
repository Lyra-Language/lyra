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
	IsDet         bool
	IsNoAlloc     bool
	IsAsync       bool
	IsGenerator   bool
	// GenericBounds are the `where` bounds of the declaration this lambda is the
	// value of, keyed by type-parameter name — lifted here by the collector exactly
	// as the leading modifiers are, because the bounds are written on the *binding*
	// (`let describe<t> where t: Show = …`) while every consumer has only the lambda.
	// The typechecker reads them twice: to put a bound in scope for the body (so a
	// call on a value of type `t` dispatches through it) and to check each solved
	// type argument at the instantiation.
	GenericBounds map[string][]string `print:"-"`
	// ReturnTypeInferred records that ReturnType.Type was filled in from the body
	// (inferLambdaReturnType) rather than written by the author. Everything that
	// consumes a signature wants the filled-in type and should ignore this; the one
	// consumer that needs to tell them apart is the entry point, where an *absent*
	// annotation is a documented spelling of `void` and must not become "returns
	// whatever the last expression happened to be".
	ReturnTypeInferred bool
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
	if r.Value == nil {
		return "return"
	}
	return fmt.Sprintf("return %s", r.Value.GetName())
}
