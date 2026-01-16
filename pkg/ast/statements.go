package ast

import "github.com/Lyra-Language/lyra/pkg/types"

// TypeDeclarationStmt represents a type declaration (struct, data type, etc.)
type TypeDeclarationStmt struct {
	AstBase
	Name          string
	GenericParams []string
	Type          types.Type
	IsPublic      bool
}

func (t *TypeDeclarationStmt) GetName() string { return t.Name }

// ExpressionStmt wraps an expression used as a statement
type ExpressionStmt struct {
	AstBase
	Expression Expression
}

// VariableDeclarationStmt represents a let/var/const binding
type VariableDeclarationStmt struct {
	AstBase
	Keyword string // "let", "var", "const"
	Name    string
	Type    types.Type // may be nil if needs inference
	Value   Expression
}

func (v *VariableDeclarationStmt) GetName() string { return v.Name }

// IsMutable returns true if this is a var declaration
func (v *VariableDeclarationStmt) IsMutable() bool { return v.Keyword == "var" }

// IsConstant returns true if this is a const declaration
func (v *VariableDeclarationStmt) IsConstant() bool { return v.Keyword == "const" }

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

func (f *FunctionDefStmt) GetName() string { return f.Name }

// FunctionClause represents a single clause of a function (pattern matching)
type FunctionClause struct {
	AstBase
	Parameters []Pattern
	Guard      Expression
	Body       Expression
}

// ReturnStmt represents a return statement
type ReturnStmt struct {
	AstBase
	Value Expression // nil for bare return
}
