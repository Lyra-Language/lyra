package typechecker

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/Lyra-Language/lyra/pkg/ast"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/types"
	"github.com/Lyra-Language/lyra/pkg/typetable"
)

// resolvedTraitMethod pairs a matched trait-impl method with the trait's own
// declared signature for it (needed to build the call's LambdaType, with
// Self substituted for the impl's concrete type).
type resolvedTraitMethod struct {
	Impl      *ast.TraitImplStmt
	Method    *ast.TraitMethodImpl
	Signature *types.LambdaType // trait's declared signature, Self already substituted; nil if the trait method has none
	// Bindings maps each of a generic impl's type variables to the concrete type
	// it unified with at this call site (empty for a non-generic impl). Consumed
	// by checkImplConstraints to verify the impl's `where` bounds.
	Bindings map[string]types.Type
}

// resolveTraitMethod finds every impl in the program whose target type
// structurally equals receiverType (via types.TypesEqual) and that provides
// an identifier-named method called methodName, optionally restricted to a
// single trait (requiredTrait != ""). Multiple matches without requiredTrait
// mean the call is genuinely ambiguous (two different traits implementing the
// same method name for the same type) — the caller decides what to do with
// len(matches) > 1.
//
// Only identifier-named methods participate (operator-overload methods like
// `(_==_)` are invoked through the operator itself, never through `.name()`
// or `TraitName::name()` syntax, so they're never a candidate here).
//
// A generic impl (`impl<t> Show for Box<t>`) matches when its target *unifies*
// with the receiver — its own `<t,…>` parameters act as wildcards binding to
// the receiver's corresponding type arguments (`Box<t>` matches `Box<i64>`),
// with binding-consistency (`Pair<t, t>` matches `Pair<i64, i64>` but not
// `Pair<i64, string>`). See implTargetMatches. Self is substituted with the
// concrete receiver type, so a method whose signature is in terms of Self and
// concrete types (Show/Debug/Hash-style) type-checks against the instantiation.
// A method that returns the impl's element type (`Container<e>.get -> e`) is not
// yet fully instantiated — trait-type-parameter binding is a separate feature.
func (tc *TypeChecker) resolveTraitMethod(receiverType types.Type, methodName string, requiredTrait string) []resolvedTraitMethod {
	var matches []resolvedTraitMethod
	for _, impl := range tc.traitImpls {
		if requiredTrait != "" && impl.TraitName != requiredTrait {
			continue
		}
		implType := tc.resolveTypeIfKnown(impl.Type, impl.GetLocation())
		bindings, ok := implTargetMatches(implType, receiverType)
		if !ok {
			continue
		}
		trait, ok := tc.symTable.LookupTraitFrom(impl.TraitName, impl.GetLocation())
		if !ok {
			continue
		}
		// Bind the trait's own type parameters: each declared param (`Get<e>` → e)
		// maps to the impl's corresponding trait argument (`impl Get<t>` → t),
		// with that argument's own variables resolved through the receiver
		// bindings ({t: i64}). So the trait method's `-> e` return becomes i64.
		traitSubst := map[string]types.Type{}
		for i, gp := range trait.GenericParams {
			if i < len(impl.TraitArgs) {
				traitSubst[gp.Name] = substituteGenerics(impl.TraitArgs[i], bindings)
			}
		}
		for i := range impl.Methods {
			m := &impl.Methods[i]
			if m.Name.Kind != ast.MethodNameKindIdentifier || m.Name.Value != methodName {
				continue
			}
			traitMethod := findTraitMethod(trait, methodName)
			if traitMethod == nil {
				// Not declared in the trait (the "extraneous method" case
				// checkTraitImpl already warns about) — not dispatchable.
				continue
			}
			var sig *types.LambdaType
			if traitMethod.Signature != nil {
				// Substitute Self with the concrete receiver (not impl.Type,
				// which for a generic impl still holds the `<t>` placeholder),
				// then bind the trait's own type parameters.
				sig = substituteSelf(traitMethod.Signature, receiverType)
				sig = substituteSigGenerics(sig, traitSubst)
			}
			matches = append(matches, resolvedTraitMethod{Impl: impl, Method: m, Signature: sig, Bindings: bindings})
		}
	}
	return matches
}

