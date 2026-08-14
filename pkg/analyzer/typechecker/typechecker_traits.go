package typechecker

import (
	"github.com/Lyra-Language/lyra/pkg/ast"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/types"
)

func (tc *TypeChecker) checkTraitImpl(impl *ast.TraitImplStmt) {
	trait, ok := tc.symTable.LookupTraitFrom(impl.TraitName, impl.GetLocation())
	if !ok {
		tc.addError(impl.GetLocation(), SeverityError,
			"impl: unknown trait %q", impl.TraitName)
		return
	}

	// A supertrait is a promise that every implementer of this trait also implements
	// the ones it names, which is what would let a `where t: B` bound reach `A`'s
	// methods. Checked here because this is where the impl and its trait are both in
	// hand; unchecked until 08/07, when a sweep for AST fields nothing reads found
	// `TraitDeclStmt.Bounds` with no consumer at all — so `trait B: A` parsed and
	// `impl B for S` compiled with no `A` anywhere.
	implType := tc.resolveTypeIfKnown(impl.Type, impl.GetLocation())
	for _, bound := range trait.Bounds {
		if _, known := tc.symTable.LookupTraitFrom(bound, impl.GetLocation()); !known {
			// An unknown supertrait is the *declaration's* mistake, reported there.
			continue
		}
		if !tc.typeImplementsTrait(implType, bound) {
			tc.addErrorCode(impl.GetLocation(), SeverityError, diag.CodeUnsatisfiedSupertrait,
				"impl of %s for %s: %s requires %s, which %s does not implement",
				impl.TraitName, implType, impl.TraitName, bound, implType)
		}
	}

	// Put the impl's `where` bounds (`t: Show`) in scope for the duration of its
	// method-body checks, so a call on a value of type `t` can dispatch through
	// the bound (see dispatchViaGenericBound). Save/restore handles nesting.
	//
	// Closed over supertraits by the same helper pushGenericBounds uses, which is the
	// point of that comment about the two being twins: a bound that reaches `A`'s
	// methods when written on a binding and not when written on an impl would mean two
	// different things depending on where it is written.
	oldBounds := tc.genericBounds
	tc.genericBounds = map[string][]string{}
	for _, c := range impl.Constraints {
		tc.genericBounds[c.GenericType] = tc.closeOverSupertraits(c.TraitBounds, impl.GetLocation())
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
		// Bind the trait's own type parameters to the impl's trait arguments
		// (`Get<e>`'s `e` → `impl Get<t>`'s `t`), so the signature — in particular
		// the return type — is expressed in the impl's own variables and matches
		// what the body produces (`get = (self) => self.value` yields `t`). No
		// receiver bindings here: this is the abstract impl-definition check.
		if len(trait.GenericParams) > 0 && len(impl.TraitArgs) > 0 {
			traitParamSubst := map[string]types.Type{}
			for i, gp := range trait.GenericParams {
				if i < len(impl.TraitArgs) {
					traitParamSubst[gp.Name] = impl.TraitArgs[i]
				}
			}
			traitSig = substituteSigGenerics(traitSig, traitParamSubst)
		}

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

		// Type-check the body: verify it against the declared return type, and
		// register any method calls inside in tc.methodTable (which lets
		// inferImpurity's fixpoint track method-to-method call chains — FP/Imperative #3).
		// Record which method of which type is being checked, so the `show` desugar can
		// refuse to rewrite `${self}` into a call to the very method it is inside. See
		// showApplies.
		prevMethod, prevType := tc.currentImplMethod, tc.currentImplType
		tc.currentImplMethod, tc.currentImplType = implMethod.Name, implType
		tc.checkTraitImplMethodBody(implMethod.Name.GetName(), implMethod, traitSig)
		tc.currentImplMethod, tc.currentImplType = prevMethod, prevType
	}
}

