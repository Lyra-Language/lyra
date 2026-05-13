package ast

import (
	"github.com/Lyra-Language/lyra/pkg/types"
)

type TraitDeclStmt struct {
	AstBase
	Name          string
	GenericParams []GenericParam
	Bounds        []string
	Methods       []TraitMethod
	IsPublic                    bool
}

func (t *TraitDeclStmt) statementNode() {}

func (t *TraitDeclStmt) GetName() string { return t.Name }

type TraitMethod struct {
	Name          MethodName
	Signature     *types.LambdaType
	DefaultMethod *LambdaClause
}

func (t *TraitMethod) GetName() string {
	return t.Name.GetName()
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

type PrefixOperator string

const (
	Negate     PrefixOperator = "-"
	Not        PrefixOperator = "!"
	BitwiseNot PrefixOperator = "~"
)

type SuffixOperator string

const (
	Increment SuffixOperator = "++"
	Decrement SuffixOperator = "--"
)

type BinaryOperator string

const (
	Equal              BinaryOperator = "=="
	NotEqual           BinaryOperator = "!="
	GreaterThan        BinaryOperator = ">"
	LessThan           BinaryOperator = "<"
	GreaterThanOrEqual BinaryOperator = ">="
	LessThanOrEqual    BinaryOperator = "<="
	Spaceship          BinaryOperator = "<=>"
	And                BinaryOperator = "&&"
	Or                 BinaryOperator = "||"
	Add                BinaryOperator = "+"
	Subtract           BinaryOperator = "-"
	Multiply           BinaryOperator = "*"
	Divide             BinaryOperator = "/"
	Modulus            BinaryOperator = "%"
	Power              BinaryOperator = "**"
	LeftShift          BinaryOperator = "<<"
	RightShift         BinaryOperator = ">>"
	BitwiseAnd         BinaryOperator = "&"
	BitwiseOr          BinaryOperator = "|"
	BitwiseXor         BinaryOperator = "^"
)
