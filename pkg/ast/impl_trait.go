package ast

import "github.com/Lyra-Language/lyra/pkg/types"

type TraitImplStmt struct {
	AstBase
	TraitName     string
	GenericParams []GenericParam
	// TraitArgs are the trait's type arguments written after the trait name
	// (`impl Get<t> for Box<t>` → [t]), positionally matched to the trait's
	// declared type parameters. Used to bind a trait's own parameters at a call
	// site (the method's `-> e` return becomes the impl's `t`, then the
	// receiver's concrete type). Distinct from GenericParams, which the grammar
	// does not populate for impls.
	TraitArgs   []types.Type
	Type        types.Type
	Constraints []TraitImplConstraint
	Methods     []TraitMethodImpl
	// Doc is the `///` comment block above the `impl`, nil if undocumented — for
	// what this *particular* implementation does differently, the trait's own doc
	// covering what every implementation must do.
	Doc *Doc
}

func (t *TraitImplStmt) statementNode() {}

func (t *TraitImplStmt) GetName() string { return t.TraitName }

type TraitImplConstraint struct {
	GenericType string
	TraitBounds []string
}

type TraitMethodImpl struct {
	Name      MethodName
	IsPure    bool
	IsDet     bool
	IsNoAlloc bool
	Clause    LambdaClause
	// Doc is the `///` comment block above this method inside the impl.
	Doc *Doc
}

func (t *TraitMethodImpl) GetName() string { return t.Name.GetName() }
