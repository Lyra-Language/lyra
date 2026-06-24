package ast

import "github.com/Lyra-Language/lyra/pkg/types"

type TraitImplStmt struct {
	AstBase
	TraitName string
	GenericParams []GenericParam
	Type types.Type
	Constraints []TraitImplConstraint
	Methods []TraitMethodImpl
}

func (t *TraitImplStmt) statementNode() {}

func (t *TraitImplStmt) GetName() string { return t.TraitName }

type TraitImplConstraint struct {
	GenericType string
	TraitBounds []string
}

type TraitMethodImpl struct {
	Name   MethodName
	IsPure bool
	Clause LambdaClause
}

func (t *TraitMethodImpl) GetName() string { return t.Name.GetName() }