// implTargetMatches reports whether an impl's target type matches receiverType.
// A concrete target matches by structural equality (types.TypesEqual). A generic
// target — one containing any lowercase GenericType, Lyra's implicit type
// variables (an uppercase name is a concrete type, never a parameter) — matches
// when it *unifies* with the receiver, its GenericTypes binding to the receiver's
// corresponding subterms. (The impl's `<…>` after the trait name is the trait's
// argument list, not a parameter binder, so the variables are read off the
// target itself rather than a separate binder list.)
func implTargetMatches(implType, receiverType types.Type) (map[string]types.Type, bool) {
	bindings := map[string]types.Type{}
	if types.TypesEqual(implType, receiverType) {
		return bindings, true
	}
	generics := map[string]bool{}
	collectGenericNames(implType, generics)
	if len(generics) == 0 {
		return bindings, false
	}
	ok := unifyGenericTarget(implType, receiverType, generics, bindings)
	return bindings, ok
}

// collectGenericNames adds the name of every GenericType reachable within t to
// set — the implicit type variables of a generic impl target. It descends the
// composite types a target can take (parameterized, array, tuple).
func collectGenericNames(t types.Type, set map[string]bool) {
	switch v := t.(type) {
	case types.GenericType:
		set[v.Name] = true
	case types.ParameterizedType:
		for _, a := range v.TypeArguments {
			collectGenericNames(a, set)
		}
	case types.StaticArrayType:
		collectGenericNames(v.ElementType, set)
	case types.DynamicArrayType:
		collectGenericNames(v.ElementType, set)
	case types.TupleType:
		for _, e := range v.Elements {
			collectGenericNames(e, set)
		}
	case *types.LambdaType:
		for _, p := range v.Parameters {
			collectGenericNames(p.Type, set)
		}
		if v.ReturnType.Type != nil {
			collectGenericNames(v.ReturnType.Type, set)
		}
	}
}

// unifyGenericTarget reports whether implType matches receiverType, treating any
// GenericType named in `generics` (the impl's own parameters) as a wildcard that
// binds to the receiver's corresponding subterm. A parameter used more than once
// must bind consistently. Non-generic positions require structural equality;
// nominal types unify by head name, then pairwise over their type arguments —
// with a lenient fallback to head-name match when either side omits arguments
// (e.g. a receiver typed as the bare struct without instantiation).
func unifyGenericTarget(implType, receiverType types.Type, generics map[string]bool, bindings map[string]types.Type) bool {
	if g, ok := implType.(types.GenericType); ok && generics[g.Name] {
		if prior, bound := bindings[g.Name]; bound {
			return types.TypesEqual(prior, receiverType)
		}
		bindings[g.Name] = receiverType
		return true
	}
	switch it := implType.(type) {
	case types.DynamicArrayType:
		rt, ok := receiverType.(types.DynamicArrayType)
		return ok && unifyGenericTarget(it.ElementType, rt.ElementType, generics, bindings)
	case types.StaticArrayType:
		rt, ok := receiverType.(types.StaticArrayType)
		return ok && it.Size == rt.Size && unifyGenericTarget(it.ElementType, rt.ElementType, generics, bindings)
	case types.TupleType:
		rt, ok := receiverType.(types.TupleType)
		if !ok || len(it.Elements) != len(rt.Elements) {
			return false
		}
		for i := range it.Elements {
			if !unifyGenericTarget(it.Elements[i], rt.Elements[i], generics, bindings) {
				return false
			}
		}
		return true
	case *types.LambdaType:
		// A function type binds its variables through its own signature, so a
		// higher-order generic can be solved from the function it is handed:
		// `(m: Maybe<t>, f: () -> t) -> t` called with a `() -> i64` binds `t`.
		// Without this the unifier fell through to TypesEqual, which is never true
		// of `() -> t` against `() -> i64`, and the call reported "cannot infer type
		// variable t from these arguments" — leaving every combinator that takes a
		// callback (`unwrap_or_else`, `map`, `and_then`) unusable.
		//
		// Parameters are unified in the same direction as the return type. A
		// function type is contravariant in its parameters, but this is unification
		// against a *pattern*, not a subtyping test: both sides are concrete apart
		// from the variables being solved, so direction only decides which side a
		// variable may be read from, and reading it from either is correct here.
		rt, ok := receiverType.(*types.LambdaType)
		if !ok || len(it.Parameters) != len(rt.Parameters) {
			return false
		}
		for i := range it.Parameters {
			if !unifyGenericTarget(it.Parameters[i].Type, rt.Parameters[i].Type, generics, bindings) {
				return false
			}
		}
		if it.ReturnType.Type == nil || rt.ReturnType.Type == nil {
			// An un-inferred return (an unannotated lambda literal) pins nothing.
			return it.ReturnType.Type == rt.ReturnType.Type
		}
		return unifyGenericTarget(it.ReturnType.Type, rt.ReturnType.Type, generics, bindings)
	}
	implName, implArgs, implNominal := nominalHead(implType)
	recvName, recvArgs, recvNominal := nominalHead(receiverType)
	if implNominal && recvNominal {
		if implName != recvName {
			return false
		}
		if len(implArgs) == 0 || len(recvArgs) == 0 {
			return true // one side carries no type arguments: match on head name
		}
		if len(implArgs) != len(recvArgs) {
			return false
		}
		for i := range implArgs {
			if !unifyGenericTarget(implArgs[i], recvArgs[i], generics, bindings) {
				return false
			}
		}
		return true
	}
	return types.TypesEqual(implType, receiverType)
}

