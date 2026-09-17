package typechecker

import (
	"github.com/Lyra-Language/lyra/pkg/ast"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/types"
	"strings"
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

	// **A bound on the trait's own generic parameter** (`trait Holder<t: Tag>`), checked at
	// the impl that binds it. This is the supertrait check's sibling in every way: a claim
	// the declaration makes, verified where the impl and the trait are both in hand, and
	// unenforced until somebody looked — `impl Holder<NoTag> for Cell` compiled and ran with
	// no `Tag` impl on `NoTag` (09/16).
	//
	// **The impl is where it belongs, because the impl is what binds the parameter.** A
	// trait's parameter has no value until one does — `checkGenericTypeBounds` hangs off
	// resolving a *type*, which a trait never is, so the generic-type fix earlier the same
	// day could not reach this. `TraitArgs` are positional against `GenericParams`, which is
	// what the declaration's own comment says they are for.
	//
	// A type-variable argument is skipped, as the type side skips one: inside
	// `impl Holder<t> for Box<t>` the question is whether the *impl's* `t` carries the bound,
	// which is its `where` clause's business and is checked when that clause is used.
	for i, param := range trait.GenericParams {
		if len(param.Constraints) == 0 || i >= len(impl.TraitArgs) {
			continue
		}
		arg := tc.resolveTypeIfKnown(impl.TraitArgs[i], impl.GetLocation())
		if arg == nil {
			continue
		}
		if _, isVar := arg.(types.GenericType); isVar {
			continue
		}
		for _, traitName := range param.Constraints {
			if _, known := tc.symTable.LookupTraitFrom(traitName, impl.GetLocation()); !known {
				// An unknown trait in the bound is the declaration's mistake, reported there.
				continue
			}
			ok, why := tc.typeImplementsTraitWhy(arg, traitName, nil)
			if ok {
				continue
			}
			because := ""
			if why != "" {
				because = " — " + why
			}
			tc.addErrorCode(impl.GetLocation(), SeverityError, diag.CodeUnsatisfiedTraitBound,
				"impl of %s for %s: %s is bound at %s, which does not implement %s%s (required by `%s<%s: %s>`)",
				impl.TraitName, implType, param.Name, arg, traitName, because,
				trait.Name, param.Name, traitName)
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

		if !tc.checkSelfApplications(impl, traitMethod) {
			continue
		}
		traitSig := substituteSelf(tc.methodSignatureForImpl(trait, traitMethod.Signature, impl), impl.Type)
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

// methodSignatureForImpl is a trait method's signature as one impl sees it: the method's
// **own** type variables — those neither `Self` nor the trait's parameters, the `b` of
// `mapv: (Self, (i64) -> b) -> b` — renamed away from the impl's variables.
//
// Both kinds of variable meet in one namespace. The impl's body is checked with them side by
// side (`impl Pair for Box<t>`'s `self.v: t` beside a method's `t`), and a call's bindings map
// holds the impl's solved `t` and the method's solved `t` under one key, so the second
// overwrote the first and the body lowered at the wrong type (an llir store panic). A
// clashing method variable gets a prime (`t'`) until it is free. The impl check and dispatch
// both build the signature through this, so the renamed variable the body was checked with is
// the key the backend specializes it under.
func (tc *TypeChecker) methodSignatureForImpl(trait *ast.TraitDeclStmt, sig *types.LambdaType, impl *ast.TraitImplStmt) *types.LambdaType {
	renamed, _ := methodSignatureRenamedAway(trait, sig, typeVarsOfImpl(impl))
	return renamed
}

// typeVarsOfImpl is the type variables an impl declares by writing them: its target's and
// its trait arguments'.
func typeVarsOfImpl(impl *ast.TraitImplStmt) map[string]bool {
	vars := map[string]bool{}
	collectTypeVars(impl.Type, vars)
	for _, a := range impl.TraitArgs {
		collectTypeVars(a, vars)
	}
	return vars
}

// methodSignatureRenamedAway primes each of the method's own type variables that is in avoid,
// returning the signature and the renames made (trait's name → new name; nil for none).
func methodSignatureRenamedAway(trait *ast.TraitDeclStmt, sig *types.LambdaType, avoid map[string]bool) (*types.LambdaType, map[string]string) {
	if sig == nil {
		return nil, nil
	}
	implVars := avoid
	if len(implVars) == 0 {
		return sig, nil
	}
	sigVars := methodOwnTypeVars(trait, sig)
	rename := map[string]types.Type{}
	for v := range sigVars {
		if !implVars[v] {
			continue
		}
		fresh := v + "'"
		for implVars[fresh] || sigVars[fresh] {
			fresh += "'"
		}
		rename[v] = types.GenericType{Name: fresh}
	}
	if len(rename) == 0 {
		return sig, nil
	}
	names := make(map[string]string, len(rename))
	for v, g := range rename {
		names[v] = g.(types.GenericType).Name
	}
	return substituteSigGenerics(sig, rename), names
}

// checkSelfApplications refuses an impl whose target cannot stand for a `Self<…>` its method's
// signature writes, and reports whether the method may be checked further.
//
// `map: (Self<a>, (a) -> b) -> Self<b>` means the target's head at new arguments, which has
// one reading only for a target applied to exactly that many distinct type variables
// (types.SelfApplicable): `impl Functor for Box<t>` makes `Self<b>` `Box<b>`. For
// `Result<t, e>` it could be `Result<b, e>` or `Result<t, b>`, and for `i64` it is nothing, so
// those are refused rather than given a meaning a later rule would have to break. Skipping the
// body keeps the refusal the one diagnostic.
func (tc *TypeChecker) checkSelfApplications(impl *ast.TraitImplStmt, traitMethod ast.TraitMethod) bool {
	ok := true
	holed := types.HasHole(impl.Type)
	// A target with a hole has no reading for a bare `Self`: nothing says what fills `_`.
	if holed && traitMethod.Signature != nil && types.MentionsBareSelf(traitMethod.Signature) {
		tc.addError(impl.GetLocation(), SeverityError,
			"impl of %s for %s: the target has a hole, but method %q writes a bare Self, which does not say what fills `_`; a hole serves only a trait whose methods write Self<…>",
			impl.TraitName, impl.Type, traitMethod.Name.GetName())
		ok = false
	}
	for n := range selfApplicationsOf(traitMethod.Signature) {
		if types.SelfApplicable(impl.Type, n) {
			continue
		}
		if holed {
			tc.addError(impl.GetLocation(), SeverityError,
				"impl of %s for %s: method %q writes Self with %d type argument(s), so the target must have %d hole(s)",
				impl.TraitName, impl.Type, traitMethod.Name.GetName(), n, n)
		} else {
			tc.addError(impl.GetLocation(), SeverityError,
				"impl of %s for %s: method %q writes Self with %d type argument(s), so the target must be a generic type applied to %d distinct type variable(s), like Box<t>, or mark the positions with holes, like Result<_, e>",
				impl.TraitName, impl.Type, traitMethod.Name.GetName(), n, n)
		}
		ok = false
	}
	return ok
}

// methodOwnTypeVars is the type variables a trait method's signature mentions that are its own
// — neither `Self` (a SelfType, never collected) nor one of the trait's parameters.
func methodOwnTypeVars(trait *ast.TraitDeclStmt, sig *types.LambdaType) map[string]bool {
	vars := map[string]bool{}
	if sig == nil {
		return vars
	}
	for _, p := range sig.Parameters {
		collectTypeVars(p.Type, vars)
	}
	collectTypeVars(sig.ReturnType.Type, vars)
	for _, gp := range trait.GenericParams {
		delete(vars, gp.Name)
	}
	return vars
}

// substituteSelf replaces every SelfType occurrence in sig with concreteType, at any depth
// (`[]Self`, `Maybe<Self>`, a callback's `(Self) -> Self`).
func substituteSelf(sig *types.LambdaType, concreteType types.Type) *types.LambdaType {
	if sig == nil {
		return nil
	}
	subst := map[string]types.Type{types.SelfVar: concreteType}
	params := make([]types.ParameterType, len(sig.Parameters))
	for i, p := range sig.Parameters {
		params[i] = types.ParameterType{
			Type:         types.Substitute(p.Type, subst),
			DefaultValue: p.DefaultValue,
			Modifier:     p.Modifier,
			Borrow:       p.Borrow,
		}
	}
	return &types.LambdaType{
		Parameters: params,
		ReturnType: types.ReturnType{Type: types.Substitute(sig.ReturnType.Type, subst)},
	}
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
//
// **The trait half is keyed on the resolved declaration, not on the name** (08/14). A
// name does not identify a declaration (hazard 9), and a module may declare its own
// `Add` beside the prelude's — so `impl Add for i64` in each is two impls of two
// different traits, and calling them duplicates refuses a program that is correct. This
// is the same mistake dispatch already avoids by filtering candidate impls on the
// resolved declaration, for the same reason: filtering by name is what let a user's own
// `trait Ord` be taken for the prelude's.
//
// It was reachable before the prelude shipped arithmetic impls — a user's own
// `trait Show` plus `impl Show for i64` collides with `std/prelude/show.lyra` — and
// latent only because nothing had written that.
func (tc *TypeChecker) checkImplCoherence() {
	type key struct {
		trait  *ast.TraitDeclStmt
		args   string
		target string
	}
	first := map[key]*ast.TraitImplStmt{}
	for _, impl := range tc.traitImpls {
		if impl.Type == nil {
			continue
		}
		decl, known := tc.symTable.LookupTraitFrom(impl.TraitName, impl.GetLocation())
		if !known {
			// An unknown trait is checkTraitImpl's diagnostic; a second one here
			// would name the same line twice.
			continue
		}
		// The trait's arguments are part of the identity: `impl From<A> for High` beside
		// `impl From<B> for High` are two conversions, not one impl twice, and `?` picks
		// between them by the operand's error (fromConversion).
		args := make([]string, len(impl.TraitArgs))
		for i, a := range impl.TraitArgs {
			args[i] = a.String()
		}
		k := key{decl, strings.Join(args, ","), impl.Type.String()}
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
