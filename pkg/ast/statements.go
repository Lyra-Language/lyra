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
	if t.IsPublic {
		fmt.Printf("%s\tIsPublic: true\n", indent)
	}
	if t.GenericParams != nil {
		fmt.Printf("%s\tGenericParams: %v\n", indent, t.GenericParams)
	}
	if t.Type != nil {
		fmt.Printf("%s\tType: {\n", indent)
		t.Type.Print(indent + "\t\t")
		fmt.Printf("%s\t}\n", indent)
	}
	fmt.Printf("%s}\n", indent)
}

// ExpressionStmt wraps an expression used as a statement
type ExpressionStmt struct {
	AstBase
	Expression Expression
}

func (e *ExpressionStmt) GetName() string { return e.Expression.GetName() }

func (e *ExpressionStmt) Print(indent string) {
	fmt.Printf("%sExpressionStmt(%s)\n", indent, e.Expression.GetName())
	e.Expression.Print(indent + "\t")
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
	fmt.Printf("%sVarDeclStmt(%s) {\n", indent, v.Name)
	if v.Keyword != "" {
		fmt.Printf("%s\tKeyword: %s\n", indent, v.Keyword)
	}
	if v.Type != nil {
		fmt.Printf("%s\tType: %s\n", indent, v.Type.GetName())
	}
	if v.Value != nil {
		fmt.Printf("%s\tValue: {\n", indent)
		v.Value.Print(indent + "\t")
		fmt.Printf("%s\t}\n", indent)
	} else {
		fmt.Printf("%s\tValue: nil\n", indent)
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
		fmt.Printf("%s\tGenericParams: %v\n", indent, f.GenericParams)
	}
	if f.Signature != nil {
		fmt.Printf("%s\tSignature: %s\n", indent, f.Signature.GetName())
	}
	if f.Clauses != nil {
		fmt.Printf("%s\tClauses:\n", indent)
		for _, clause := range f.Clauses {
			clause.Print(indent + "\t")
		}
	}
	if f.IsPublic {
		fmt.Printf("%s\tIsPublic: true\n", indent)
	}
	if f.IsPure {
		fmt.Printf("%s\tIsPure: true\n", indent)
	}
	if f.IsAsync {
		fmt.Printf("%s\tIsAsync: true\n", indent)
	}
	fmt.Printf("%s}\n", indent)
}

// FunctionClause represents a single clause of a function (pattern matching)
type FunctionClause struct {
	AstBase
	Parameters []Parameter
	Guard      *GuardExpr
	Body       Expression
}

func (f *FunctionClause) Print(indent string) {
	parameters_str := ""
	for idx, parameter := range f.Parameters {
		if idx > 0 {
			parameters_str += ", "
		}
		parameters_str += parameter.GetName()
	}
	fmt.Printf("%sFunctionClause(%s)\n", indent, parameters_str)
	if f.Guard != nil {
		fmt.Printf("%s\tGuard: {\n", indent)
		f.Guard.Print(indent + "\t")
		fmt.Printf("%s\t}\n", indent)
	} else {
		fmt.Printf("%s\tGuard: nil\n", indent)
	}
	if f.Body != nil {
		fmt.Printf("%s\tBody: {\n", indent)
		f.Body.Print(indent + "\t")
		fmt.Printf("%s\t}\n", indent)
	} else {
		fmt.Printf("%s\tBody: nil\n", indent)
	}
	fmt.Printf("%s}\n", indent)
}

type Parameter struct {
	Pattern      Pattern
	DefaultValue Expression
}

func (p *Parameter) Print(indent string) {
	fmt.Printf("%sParameter(%s)\n", indent, p.Pattern.GetName())
	if p.DefaultValue != nil {
		fmt.Printf("%s\tDefaultValue: %v\n", indent, p.DefaultValue)
	}
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

func (r *ReturnStmt) Print(indent string) {
	fmt.Printf("%sReturnStmt(%s)\n", indent, r.Value.GetName())
}

func (r *ReturnStmt) GetName() string {
	return fmt.Sprintf("return %s", r.Value.GetName())
}