// nominalHead extracts the head name and type arguments of a nominal type: a
// ParameterizedType carries both; a NamedStructType, DataType, or UnresolvedType
// carries only a name (its concrete instantiation, when any, lives in a
// ParameterizedType). Returns ok=false for non-nominal types (primitives,
// tuples, lambdas, …).
func nominalHead(t types.Type) (name string, args []types.Type, ok bool) {
	switch v := t.(type) {
	case types.ParameterizedType:
		return v.Name, v.TypeArguments, true
	case types.NamedStructType:
		return v.Name, nil, true
	case types.DataType:
		return v.Name, nil, true
	case types.UnresolvedType:
		return v.Name, nil, true
	}
	return "", nil, false
}

// checkImplConstraints verifies a matched generic impl's `where` bounds against
// the types its variables unified with. For `impl Ord<t> for Box<t> where t:
// Ord` dispatched on `Box<Widget>`, the variable `t` is bound to Widget, so each
// bound (`Ord`) must be satisfied by Widget — i.e. some impl of that trait must
// exist for Widget. An unbound variable (a receiver that carried no type
// argument in that position) is skipped: there is nothing concrete to check.
func (tc *TypeChecker) checkImplConstraints(match resolvedTraitMethod, loc ast.Location) {
	for _, c := range match.Impl.Constraints {
		bound, ok := match.Bindings[c.GenericType]
		if !ok || bound == nil {
			continue
		}
		for _, traitName := range c.TraitBounds {
			if !tc.typeImplementsTrait(bound, traitName) {
				tc.addError(loc, SeverityError,
					"%s does not implement %s, required by this impl's `%s: %s` bound",
					bound, traitName, c.GenericType, traitName)
			}
		}
	}
}

// typeImplementsTrait reports whether some impl of traitName applies to t — the
// bound-satisfaction test for a `where` clause, on a generic impl and (since 08/07)
// on a generic *binding* checked at its instantiation. It reuses the
// same target-matching used for dispatch (so `i64` satisfies `Ord` via `impl Ord
// for i64`, and a generic `impl Ord<u> for Box<u>` would satisfy `Box<i64>`).
// This is a single level: the matched impl's *own* `where` bounds are not
// recursively verified here (a deliberate first-cut limit — the recursive
// obligation surfaces when that impl is itself dispatched).
func (tc *TypeChecker) typeImplementsTrait(t types.Type, traitName string) bool {
	for _, impl := range tc.traitImpls {
		if impl.TraitName != traitName {
			continue
		}
		implType := tc.resolveTypeIfKnown(impl.Type, impl.GetLocation())
		if _, ok := implTargetMatches(implType, t); ok {
			return true
		}
	}
	return false
}

