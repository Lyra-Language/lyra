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
	// IsExtern marks the body-less function an `extern` declaration *is*
	// (ExternDeclStmt.Func). Two passes must not read it as an ordinary lambda that
	// happens to be empty: the purity fixpoint would find no body, charge no effect and
	// call a foreign function pure, and the backend would emit a definition with no
	// blocks. Effects come from the declared bound instead, and the backend declares
	// rather than defines.
	IsExtern bool
	// IsVariadic marks the body-less function an `extern printf: (^u8, ...) -> i32` is.
	// Only an extern can carry it — Lyra has no variadic functions, and the collector
	// refuses the marker anywhere else (lyra-E065) — so it travels beside IsExtern rather
	// than being a general property of a lambda.
	//
	// It is lifted off the signature into the lambda for the same reason the effect bounds
	// are: every consumer that matters has the lambda and not the declaration, and the
	// call path is the one that needs it (an "at least N arguments" arity rule, and C's
	// default argument promotions on the rest).
	IsVariadic  bool
	IsUnsafe    bool
	IsPure      bool
	IsDet       bool
	IsNoAlloc   bool
	IsAsync     bool
	IsGenerator bool
	// GenericBounds are the `where` bounds of the declaration this lambda is the
	// value of, keyed by type-parameter name — lifted here by the collector exactly
	// as the leading modifiers are, because the bounds are written on the *binding*
	// (`let describe<t> where t: Show = …`) while every consumer has only the lambda.
	// The typechecker reads them twice: to put a bound in scope for the body (so a
	// call on a value of type `t` dispatches through it) and to check each solved
	// type argument at the instantiation.
	GenericBounds map[string][]string `print:"-"`
	// GenericParams are the declaration's type parameters **in declaration order**,
	// lifted off the binding for the same reason GenericBounds are. The order is the
	// content: a turbofish binds positionally (`empty::<i64>()`), so the list is what
	// pairs an explicit argument with the parameter it names. Deriving that order from
	// the signature instead would be wrong wherever a parameter is declared in one
	// order and first mentioned in another — `let f<u, t> = (a: t, b: u) -> …` — and
	// wrong silently, binding each argument to the other's parameter.
	GenericParams []GenericParam `print:"-"`
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
