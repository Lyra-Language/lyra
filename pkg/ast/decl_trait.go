package ast

import (
	"fmt"

	"github.com/Lyra-Language/lyra/pkg/types"
)

type TraitDeclStmt struct {
	AstBase
	Name string
	GenericParams []string
	Bounds []string
	GenericParameterConstraints []TraitGenericParameterConstraint
	Methods []TraitMethod
	IsPublic bool
}

func (t *TraitDeclStmt) statementNode() {}

func (t *TraitDeclStmt) GetName() string { return t.Name }

func (t *TraitDeclStmt) Print(indent string) {
	fmt.Printf("%sTraitDeclStmt(%s) {\n", indent, t.Name)
	if len(t.GenericParams) > 0 {
		fmt.Printf("%s\tGenericParams: %v\n", indent, t.GenericParams)
	}
	if len(t.GenericParameterConstraints) > 0 {
		fmt.Printf("%s\tGenericParameterConstraints: {\n", indent)
		for _, constraint := range t.GenericParameterConstraints {
			constraint.Print(indent + "\t\t")
		}
		fmt.Printf("%s\t}\n", indent)
	}
	if len(t.Bounds) > 0 {
		fmt.Printf("%s\tBounds: %v\n", indent, t.Bounds)
	}
	fmt.Printf("%s\tMethods: {\n", indent)
	for _, method := range t.Methods {
		method.Print(indent + "\t\t")
	}
	fmt.Printf("%s\t}\n", indent)
	if t.IsPublic {
		fmt.Printf("%s\tIsPublic: true\n", indent)
	} else {
		fmt.Printf("%s\tIsPublic: false\n", indent)
	}
	fmt.Printf("%s}\n", indent)
}

type TraitGenericParameterConstraint struct {
	Name string
	Constraints []string
}

func (t *TraitGenericParameterConstraint) GetName() string {
	return t.Name
}

func (t *TraitGenericParameterConstraint) Print(indent string) {
	fmt.Printf("%sTraitGenericParameterConstraint {\n", indent)
	fmt.Printf("%s\tName: %s\n", indent, t.Name)
	fmt.Printf("%s\tConstraints: %v\n", indent, t.Constraints)
	fmt.Printf("%s}\n", indent)
}

type TraitMethod struct {
	Name MethodName
	Signature *types.FunctionType
}

func (t *TraitMethod) GetName() string {
	return t.Name.GetName()
}

func (t *TraitMethod) Print(indent string) {
	fmt.Printf("%sTraitMethod {\n", indent)
	fmt.Printf("%s\tName: %s\n", indent, t.Name.GetName())
	fmt.Printf("%s\tSignature: {\n", indent)
	t.Signature.Print(indent + "\t\t")
	fmt.Printf("%s\t}\n", indent)
	fmt.Printf("%s}\n", indent)
}

// MethodNameKind classifies a trait method name. Prefix vs binary matters for
// operators that share a spelling (e.g. "-" unary vs binary).
type MethodNameKind byte

const (
	MethodNameKindIdentifier MethodNameKind = iota
	MethodNameKindPrefix
	MethodNameKindSuffix
	MethodNameKindBinary
)

type MethodName struct {
	Kind  MethodNameKind
	Value string // identifier text or operator spelling
}

func NewMethodNameIdentifier(name string) MethodName {
	return MethodName{Kind: MethodNameKindIdentifier, Value: name}
}

func NewMethodNamePrefix(op PrefixOperator) MethodName {
	return MethodName{Kind: MethodNameKindPrefix, Value: string(op)}
}

func NewMethodNameSuffix(op SuffixOperator) MethodName {
	return MethodName{Kind: MethodNameKindSuffix, Value: string(op)}
}

func NewMethodNameBinary(op BinaryOperator) MethodName {
	return MethodName{Kind: MethodNameKindBinary, Value: string(op)}
}

func (m MethodName) GetName() string {
	return m.Value
}

func (m MethodName) Print(indent string) {
	fmt.Printf("%sMethodName(%s)\n", indent, m.GetName())
}

type PrefixOperator string

const (
	Negate PrefixOperator = "-"
	Not PrefixOperator = "!"
	BitwiseNot PrefixOperator = "~"
)

type SuffixOperator string

const (
	Increment SuffixOperator = "++"
	Decrement SuffixOperator = "--"
)

type BinaryOperator string

const (
	Equal BinaryOperator = "=="
	NotEqual BinaryOperator = "!="
	GreaterThan BinaryOperator = ">"
	LessThan BinaryOperator = "<"
	GreaterThanOrEqual BinaryOperator = ">="
	LessThanOrEqual BinaryOperator = "<="
	Spaceship BinaryOperator = "<=>"
	And BinaryOperator = "&&"
	Or BinaryOperator = "||"
	Add BinaryOperator = "+"
	Subtract BinaryOperator = "-"
	Multiply BinaryOperator = "*"
	Divide BinaryOperator = "/"
	Modulus BinaryOperator = "%"
	Power BinaryOperator = "**"
	LeftShift BinaryOperator = "<<"
	RightShift BinaryOperator = ">>"
	BitwiseAnd BinaryOperator = "&"
	BitwiseOr BinaryOperator = "|"
	BitwiseXor BinaryOperator = "^"
)