// dispatchViaGenericBound resolves a `.method()` call whose receiver is a bare
// type parameter `recv` (e.g. `self.value` of type `t` inside a generic impl
// body) against the traits `recv` is bounded by in scope (`where t: Show`). If
// one of those traits declares an identifier method of that name, the call is
// type-checked against the trait's signature with Self substituted by the
// parameter, and its return type is returned. This is *abstract* dispatch: there
// is no concrete impl to record (the actual impl is chosen when the enclosing
// generic is instantiated at its own call site, where checkImplConstraints has
// already verified the bound holds), so nothing is written to the MethodTable.
func (tc *TypeChecker) dispatchViaGenericBound(recv types.GenericType, methodName string, call *ast.FunctionCallExpr) (types.Type, bool) {
	for _, traitName := range tc.genericBounds[recv.Name] {
		trait, ok := tc.symTable.LookupTraitFrom(traitName, call.GetLocation())
		if !ok {
			continue
		}
		tm := findTraitMethod(trait, methodName)
		if tm == nil || tm.Signature == nil {
			continue
		}
		// Record the abstract resolution so the purity checker can account for it
		// (joining over the bound trait method's concrete impls) instead of
		// treating the call as an unverifiable external one.
		tc.methodTable.SetBound(call, typetable.BoundMethodRef{Trait: traitName, Method: methodName})
		// Publish one concrete resolution per implementing type, so the backend can pick
		// the one its specialization names. Done here rather than in the backend because
		// the matching (implTargetMatches, Self substitution, the trait's own parameter
		// bindings) is dispatch's job — a second copy in codegen is free to disagree with
		// the one that type-checked the call, which is the drift Resolution exists to
		// prevent.
		tc.publishBoundCandidates(call, traitName, methodName)
		sig := substituteSelf(tm.Signature, recv)
		return tc.inferDotCallFromType(traitName+"::"+methodName, sig, call), true
	}
	return nil, false
}

func findTraitMethod(trait *ast.TraitDeclStmt, methodName string) *ast.TraitMethod {
	for i := range trait.Methods {
		if trait.Methods[i].Name.Kind == ast.MethodNameKindIdentifier && trait.Methods[i].Name.Value == methodName {
			return &trait.Methods[i]
		}
	}
	return nil
}

// traitNamesOf renders the trait names of matches for an ambiguity message,
// e.g. `Pilot, Wizard`.
func traitNamesOf(matches []resolvedTraitMethod) string {
	names := make([]string, len(matches))
	for i, m := range matches {
		names[i] = m.Impl.TraitName
	}
	return strings.Join(names, ", ")
}

// inferResolvedTraitMethodCall type-checks call's arguments against a single
// resolved trait method's signature and returns its declared return type,
// recording the resolution in the MethodTable so later passes (the purity
// checker) can find it without re-deriving dispatch.
//
// receiver is the expression supplying Signature.Parameters[0] (Self,
// already substituted to the impl's concrete type) for a `.`-call
// (`n.show()`), whose receiver is implicit — never present in call.Arguments
// — so call.Arguments lines up against Signature.Parameters[1:]. Pass nil for
// a fully-qualified call (`Show::show(n)`), where the receiver is already an
// ordinary call.Arguments[0] and the whole parameter list lines up directly.
func (tc *TypeChecker) inferResolvedTraitMethodCall(calleeName string, match resolvedTraitMethod, call *ast.FunctionCallExpr, receiver ast.Expression) types.Type {
	tc.checkImplConstraints(match, call.GetLocation())
	// The full resolution, not just the method: the backend needs the impl it came
	// from and the receiver-substituted signature to emit the call.
	tc.methodTable.SetResolution(call, typetable.Resolution{
		Impl:      match.Impl,
		Method:    match.Method,
		Signature: match.Signature,
		// The bindings a generic impl unified with, which dispatch has already worked
		// out for the `where`-bound check below. They travel with the resolution
		// because the *body* is monomorphized against them: one emitted function per
		// distinct binding set, analyzed for ownership at the concrete types.
		Bindings: match.Bindings,
	})
	if match.Signature == nil {
		// No declared signature to check args against (shouldn't normally
		// happen — resolveTraitMethod only matches methods the trait declares
		// — but a trait method may omit a signature when it has a body-only
		// default; arity/type isn't checkable then).
		return nil
	}
	if receiver == nil {
		return tc.inferLambdaCallFromType(calleeName, match.Signature, call)
	}
	return tc.inferDotCallFromType(calleeName, match.Signature, call)
}