// checkTraitImplMethodBody type-checks one trait-impl method clause body against
// the trait's declared return type, mirroring checkLambdaBody for a free
// function. It also causes any method calls inside the body to be registered in
// tc.methodTable via the normal inferMemberCall path, making them visible to
// inferImpurity's fixpoint for method-to-method purity tracking (FP/Imperative #3).
//
// traitSig is the trait's declared signature with Self substituted for the
// impl's concrete type and the trait's own type parameters bound to the impl's
// trait arguments — so both the parameter types bound into tc.paramTypes and the
// return type checked against the body are expressed in the impl's variables.
func (tc *TypeChecker) checkTraitImplMethodBody(methodName string, implMethod ast.TraitMethodImpl, traitSig *types.LambdaType) {
	oldTypes, oldMods := tc.paramTypes, tc.paramMods
	tc.paramTypes = make(map[string]types.Type)
	tc.paramMods = make(map[string]types.TypeModifier)
	defer func() { tc.paramTypes, tc.paramMods = oldTypes, oldMods }()
	for i, pat := range implMethod.Clause.Patterns {
		if i >= len(traitSig.Parameters) {
			break
		}
		if traitSig.Parameters[i].Type == nil {
			continue
		}
		// **Resolved**, like the return type below. The two used to disagree — the
		// return was resolved and the parameters were not — so `same = (self) => self`
		// against `(Self) -> Self` compared an UnresolvedType against a
		// NamedStructType and failed with `expected Vec2, got Vec2`. A method returning
		// its own receiver is an ordinary thing to write, and the error named the same
		// type twice, which is the signature of exactly this asymmetry.
		paramType := tc.resolveTypeIfKnown(traitSig.Parameters[i].Type, pat.GetLocation())
		if ip, ok := pat.(*ast.IdentifierPattern); ok {
			tc.paramTypes[ip.Name] = paramType
			continue
		}
		// A destructuring parameter — `total = ({ x, y }) => x + y`. Its names come
		// from the signature's type the same way withParamScope derives a free
		// function's, and through the same walker, so a pattern binds identically in
		// an impl method and in a plain lambda. The impl writes no annotation of its
		// own (the trait's signature supplies it), which is what makes this reachable
		// where an unannotated destructured parameter on a free function is not.
		tc.walkDestructuredPattern(pat, paramType, func(name string, typ types.Type) {
			tc.paramTypes[name] = typ
		})
	}

	// Track the enclosing return type so a `?` inside the body resolves (mirrors
	// checkLambdaBody). Save/restore handles nesting.
	prevRet := tc.enclosingRet
	ret := traitSig.ReturnType
	tc.enclosingRet = &ret
	prevName := tc.enclosingFuncName
	tc.enclosingFuncName = methodName
	defer func() { tc.enclosingRet, tc.enclosingFuncName = prevRet, prevName }()

	body := implMethod.Clause.Body
	declaredReturn := tc.resolveTypeIfKnown(traitSig.ReturnType.Type, body.GetLocation())
	if declaredReturn == nil {
		tc.inferExprType(body) // no declared return to check against; still infer
		return
	}
	// An owned return must match the declared return type's allocation flavor; a
	// `ref`/`mut` return is a borrow and polymorphic (see isOwnedReturn).
	ownedReturn := isOwnedReturn(traitSig.ReturnType.TypeModifier)
	_, isVoid := declaredReturn.(types.VoidType)
	if block, ok := body.(*ast.BlockExpr); ok {
		if isVoid {
			tc.checkBlockVoidReturn(methodName, block)
		} else {
			tc.checkBlockReturn(methodName, block, declaredReturn, ownedReturn)
		}
		return
	}
	// Single-expression body: its value is the return value. A void one is still
	// inferred so an effectful call is validated, exactly as in checkLambdaBody.
	if isVoid {
		tc.inferExprType(body)
		return
	}
	tc.checkReturnValue(methodName, body, body.GetLocation(), declaredReturn, ownedReturn)
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
		// Borrow travels with the parameter: a signature built from a lambda that drops
		// it would let the call site and the body disagree about who owns the receiver.
		params[i] = types.ParameterType{Type: p.Type, Borrow: p.TypeModifier}
	}
	return &types.LambdaType{
		Parameters: params,
		ReturnType: lambda.ReturnType,
	}
}

// substituteSigGenerics applies a generic substitution (type-parameter name →
// concrete type) to every parameter type and the return type of sig — used to
// bind a trait's own type parameters (`get: (self) -> e` with e → i64). A nil
// signature or empty substitution is returned unchanged.
func substituteSigGenerics(sig *types.LambdaType, subst map[string]types.Type) *types.LambdaType {
	if sig == nil || len(subst) == 0 {
		return sig
	}
	params := make([]types.ParameterType, len(sig.Parameters))
	for i, p := range sig.Parameters {
		params[i] = p
		params[i].Type = substituteGenerics(p.Type, subst)
	}
	return &types.LambdaType{
		Parameters: params,
		ReturnType: types.ReturnType{Type: substituteGenerics(sig.ReturnType.Type, subst)},
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
			Borrow:       p.Borrow,
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

// checkImplCoherence reports a second `impl <Trait> for <Type>` for a trait/type pair
// that already has one.
//
// Run once over the gathered impls rather than at dispatch, so the diagnostic lands on
// the declaration that caused it and appears once — a call-site report would name a
// line that is correct, repeat per call, and leave the reader hunting for the pair.
//
// **Identical targets only.** `impl Show for Box<t>` beside `impl Show for Box<i64>`
// *overlaps* without being identical, and deciding which is more specific is the
// specificity ordering this language deliberately does not have (see the
// receiver-keyed overloading decision). Rejecting exact duplicates is the part that is
// unambiguous, and it is what the `Eq` override needs; genuine overlap is left open
// rather than half-answered.
//
// Keyed on the *written* target type rather than a resolved one: this runs before
// anything is resolved, and two impls whose targets resolve to one type through
// different aliases is the overlap case above, not this one.
func (tc *TypeChecker) checkImplCoherence() {
	type key struct{ trait, target string }
	first := map[key]*ast.TraitImplStmt{}
	for _, impl := range tc.traitImpls {
		if impl.Type == nil {
			continue
		}
		k := key{impl.TraitName, impl.Type.String()}
		prev, seen := first[k]
		if !seen {
			first[k] = impl
			continue
		}
		tc.addErrorCode(impl.GetLocation(), SeverityError, diag.CodeDuplicateTraitImpl,
			"duplicate impl: %s is already implemented for %s at %s; a trait may be implemented once per type, or which impl a call uses would depend on declaration order",
			impl.TraitName, impl.Type, prev.GetLocation().Pretty())
	}
}
