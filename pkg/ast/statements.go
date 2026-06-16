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
	NameLocation  Location `print:"-"` // span of just the type name (Location covers the whole decl)
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
	BindingKind BindingKind
	// IsMut records the `mut` modifier on a `let mut x` binding: the name may
	// not be reassigned, but the value's interior may be mutated. It is only
	// meaningful for BindingLet; a `var` is always interior-mutable and a
	// `const` never is.
	IsMut         bool
	Name          string
	NameLocation  Location `print:"-"` // span of just the bound name (Location covers the whole decl)
	GenericParams []GenericParam
	Type          types.Type // may be nil if needs inference
	Value         Expression
}

func (v *VarDeclStmt) statementNode() {}

func (v *VarDeclStmt) GetName() string { return v.Name }

// IsMutable returns true if the binding's *name* may be reassigned (a `var`).
func (v *VarDeclStmt) IsMutable() bool { return v.BindingKind == BindingVar }

// CanMutateInterior reports whether the value's interior (struct fields, array
// elements, …) may be mutated through this binding. True for a `var` and for a
// `let mut`; false for a plain `let` (deeply immutable) and a `const`.
func (v *VarDeclStmt) CanMutateInterior() bool {
	return v.BindingKind == BindingVar || (v.BindingKind == BindingLet && v.IsMut)
}

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

// LValueAssignmentStmt represents interior mutation of an aggregate through a
// member or index path: `p.x = v`, `arr[i] = v`, `grid[i].y = v`. Target is a
// *MemberExpr or *IndexExpr. The typechecker enforces that the root binding the
// path is rooted at permits interior mutation (a `var` or a `let mut`).
type LValueAssignmentStmt struct {
	AstBase
	Target Expression
	Value  Expression
}

func (l *LValueAssignmentStmt) statementNode()  {}
func (l *LValueAssignmentStmt) GetName() string { return "lvalue_assignment" }