// inferDotCallFromType is inferLambdaCallFromType's counterpart for a
// `.`-call whose receiver is implicit: lambdaType.Parameters[0] (Self) has no
// corresponding entry in call.Arguments — already confirmed to match by the
// TypesEqual check in resolveTraitMethod — so only Parameters[1:] is checked
// against call.Arguments.
func (tc *TypeChecker) inferDotCallFromType(calleeName string, lambdaType *types.LambdaType, call *ast.FunctionCallExpr) types.Type {
	if len(lambdaType.Parameters) == 0 {
		// A trait method signature normally always has Self as its first
		// parameter; a malformed zero-param signature has no receiver slot to
		// have matched against in the first place.
		tc.addError(call.GetLocation(), SeverityError,
			"%s: method has no receiver parameter", calleeName)
		return lambdaType.ReturnType.Type
	}
	rest := lambdaType.Parameters[1:]
	required := 0
	for _, p := range rest {
		if p.DefaultValue == nil {
			required++
		}
	}
	total := len(rest)
	got := len(call.Arguments)

	if got < required || got > total {
		if required == total {
			tc.addError(call.GetLocation(), SeverityError,
				"%s: expected %d argument(s), got %d", calleeName, total, got)
		} else {
			tc.addError(call.GetLocation(), SeverityError,
				"%s: expected %d to %d argument(s), got %d",
				calleeName, required, total, got)
		}
		return lambdaType.ReturnType.Type
	}

	for i, arg := range call.Arguments {
		param := rest[i]
		if param.Type == nil {
			continue
		}
		argType := tc.inferExprType(arg)
		if argType == nil {
			continue
		}
		// Resolve the declared parameter type before comparing, exactly as inferLambdaCall
		// does. A trait signature stores a named type as an UnresolvedType (just the name),
		// so comparing it raw made a struct-typed parameter reject its own type with
		// "cannot assign Cell to Cell" — the same shape the free-function path fixed when
		// declared return types started resolving.
		paramType := tc.resolveType(param.Type, arg.GetLocation())
		if !isAssignable(argType, paramType) {
			tc.addError(arg.GetLocation(), SeverityError,
				"%s: argument %d: cannot assign %s to %s",
				calleeName, i+1, argType, paramType)
			continue
		}
		// The borrow modes a trait signature declares are checked exactly as a free
		// function's are (inferLambdaCall): a `mut` argument must be a mutable lvalue,
		// since the callee writes through to it, and an `own` one adopts the value into
		// the callee's storage so the allocation flavors must match. Without these a
		// `mut Self` method silently accepted a temporary and discarded every write.
		if param.Borrow == types.Mut {
			tc.checkMutArgument(calleeName, i+1, "", arg, paramType)
		}
		if paramOwnsArgument(param.Borrow) {
			tc.checkAllocationCompat(argType, paramType, arg.GetLocation(),
				fmt.Sprintf("%s: argument %d", calleeName, i+1))
		}
	}

	return lambdaType.ReturnType.Type
}

// pushGenericBounds puts a declaration's `where` bounds in scope for the duration of
// its body check and returns the restore. It is the binding-side twin of the impl
// block in checkTraitImpl, which does the same thing inline for an impl's
// constraints — both feed dispatchViaGenericBound, and a bound that is in scope for
// one form and not the other is a bound that means different things depending on
// where it is written.
//
// Save/restore rather than merge, so a nested generic declaration cannot leak its
// parameters' bounds outward — and so a shadowing parameter of the same name gets its
// own bounds rather than inheriting the outer ones.
func (tc *TypeChecker) pushGenericBounds(params []ast.GenericParam) func() {
	if len(params) == 0 {
		return func() {}
	}
	old := tc.genericBounds
	next := make(map[string][]string, len(params))
	for _, p := range params {
		if len(p.Constraints) > 0 {
			next[p.Name] = p.Constraints
		}
	}
	if len(next) == 0 {
		return func() {}
	}
	// Inherit the enclosing scope's bounds for parameters this declaration does not
	// rebind: a method of a generic impl sees both the impl's `t` and its own.
	for name, bounds := range old {
		if _, shadowed := next[name]; !shadowed {
			next[name] = bounds
		}
	}
	tc.genericBounds = next
	return func() { tc.genericBounds = old }
}

