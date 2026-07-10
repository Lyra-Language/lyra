package typechecker

import (
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
)

func (tc *TypeChecker) checkTraitImpl(impl *ast.TraitImplStmt) {
	trait, ok := tc.symTable.Traits[impl.TraitName]
	if !ok {
		tc.addError(impl.GetLocation(), SeverityError,
			"impl: unknown trait %q", impl.TraitName)
		return
	}

	// Put the impl's `where` bounds (`t: Show`) in scope for the duration of its
	// method-body checks, so a call on a value of type `t` can dispatch through
	// the bound (see dispatchViaGenericBound). Save/restore handles nesting.
	oldBounds := tc.genericBounds
	tc.genericBounds = map[string][]string{}
	for _, c := range impl.Constraints {
		tc.genericBounds[c.GenericType] = c.TraitBounds
	}
	defer func() { tc.genericBounds = oldBounds }()

	traitMethods := make(map[traitMethodKey]ast.TraitMethod, len(trait.Methods))
	for _, m := range trait.Methods {
		traitMethods[makeTraitMethodKey(m.Name)] = m
	}

	// Count how many impl clauses address each method name (multi-clause impls).
	implMethodCounts := make(map[traitMethodKey]int, len(impl.Methods))
	for _, m := range impl.Methods {
		implMethodCounts[makeTraitMethodKey(m.Name)]++
	}

	// 1. Required methods that the impl does not provide.
	for _, traitMethod := range trait.Methods {
		if traitMethod.DefaultMethod != nil {
			continue
		}
		if implMethodCounts[makeTraitMethodKey(traitMethod.Name)] == 0 {
			tc.addError(impl.GetLocation(), SeverityError,
				"impl of %s for %s: missing required method %q",
				impl.TraitName, impl.Type, traitMethod.Name.GetName())
		}
	}

	for _, implMethod := range impl.Methods {
		key := makeTraitMethodKey(implMethod.Name)
		traitMethod, declared := traitMethods[key]

		// 3. Method not declared in the trait.
		if !declared {
			tc.addError(implMethod.Clause.GetLocation(), SeverityWarning,
				"impl of %s for %s: method %q is not declared in trait",
				impl.TraitName, impl.Type, implMethod.Name.GetName())
			continue
		}

		// 2. Arity: number of clause patterns must match the trait signature.
		if traitMethod.Signature == nil {
			continue
		}
		want := len(traitMethod.Signature.Parameters)
		got := len(implMethod.Clause.Patterns)
		if got != want {
			tc.addError(implMethod.Clause.GetLocation(), SeverityError,
				"impl of %s for %s: method %q has wrong number of parameters: expected %d, got %d",
				impl.TraitName, impl.Type, implMethod.Name.GetName(), want, got)
			continue
		}

		traitSig := substituteSelf(traitMethod.Signature, impl.Type)

		// Full parameter-type and return-type comparison using TypesEqual with
		// Self substituted by the impl's concrete type.  This only fires when
		// the impl clause body is a *LambdaExpr with explicit parameter types
		// and an explicit return type annotation (uncommon in practice today but
		// wired up so it works when the grammar gains that capability).
		if lambda, ok := implMethod.Clause.Body.(*ast.LambdaExpr); ok {
			implSig := implLambdaSignature(lambda)
			if implSig != nil && !types.TypesEqual(implSig, traitSig) {
				tc.addError(implMethod.Clause.GetLocation(), SeverityError,
					"impl of %s for %s: method %q signature mismatch: expected %s, got %s",
					impl.TraitName, impl.Type, implMethod.Name.GetName(), traitSig, implSig)
			}
		}

		// Type-check the body so that any method calls inside are registered in
		// tc.methodTable, which is what lets inferImpurity's fixpoint track
		// method-to-method call chains when computing purity (FP/Imperative #3).
		tc.checkTraitImplMethodBody(implMethod, traitSig)
	}
}

// checkTraitImplMethodBody type-checks the body of one trait-impl method clause
// by calling inferExprType on it. The primary effect is that any method calls
// found inside the body get registered in tc.methodTable via the normal
// inferMemberCall → inferResolvedTraitMethodCall path, making them visible to
// inferImpurity's fixpoint for method-to-method purity tracking (FP/Imperative #3).
//
// traitSig is the trait's declared signature with Self already substituted for
// the impl's concrete type. Pattern names are bound in tc.paramTypes so that
// identifier references inside the body (e.g. the receiver "self") resolve to
// the correct types for dispatch.
func (tc *TypeChecker) checkTraitImplMethodBody(implMethod ast.TraitMethodImpl, traitSig *types.LambdaType) {
	oldTypes, oldMods := tc.paramTypes, tc.paramMods
	tc.paramTypes = make(map[string]types.Type)
	tc.paramMods = make(map[string]types.TypeModifier)
	for i, pat := range implMethod.Clause.Patterns {
		if i >= len(traitSig.Parameters) {
			break
		}
		if ip, ok := pat.(*ast.IdentifierPattern); ok && traitSig.Parameters[i].Type != nil {
			tc.paramTypes[ip.Name] = traitSig.Parameters[i].Type
		}
	}
	tc.inferExprType(implMethod.Clause.Body)
	tc.paramTypes, tc.paramMods = oldTypes, oldMods
}

// implLambdaSignature builds a *types.LambdaType from a LambdaExpr only when
// every parameter has an explicit type annotation AND the return type is
// annotated.  Returns nil if any annotation is missing.
func implLambdaSignature(lambda *ast.LambdaExpr) *types.LambdaType {
	if lambda.ReturnType.Type == nil {
		return nil
	}
	params := make([]types.ParameterType, len(lambda.Parameters))
	for i, p := range lambda.Parameters {
		if p.Type == nil {
			return nil
		}
		params[i] = types.ParameterType{Type: p.Type}
	}
	return &types.LambdaType{
		Parameters: params,
		ReturnType: lambda.ReturnType,
	}
}

// substituteSelf replaces every SelfType occurrence in sig with concreteType.
func substituteSelf(sig *types.LambdaType, concreteType types.Type) *types.LambdaType {
	if sig == nil {
		return nil
	}
	params := make([]types.ParameterType, len(sig.Parameters))
	for i, p := range sig.Parameters {
		params[i] = types.ParameterType{
			Type:         substituteTypeInSig(p.Type, concreteType),
			DefaultValue: p.DefaultValue,
			Modifier:     p.Modifier,
		}
	}
	return &types.LambdaType{
		Parameters: params,
		ReturnType: types.ReturnType{Type: substituteTypeInSig(sig.ReturnType.Type, concreteType)},
	}
}

func substituteTypeInSig(t types.Type, concreteType types.Type) types.Type {
	if t == nil {
		return nil
	}
	if _, ok := t.(types.SelfType); ok {
		return concreteType
	}
	return t
}

type traitMethodKey struct {
	Kind  ast.MethodNameKind
	Value string
}

func makeTraitMethodKey(name ast.MethodName) traitMethodKey {
	return traitMethodKey{Kind: name.Kind, Value: name.Value}
}
