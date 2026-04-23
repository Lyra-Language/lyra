package ast

import (
	"fmt"

	"github.com/Lyra-Language/lyra/pkg/types"
)

type Statement interface {
	AstNode
	statementNode()
	GetName() string
}

// TypeDeclarationStmt represents a type declaration (struct, data type, etc.)
type TypeDeclStmt struct {
	AstBase
	Name          string
	GenericParams []string
	Type          types.Type
	IsPublic      bool
	Allocation    types.AllocationModifier
}

func (t *TypeDeclStmt) statementNode() {}

func (t *TypeDeclStmt) GetName() string { return t.Name }

// ExpressionStmt wraps an expression used as a statement
type ExpressionStmt struct {
	AstBase
	Expression Expression
}

func (e *ExpressionStmt) statementNode() {}

func (e *ExpressionStmt) GetName() string { return e.Expression.GetName() }

type BindingKind int

const (
	BindingUnknown BindingKind = iota
	BindingLet
	BindingVar
	BindingConst
)

func (k BindingKind) String() string {
	switch k {
	case BindingLet:
		return "let"
	case BindingVar:
		return "var"
	case BindingConst:
		return "const"
	default:
		return "unknown"
	}
}

// VariableDeclarationStmt represents a let/var/const binding
type VarDeclStmt struct {
	AstBase
	BindingKind BindingKind
	Name    string
	Type    types.Type // may be nil if needs inference
	Value   Expression
}

func (v *VarDeclStmt) statementNode() {}

func (v *VarDeclStmt) GetName() string { return v.Name }

// IsMutable returns true if this is a var declaration
func (v *VarDeclStmt) IsMutable() bool { return v.BindingKind == BindingVar }

// IsConstant returns true if this is a const declaration
func (v *VarDeclStmt) IsConstant() bool { return v.BindingKind == BindingConst }

// FunctionDefStmt represents a function definition
type FunctionDefStmt struct {
	AstBase
	Name          string
	GenericParams []string
	Signature     *types.FunctionType
	Clauses       []*FunctionClause
	IsPublic      bool
	IsPure        bool
	IsAsync       bool
}

func (f *FunctionDefStmt) statementNode() {}

func (f *FunctionDefStmt) GetName() string { return f.Name }

// FunctionClause represents a single clause of a function (pattern matching)
type FunctionClause struct {
	AstBase
	Parameters []Parameter
	Guard      *GuardExpr
	Body       Expression
}

type Parameter struct {
	Pattern      Pattern
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
