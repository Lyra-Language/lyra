package ast

import (
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
	GenericParams []GenericParam
	Type          types.Type
	IsPublic      bool
	Allocation    types.AllocationModifier
	Derives       []string
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
	BindingKind   BindingKind
	Name          string
	GenericParams []GenericParam
	Type          types.Type // may be nil if needs inference
	Value   Expression
}

func (v *VarDeclStmt) statementNode() {}

func (v *VarDeclStmt) GetName() string { return v.Name }

// IsMutable returns true if this is a var declaration
func (v *VarDeclStmt) IsMutable() bool { return v.BindingKind == BindingVar }

// IsConstant returns true if this is a const declaration
func (v *VarDeclStmt) IsConstant() bool { return v.BindingKind == BindingConst }

// VarReassignmentStmt represents a mutable variable update: x = value
type VarReassignmentStmt struct {
	AstBase
	Name  string
	Value Expression
}

func (v *VarReassignmentStmt) statementNode()  {}
func (v *VarReassignmentStmt) GetName() string { return v.Name }

// DerefAssignmentStmt represents a pointer write: *target = value
type DerefAssignmentStmt struct {
	AstBase
	Target DerefExpr
	Value  Expression
}

func (d *DerefAssignmentStmt) statementNode()  {}
func (d *DerefAssignmentStmt) GetName() string { return "deref_assignment" }