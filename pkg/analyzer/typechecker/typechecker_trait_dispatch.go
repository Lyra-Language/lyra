package typechecker

import (
	"strings"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
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
		implType := tc.resolveTypeIfKnown(impl.Type)
		bindings, ok := implTargetMatches(implType, receiverType)
		if !ok {
			continue
		}
		trait, ok := tc.symTable.Traits[impl.TraitName]
		if !ok {
			continue
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
				// which for a generic impl still holds the `<t>` placeholder).
				sig = substituteSelf(traitMethod.Signature, receiverType)
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
// bound-satisfaction test for a generic impl's `where` clause. It reuses the
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
		implType := tc.resolveTypeIfKnown(impl.Type)
		if _, ok := implTargetMatches(implType, t); ok {
			return true
		}
	}
	return false
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
	tc.methodTable.Set(call, match.Method)
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
		if !isAssignable(argType, param.Type) {
			tc.addError(arg.GetLocation(), SeverityError,
				"%s: argument %d: cannot assign %s to %s",
				calleeName, i+1, argType, param.Type)
		}
	}

	return lambdaType.ReturnType.Type
}
