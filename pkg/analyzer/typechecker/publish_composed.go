package typechecker

import (
	"sort"

	"github.com/Lyra-Language/lyra/pkg/typetable"
)

// Candidates for specializations only the driver discovers.
//
// A `where`-bound call is lowered through a candidate per implementing type
// (SetBoundCandidates), and a *generic* impl can only be a candidate once its target is
// concrete — which the typechecker publishes where it sees one: an instantiation at a
// concrete call site (checkGenericBounds), a concretely dispatched generic impl
// (publishImplBodyCandidates), a default reached at a concrete receiver
// (publishDefaultBodyCandidates). A specialization the driver's closure composes through a
// second generic — `outer<w>` calling `g(Box { v: y })`, where `g<u> where u: Get` calls
// `x.get()` — was never one of those sites: the typechecker saw `u = Box<w>`, and the backend,
// lowering `u = Box<i64>`, found no candidate (`no impl of Get for Box$i64`).
//
// The matching stays here, where dispatch does it; the driver says when. Both hooks are
// idempotent — a candidate is keyed by type and re-publishing replaces it with itself.

// PublishCandidatesForInstantiation publishes the bound-call candidates inside a generic
// function for one concrete specialization of it: each `where`-bound parameter at the type
// the specialization fixes it to.
func (tc *TypeChecker) PublishCandidatesForInstantiation(inst typetable.Instantiation) {
	lambda := inst.Func
	if lambda == nil || len(lambda.GenericBounds) == 0 {
		return
	}
	params := make([]string, 0, len(lambda.GenericBounds))
	for p := range lambda.GenericBounds {
		params = append(params, p)
	}
	sort.Strings(params)
	for _, p := range params {
		concrete, ok := inst.Subst[p]
		if !ok || concrete == nil || !mentionsNoTypeVar(concrete) {
			continue
		}
		for _, traitName := range lambda.GenericBounds[p] {
			tc.publishCandidatesAt(lambda, traitName, concrete, inst.Subst)
		}
	}
}

// PublishCandidatesForMethod is PublishCandidatesForInstantiation for a trait-method
// specialization: an impl method's bound calls through the impl's own `where` clause, and a
// default body's through `Self`, at the bindings r carries.
func (tc *TypeChecker) PublishCandidatesForMethod(r typetable.Resolution) {
	if r.Impl == nil || r.Method == nil {
		return
	}
	if trait, ok := tc.symTable.LookupTraitFrom(r.Impl.TraitName, r.Impl.GetLocation()); ok {
		if tm := findTraitMethodNamed(trait, r.Method.Name); tm != nil && tm.DefaultImpl() == r.Method {
			if self, ok := r.Bindings[selfVar]; ok && self != nil && mentionsNoTypeVar(self) {
				tc.publishDefaultBodyCandidates(trait, tm, self, r.Bindings)
			}
			return
		}
	}
	tc.publishImplBodyCandidates(resolvedTraitMethod{
		Impl: r.Impl, Method: r.Method, Signature: r.Signature, Bindings: r.Bindings,
		MethodVarNames: r.MethodVarNames,
	})
}
