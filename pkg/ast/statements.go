package ast

import (
	"fmt"

	"github.com/Lyra-Language/lyra/pkg/types"
)

// TypeDeclarationStmt represents a type declaration (struct, data type, etc.)
type TypeDeclStmt struct {
	AstBase
	Name          string
	GenericParams []string
	Type          types.Type
	IsPublic      bool
}

func (t *TypeDeclStmt) GetName() string { return t.Name }

func (t *TypeDeclStmt) Print(indent string) {
	fmt.Printf("%sTypeDeclStmt(%s) {\n", indent, t.Name)
	if t.GenericParams != nil {
		fmt.Printf("%s  GenericParams: %v\n", indent, t.GenericParams)
	}
	if t.Type != nil {
		fmt.Printf("%s  Type:\n", indent)
		t.Type.Print(indent + "    ")
	}
	if t.IsPublic {
		fmt.Printf("%s  IsPublic: true\n", indent)
	}
	fmt.Printf("%s}\n", indent)
}

// ExpressionStmt wraps an expression used as a statement
type ExpressionStmt struct {
	AstBase
	Expression Expression
}

// VariableDeclarationStmt represents a let/var/const binding
type VarDeclStmt struct {
	AstBase
	Keyword string // "let", "var", "const"
	Name    string
	Type    types.Type // may be nil if needs inference
	Value   Expression
}

func (v *VarDeclStmt) GetName() string { return v.Name }

func (v *VarDeclStmt) Print(indent string) {
	fmt.Printf("%sVarDeclStmt(%s)\n", indent, v.Name)
	if v.Keyword != "" {
		fmt.Printf("%s  Keyword: %s\n", indent, v.Keyword)
	}
	if v.Type != nil {
		fmt.Printf("%s  Type: %s\n", indent, v.Type.GetName())
	}
	if v.Value != nil {
		fmt.Printf("%s  Value: %s\n", indent, v.Value.GetName())
	}
	fmt.Printf("%s}\n", indent)
}

// IsMutable returns true if this is a var declaration
func (v *VarDeclStmt) IsMutable() bool { return v.Keyword == "var" }

// IsConstant returns true if this is a const declaration
func (v *VarDeclStmt) IsConstant() bool { return v.Keyword == "const" }

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

func (f *FunctionDefStmt) Print(indent string) {
	fmt.Printf("%sFunctionDefStmt(%s)\n", indent, f.Name)
	if f.GenericParams != nil {
		fmt.Printf("%s  GenericParams: %v\n", indent, f.GenericParams)
	}
	if f.Signature != nil {
		fmt.Printf("%s  Signature: %s\n", indent, f.Signature.GetName())
	}
	if f.Clauses != nil {
		fmt.Printf("%s  Clauses:\n", indent)
		for _, clause := range f.Clauses {
			clause.Print(indent + "    ")
		}
	}
	if f.IsPublic {
		fmt.Printf("%s  IsPublic: true\n", indent)
	}
	if f.IsPure {
		fmt.Printf("%s  IsPure: true\n", indent)
	}
	if f.IsAsync {
		fmt.Printf("%s  IsAsync: true\n", indent)
	}
	fmt.Printf("%s}\n", indent)
}

// FunctionClause represents a single clause of a function (pattern matching)
type FunctionClause struct {
	AstBase
	Parameters []Pattern
	Guard      Expression
	Body       Expression
}

func (f *FunctionClause) Print(indent string) {
	parameters_str := "("
	for idx, parameter := range f.Parameters {
		if idx > 0 {
			parameters_str += ", "
		}
		parameters_str += parameter.GetName()
	}
	parameters_str += ")"
	fmt.Printf("%sFunctionClause(%s)\n", indent, parameters_str)
	if f.Guard != nil {
		fmt.Printf("%s  Guard: %s\n", indent, f.Guard.GetName())
	} else {
		fmt.Printf("%s  Guard: nil\n", indent)
	}
	if f.Body != nil {
		fmt.Printf("%s  Body: %s\n", indent, f.Body.GetName())
	} else {
		fmt.Printf("%s  Body: nil\n", indent)
	}
	fmt.Printf("%s}\n", indent)
}

// ReturnStmt represents a return statement
type ReturnStmt struct {
	AstBase
	Value Expression // nil for bare return
}