// checkGenericBounds verifies the solved type arguments of one call against the
// callee's `where` bounds, and reports each unsatisfied one at the call.
//
// This is where a bound stops being decoration. Until 08/07 the bounds were
// collected and never checked, so `describe(p)` on a `Pt` with no `Show` impl
// type-checked clean and then failed in the backend with
// `llvm: unsupported method call "show"` — the same hazard-5 inversion the
// type-name member call had, and the same fix: refuse it in the front end, where the
// author can be told which bound failed and on what.
//
// A parameter the solve left unbound is skipped rather than reported: the call has
// already been diagnosed for whatever prevented the solve, and a second error naming
// a bound on a type nobody knows is noise.
//
// **Only a concrete argument is checked.** Inside another generic body, `t` may be
// solved to a *type variable* — `let via<u> where u: Show = (x: u) -> string =>
// describe(x)` binds describe's `t` to `u` — and whether that satisfies `Show` is a
// question about the *enclosing* declaration's bounds, not about any impl. It is
// satisfied when the enclosing scope bounds that variable by the same trait, which is
// what tc.genericBounds holds while the body is checked; anything else is left alone
// rather than guessed, since reporting there would make a correctly-forwarded bound an
// error.
func (tc *TypeChecker) checkGenericBounds(calleeName string, lambda *ast.LambdaExpr, call *ast.FunctionCallExpr, subst map[string]types.Type) {
	if len(lambda.GenericBounds) == 0 {
		return
	}
	// Deterministic order: a call may fail several bounds, and a diagnostic list that
	// reorders between runs is one nobody can baseline in a test.
	names := make([]string, 0, len(lambda.GenericBounds))
	for name := range lambda.GenericBounds {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, param := range names {
		arg, solved := subst[param]
		if !solved || arg == nil {
			continue
		}
		concrete := tc.resolveTypeIfKnown(arg, call.GetLocation())
		for _, traitName := range lambda.GenericBounds[param] {
			if _, ok := tc.symTable.LookupTraitFrom(traitName, call.GetLocation()); !ok {
				// An unknown trait in the bound is the declaration's problem, and the
				// declaration is where it is reported. Silently satisfying the bound
				// here would turn one error into none.
				continue
			}
			if g, isVar := concrete.(types.GenericType); isVar {
				if !slices.Contains(tc.genericBounds[g.Name], traitName) {
					tc.addErrorCode(call.GetLocation(), SeverityError, diag.CodeUnsatisfiedTraitBound,
						"%s: %s is instantiated at the type parameter %s, which is not bound by %s; add `where %s: %s` to the enclosing declaration",
						calleeName, param, g.Name, traitName, g.Name, traitName)
				}
				continue
			}
			if !tc.typeImplementsTrait(concrete, traitName) {
				tc.addErrorCode(call.GetLocation(), SeverityError, diag.CodeUnsatisfiedTraitBound,
					"%s: %s is instantiated at %s, which does not implement %s (required by `where %s: %s`)",
					calleeName, param, concrete, traitName, param, traitName)
			}
		}
	}
}

// publishBoundCandidates records the concrete resolution of a `where`-bound call for
// every type that implements the bound trait (MethodTable.SetBoundCandidates).
//
// The abstract resolution above is all the *typechecker* needs — the receiver is a
// type variable and every implementing type type-checks identically against the
// trait's signature. The backend needs more: it lowers one specialization at a time,
// where the variable has become a concrete type and the call must name a real
// function. Enumerating here is what lets it do that without re-implementing impl
// matching.
//
// Every implementing type is published rather than only the ones some specialization
// reaches, because which those are is not known until the instantiation set is closed
// — which happens in the driver, after this pass. The set is small (the impls of one
// trait) and a candidate nobody selects costs nothing: it is a table entry, not an
// emitted function.
func (tc *TypeChecker) publishBoundCandidates(call *ast.FunctionCallExpr, traitName, methodName string) {
	byType := map[string]typetable.Resolution{}
	for _, impl := range tc.traitImpls {
		if impl.TraitName != traitName {
			continue
		}
		target := tc.resolveTypeIfKnown(impl.Type, impl.GetLocation())
		if target == nil {
			continue
		}
		matches := tc.resolveTraitMethod(target, methodName, traitName)
		if len(matches) != 1 {
			// Zero: the impl does not provide this method (checkTraitImpl reports that).
			// More than one: ambiguous at this type, which the call site reports when it
			// is reached concretely. Neither is this function's to diagnose.
			continue
		}
		m := matches[0]
		byType[target.String()] = typetable.Resolution{
			Impl: m.Impl, Method: m.Method, Signature: m.Signature, Bindings: m.Bindings,
		}
	}
	tc.methodTable.SetBoundCandidates(call, byType)
}
