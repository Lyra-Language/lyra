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
// Only identifier-named methods participate here: an operator-overload method like
// `(_+_)` is invoked through the operator itself, never through `.name()` or
// `TraitName::name()` syntax. The operator's own dispatch goes through
// resolveTraitMethodNamed, which is this function keyed on the full name.
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
	return tc.resolveTraitMethodNamed(receiverType, ast.NewMethodNameIdentifier(methodName), requiredTrait)
}

// resolveTraitMethodNamed is resolveTraitMethod keyed on a full MethodName rather
// than an identifier, which is what lets an **operator** find its impl: `a + b`
// asks for the binary `+` method on a's type, exactly as `a.show()` asks for the
// identifier `show`. Everything else — impl matching, Self substitution, trait
// type-parameter binding — is the same work, and deliberately not a second copy of
// it: the two dispatch paths differing would be a divergence nobody would see until
// a generic impl behaved differently under an operator than under a call.
func (tc *TypeChecker) resolveTraitMethodNamed(receiverType types.Type, methodName ast.MethodName, requiredTrait string) []resolvedTraitMethod {
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
		provided := false
		for i := range impl.Methods {
			m := &impl.Methods[i]
			if m.Name != methodName {
				continue
			}
			traitMethod := findTraitMethodNamed(trait, methodName)
			if traitMethod == nil {
				// Not declared in the trait (the "extraneous method" case
				// checkTraitImpl already warns about) — not dispatchable.
				continue
			}
			provided = true
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
		// An impl that provides no clause for the name falls back to the trait's
		// **default**, if it declares one. Tried after the impl's own methods and only
		// when they matched nothing, which is what makes an impl's clause an override
		// rather than an ambiguity — the same last-rung shape a newtype's method
		// fallback has.
		if !provided {
			if m, sig, ok := tc.defaultMatch(trait, methodName, receiverType, traitSubst); ok {
				// `Self` joins the impl's own bindings rather than replacing them, so a
				// generic impl's variables survive: a default running for
				// `impl Show for Box<t>` at `Box<i64>` needs both t→i64 and Self→Box<i64>.
				withSelf := make(map[string]types.Type, len(bindings)+1)
				for k, v := range bindings {
					withSelf[k] = v
				}
				withSelf[selfVar] = receiverType
				matches = append(matches, resolvedTraitMethod{Impl: impl, Method: m, Signature: sig, Bindings: withSelf})
			}
		}
	}
	return matches
}

// defaultMatch is the trait's default clause for methodName presented as an impl method,
// with the signature Self-substituted for this receiver exactly as a provided method's is.
//
// The signature is built for the *concrete* receiver while the body was checked with Self
// abstract, which is the same split a generic function has: the call site is checked
// against the instantiated signature, the body once against the variable.
func (tc *TypeChecker) defaultMatch(trait *ast.TraitDeclStmt, methodName ast.MethodName, receiverType types.Type, traitSubst map[string]types.Type) (*ast.TraitMethodImpl, *types.LambdaType, bool) {
	traitMethod := findTraitMethodNamed(trait, methodName)
	if traitMethod == nil || traitMethod.DefaultMethod == nil {
		return nil, nil, false
	}
	var sig *types.LambdaType
	if traitMethod.Signature != nil {
		sig = substituteSelf(traitMethod.Signature, receiverType)
		sig = substituteSigGenerics(sig, traitSubst)
	}
	// The body's own bound calls need a candidate at *this* receiver, or the default
	// type-checks abstractly and fails to lower. See publishDefaultBodyCandidates.
	tc.publishDefaultBodyCandidates(trait, traitMethod, receiverType)
	return traitMethod.DefaultImpl(), sig, true
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
	case *types.ConstrainedType:
		// A newtype is nominal — `types.HeadName` says so for receiver-keyed
		// overloading, and this is the same question one layer down. Without the arm a
		// `newtype Name = string` receiver fell through to TypesEqual against the
		// declared `self: Name` (an UnresolvedType at that point) and never matched, so
		// a method *written for* a newtype was unreachable while the base's were not —
		// hazard 8, and it inverted the precedence the call chain promises.
		if v == nil {
			return "", nil, false
		}
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
//
// **A matched impl's own `where` bounds are verified too** (08/14). Matching on the
// target's head alone meant a generic impl satisfied the bound for *every*
// instantiation of its target, including ones it explicitly excludes:
// `impl Arithmetic for Complex<t> where t: Arithmetic` made `Complex<string>` satisfy
// `where u: Arithmetic`, which type-checked clean and died in the backend as
// `llvm: type not found` — hazard 5 inverted.
//
// That single level was a documented first-cut limit, on the reasoning that "the
// recursive obligation surfaces when that impl is itself dispatched". What it misses is
// the impl that **is** its constraint: an umbrella impl has no methods, so there is no
// later dispatch, and this is the only place the question is ever asked.
func (tc *TypeChecker) typeImplementsTrait(t types.Type, traitName string) bool {
	ok, _ := tc.typeImplementsTraitWhy(t, traitName, nil)
	return ok
}

// implGoal is one "does T implement Trait?" question, for the in-progress set below.
// Keyed on the type's rendering rather than its identity, since the same type arrives as
// distinct values through different resolutions.
type implGoal struct{ trait, target string }

// typeImplementsTraitWhy is typeImplementsTrait plus the innermost constraint that
// failed, so a caller can say *because*: "Complex<string> does not implement Arithmetic"
// is not much use without "string does not implement Arithmetic" after it.
//
// `proving` carries the goals already on the stack. A constraint chain that leads back to
// its own goal is not a proof, so re-entry answers **no** rather than looping — and only
// that branch dies: the impl loop continues, so a goal provable another way still is. In
// practice the recursion terminates on its own, since each step strips a layer of type
// argument (`Complex<Complex<f64>>` → `Complex<f64>` → `f64`); the set is the guard for
// the case that does not, which would otherwise hang the typechecker and read as a frozen
// editor.
func (tc *TypeChecker) typeImplementsTraitWhy(t types.Type, traitName string, proving map[implGoal]bool) (bool, string) {
	goal := implGoal{trait: traitName}
	if t != nil {
		goal.target = t.String()
	}
	if proving[goal] {
		return false, ""
	}
	for _, impl := range tc.traitImpls {
		if impl.TraitName != traitName {
			continue
		}
		implType := tc.resolveTypeIfKnown(impl.Type, impl.GetLocation())
		bindings, matched := implTargetMatches(implType, t)
		if !matched {
			continue
		}
		if len(impl.Constraints) == 0 {
			return true, ""
		}
		if proving == nil {
			proving = map[implGoal]bool{}
		}
		proving[goal] = true
		held, why := tc.implConstraintsHold(impl, bindings, proving)
		delete(proving, goal)
		if held {
			return true, ""
		}
		// Keep the first explanation rather than the last: impls are tried in
		// declaration order, and a later near-miss is no more the author's intent
		// than an earlier one.
		if why != "" {
			return false, why
		}
	}
	return false, ""
}

// implConstraintsHold checks a matched impl's `where` clause against the bindings the
// match produced — `impl Arithmetic for Complex<t> where t: Arithmetic` matched at
// `Complex<string>` binds t to string, so the question becomes whether string is
// Arithmetic.
//
// **A binding that is itself a type variable is skipped, not failed.** Inside a generic
// body `t` may bind to another declaration's parameter, and whether *that* satisfies the
// bound is a question about the enclosing declaration's `where` clause rather than about
// any impl — the same rule checkGenericBounds states for its own type-variable arm, and
// the reason the supertrait obligation on `impl Arithmetic for Complex<t>` does not
// report against its own parameter.
func (tc *TypeChecker) implConstraintsHold(impl *ast.TraitImplStmt, bindings map[string]types.Type, proving map[implGoal]bool) (bool, string) {
	for _, c := range impl.Constraints {
		bound, bound_ok := bindings[c.GenericType]
		if !bound_ok || bound == nil {
			// The target did not bind this parameter, so there is nothing concrete to
			// check — matching checkImplConstraints, which skips an unbound variable.
			continue
		}
		if _, isVar := bound.(types.GenericType); isVar {
			continue
		}
		for _, traitName := range c.TraitBounds {
			if ok, _ := tc.typeImplementsTraitWhy(bound, traitName, proving); !ok {
				return false, fmt.Sprintf("%s does not implement %s", bound, traitName)
			}
		}
	}
	return true, ""
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
	return findTraitMethodNamed(trait, ast.NewMethodNameIdentifier(methodName))
}

// findTraitMethodNamed is the same lookup keyed on a full MethodName, so an
// operator-named method (`(_+_)`) is found by the operator that invokes it. Kind is
// part of the key: prefix `-` and binary `-` share a spelling and are different
// methods.
func findTraitMethodNamed(trait *ast.TraitDeclStmt, name ast.MethodName) *ast.TraitMethod {
	for i := range trait.Methods {
		if trait.Methods[i].Name == name {
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
	// A concrete dispatch onto a generic impl is an instantiation, and the impl's body
	// has bound-dispatched sites of its own — the operator path does the same after
	// SetOperatorResolution. Both are needed: `measure(Box { v: Box { v: 7 } })` reaches
	// `impl Depth for Box<t>` at `t = Box<i64>`, whose `self.v.depth()` must find the
	// same impl one level down, and fixing only the operator half leaves the identical
	// failure under a `.method()` call.
	tc.publishImplBodyCandidates(match)
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
		return tc.resolveTypeIfKnown(lambdaType.ReturnType.Type, call.GetLocation())
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
		return tc.resolveTypeIfKnown(lambdaType.ReturnType.Type, call.GetLocation())
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
		if !tc.assignableValue(arg, argType, paramType) {
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

	// Resolved for the same reason the parameters above are — the comment there records
	// the parameter half of this fix landing first and this half being missed, which is
	// hazard 8's return-position asymmetry to the letter. Raw, a bound call's result
	// carried `Opt<(Idx, Len)>` against a caller's resolved `Opt<(i64, i64)>` (a type
	// alias), and a struct return rejected itself as "expected Maybe<Hit>, got
	// Maybe<Hit>". The quiet twin: an unknown name in the signature is the trait
	// declaration's error, already reported once.
	return tc.resolveTypeIfKnown(lambdaType.ReturnType.Type, call.GetLocation())
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
func (tc *TypeChecker) pushGenericBounds(params []ast.GenericParam, loc ast.Location) func() {
	if len(params) == 0 {
		return func() {}
	}
	old := tc.genericBounds
	next := make(map[string][]string, len(params))
	for _, p := range params {
		if len(p.Constraints) > 0 {
			next[p.Name] = tc.closeOverSupertraits(p.Constraints, loc)
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

// closeOverSupertraits returns `declared` plus every trait reachable from it through
// supertrait edges (`trait B: A`), in a deterministic order — the declared bounds
// first, then what they drag in.
//
// This is what makes a supertrait mean anything at a **use** site. checkTraitImpl
// enforces the promise on every impl (`impl B for T` requires an `impl A for T`), so a
// `where t: B` bound may rely on it: `v.foo()` must reach A's method, and forwarding `v`
// to a callee bounded `where u: A` must satisfy that bound. Both were refused before
// 08/14, the second while telling the author to add a bound `B` already guarantees.
//
// Expanding **here** — at the one place a bound set enters scope — rather than at each
// of the four readers is hazard 8's rule: dispatchViaGenericBound, the operator path,
// the `Show` desugar and checkGenericBounds' type-variable arm all ask "what is `t`
// bounded by?", and four copies of the answer are four chances to drift. A fifth reader
// gets this for free.
//
// Cycle-safe by a visited set rather than by assuming a DAG: `trait A: B` alongside
// `trait B: A` is legal and merely means the two are always implemented together, which
// is exactly what checkTraitImpl then requires of every implementer.
func (tc *TypeChecker) closeOverSupertraits(declared []string, loc ast.Location) []string {
	if len(declared) == 0 {
		return declared
	}
	seen := make(map[string]bool, len(declared))
	closed := make([]string, 0, len(declared))
	queue := append([]string(nil), declared...)
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		if seen[name] {
			continue
		}
		seen[name] = true
		closed = append(closed, name)
		trait, ok := tc.symTable.LookupTraitFrom(name, loc)
		if !ok {
			// An unknown trait in a bound is the declaration's own error and is
			// reported there; contributing nothing here keeps that the only one.
			continue
		}
		queue = append(queue, trait.Bounds...)
	}
	return closed
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
			// The bound holds, so this instantiation fixes the callee's parameter at a
			// concrete type — the one thing the candidate tables were missing for a
			// *generic* impl. Publish before reporting, since a failing bound has no
			// specialization to publish for.
			tc.publishCandidatesAt(lambda, traitName, concrete)
			if ok, why := tc.typeImplementsTraitWhy(concrete, traitName, nil); !ok {
				// The `because` clause is what makes a nested failure actionable: an
				// impl may apply to `Complex<t>` in general and be excluded here by its
				// own `where` clause, and naming only the outer type says nothing about
				// which part of it was wrong.
				because := ""
				if why != "" {
					because = " — " + why
				}
				tc.addErrorCode(call.GetLocation(), SeverityError, diag.CodeUnsatisfiedTraitBound,
					"%s: %s is instantiated at %s, which does not implement %s%s (required by `where %s: %s`)",
					calleeName, param, concrete, traitName, because, param, traitName)
			}
		}
	}
}

// publishCandidatesAt fills in the candidate key a **generic** impl can never supply on
// its own: the substituted concrete type.
//
// The candidate tables are keyed by the impl's *written* target, which is right for a
// concrete impl (`impl Show for i64` keys `i64`, and the backend looks up `i64`) and never
// matches for a generic one — `impl Add for Box<t>` keys the literal string `Box<t>` while
// the specialization looks up `Box<i64>`. So a generic impl was unreachable through a
// bound in **both** forms, a method call and an operator, each failing in the backend with
// its own message (`no impl of Sized2 for it`, `type not found for *ast.IdentifierExpr`)
// for one cause.
//
// **This is the only place both halves of the question are in hand.** The callee's body
// holds the bound-dispatched sites, and the instantiation fixes the type — the typechecker
// knows the concrete type here and nowhere the body is visible, and the backend knows the
// body but must not re-derive the match (that is the drift `Resolution` exists to
// prevent). So the match stays here and the answer is published, exactly as the original
// candidate publication does.
//
// Sites are filtered by trait rather than by which parameter they belong to. A generic
// with two bounded parameters may publish a candidate under the wrong one, which costs a
// table entry nobody selects — the same trade `publishBoundCandidates` already documents.
func (tc *TypeChecker) publishCandidatesAt(lambda *ast.LambdaExpr, traitName string, concrete types.Type) {
	if lambda == nil || lambda.Body == nil || concrete == nil {
		return
	}
	if _, isVar := concrete.(types.GenericType); isVar {
		return
	}
	keys := candidateKeys(concrete)
	// **The bound's trait is not the method's trait once supertraits exist.** A site
	// under `where u: Arithmetic` dispatches `(_+_)` through `Add`, because `Arithmetic`
	// declares nothing — so matching the ref against the bound's name alone publishes
	// for `Box<t> where t: Add` and silently skips the umbrella case that motivated all
	// of this. The closure is the same one that makes the bound reach the method.
	reachable := tc.closeOverSupertraits([]string{traitName}, lambda.GetLocation())
	onExpr := func(e ast.Expression) bool {
		if call, isCall := e.(*ast.FunctionCallExpr); isCall {
			if ref, ok := tc.methodTable.GetBound(call); ok && slices.Contains(reachable, ref.Trait) {
				if m, found := tc.matchOneAt(concrete, ast.NewMethodNameIdentifier(ref.Method), ref.Trait); found {
					for _, k := range keys {
						tc.methodTable.AddBoundCandidate(call, k, resolutionOf(m))
					}
					tc.publishImplBodyCandidates(m)
				}
			}
		}
		if ref, ok := tc.methodTable.OperatorBound(e); ok && slices.Contains(reachable, ref.Trait) {
			if m, found := tc.matchOneAt(concrete, methodNameFromKey(ref.Method), ref.Trait); found {
				for _, k := range keys {
					tc.methodTable.AddOperatorCandidate(e, k, resolutionOf(m))
				}
				tc.publishImplBodyCandidates(m)
			}
		}
		return true
	}
	ast.WalkExpr(lambda.Body, func(ast.Statement) bool { return true }, onExpr)
}

// publishImplBodyCandidates is publishCandidatesAt for the *inside* of a generic impl
// that was just dispatched concretely.
//
// A concrete dispatch fixes the impl's own variables, so the impl's method body becomes a
// specialization — and its bound-dispatched sites need the same candidate the call site
// needed. `Box<Box<i64>> + Box<Box<i64>>` matches `impl Add for Box<t>` at `t = Box<i64>`,
// and the body's `self.v + o.v` is a site checked when `t` was still a variable, so its
// candidates are keyed by written targets and miss `Box<i64>`.
//
// The impl's own `where` clause supplies both halves: which trait each variable is bounded
// by, and — through the bindings this match produced — the concrete type it now stands
// for. Recursion is by construction rather than by a loop: publishing walks a body whose
// own dispatches publish in turn, and each step strips a layer of type argument, so
// `Box<Box<Box<i64>>>` terminates for the same reason the constraint walk does.
func (tc *TypeChecker) publishImplBodyCandidates(m resolvedTraitMethod) {
	if m.Impl == nil || m.Method == nil || len(m.Bindings) == 0 {
		return
	}
	if m.Method.Clause.Body == nil {
		return
	}
	// Publishing walks a body whose own sites publish in turn, so the same impl can be
	// re-entered at the same bindings — directly for a self-referential type, or through
	// two impls that reach each other. Each step normally strips a layer of type
	// argument and terminates, and this is the guard for when it does not: without it
	// the typechecker spins, which reads as a frozen editor rather than a crash.
	key := fmt.Sprintf("%p|%s|%s", m.Impl, m.Method.Name.Key(), describeBindings(m.Bindings))
	if tc.publishing == nil {
		tc.publishing = map[string]bool{}
	}
	if tc.publishing[key] {
		return
	}
	tc.publishing[key] = true
	defer delete(tc.publishing, key)
	body := &ast.LambdaExpr{Body: m.Method.Clause.Body}
	for _, c := range m.Impl.Constraints {
		concrete, ok := m.Bindings[c.GenericType]
		if !ok || concrete == nil {
			continue
		}
		for _, traitName := range c.TraitBounds {
			tc.publishCandidatesAt(body, traitName, concrete)
		}
	}
}

// candidateKeys is every spelling the backend might look this type up under.
//
// **A generic struct is monomorphized before it is lowered**, so inside a specialization
// the receiver's recorded type is the *substituted* one — named `Box$i64`, not
// `Box<i64>` — and a candidate filed only under the source rendering is never found. The
// mangling is `typetable.TypeSymbol`, which is shared rather than reproduced here, because
// a second copy of a naming scheme is a silent miss the day either side changes.
//
// Both are published rather than picking one: which form reaches the lookup depends on
// whether the receiver's type survived substitution, and an unselected key costs a map
// entry.
func candidateKeys(concrete types.Type) []string {
	keys := []string{concrete.String()}
	if sym := typetable.MonoTypeKey(concrete); sym != keys[0] {
		keys = append(keys, sym)
	}
	return keys
}

// resolveOneAt resolves a trait method at a concrete type. The trait it requires is the
// **ref's** — the one that declares the method — not the bound's: under `where u:
// Arithmetic` the umbrella declares nothing, and asking for `(_+_)` on `Arithmetic`
// matches no impl at all. That distinction only exists because supertraits do.
//
// Exactly one match is required, the rule boundCandidatesByType applies for the same
// reasons —
// the same rule boundCandidatesByType applies, for the same reasons: none means the impl
// does not provide it (reported at the impl) and several is an ambiguity the concrete call
// site reports.
func (tc *TypeChecker) matchOneAt(concrete types.Type, method ast.MethodName, traitName string) (resolvedTraitMethod, bool) {
	matches := tc.resolveTraitMethodNamed(concrete, method, traitName)
	if len(matches) != 1 {
		return resolvedTraitMethod{}, false
	}
	return matches[0], true
}

// describeBindings renders a binding set deterministically, for the in-progress key
// above. Sorted, since a map's iteration order would make the guard fire at random.
func describeBindings(b map[string]types.Type) string {
	names := make([]string, 0, len(b))
	for n := range b {
		names = append(names, n)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, n := range names {
		parts = append(parts, n+"="+b[n].String())
	}
	return strings.Join(parts, ",")
}

func resolutionOf(m resolvedTraitMethod) typetable.Resolution {
	return typetable.Resolution{
		Impl: m.Impl, Method: m.Method, Signature: m.Signature, Bindings: m.Bindings,
	}
}

// methodNameFromKey inverts ast.MethodName.Key() for the operator spellings, since a
// BoundMethodRef carries the key string rather than the structured name. Kind is part of a
// method's identity — prefix `-` and binary `-` are different methods — so a bare Value
// would resolve the wrong one.
func methodNameFromKey(key string) ast.MethodName {
	if len(key) >= 4 && strings.HasPrefix(key, "(_") && strings.HasSuffix(key, "_)") {
		return ast.NewMethodNameBinary(ast.BinaryOperator(key[2 : len(key)-2]))
	}
	if len(key) >= 3 && strings.HasPrefix(key, "(") && strings.HasSuffix(key, "_)") {
		return ast.NewMethodNamePrefix(ast.PrefixOperator(key[1 : len(key)-2]))
	}
	if len(key) >= 3 && strings.HasPrefix(key, "(_") && strings.HasSuffix(key, ")") {
		return ast.NewMethodNameSuffix(ast.SuffixOperator(key[2 : len(key)-1]))
	}
	return ast.NewMethodNameIdentifier(key)
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
	tc.methodTable.SetBoundCandidates(call,
		tc.boundCandidatesByType(traitName, ast.NewMethodNameIdentifier(methodName)))
}

// boundCandidatesByType is the map itself: one resolution per implementing type, keyed by
// the type's string. Shared by the call form above and by the *operator* form
// (dispatchOperatorViaBound), which publishes into the operator-candidate table instead —
// the same question asked for a different kind of node, so the same answer.
func (tc *TypeChecker) boundCandidatesByType(traitName string, methodName ast.MethodName) map[string]typetable.Resolution {
	byType := map[string]typetable.Resolution{}
	for _, impl := range tc.traitImpls {
		if impl.TraitName != traitName {
			continue
		}
		target := tc.resolveTypeIfKnown(impl.Type, impl.GetLocation())
		if target == nil {
			continue
		}
		matches := tc.resolveTraitMethodNamed(target, methodName, traitName)
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
	return byType
}

// ordTraitName is the kind of the trait that extends comparison beyond the primitives.
//
// It is a **kind, not a spelling**, as of 08/08: the prelude marks its trait
// `@builtin(Ord)`, and `canonicalTraitDecl` resolves the kind to whichever declaration
// carries it. Before the marker the name *was* the identity, so a program's own
// `trait Ord` in the entry module was silently taken for the prelude's — see the
// canonical pass in the collector for the two rules (marker wins; an unmarked,
// correctly-shaped trait of that name is the fallback when nothing claims the kind).
const ordTraitName = "Ord"

// canonicalTraitDecl is the declaration carrying kind ("Ord"/"Eq"), or nil.
func (tc *TypeChecker) canonicalTraitDecl(kind string) *ast.TraitDeclStmt {
	if tc.symTable == nil {
		return nil
	}
	for _, decl := range tc.symTable.Traits {
		if decl != nil && decl.CanonicalKind == kind {
			return decl
		}
	}
	return nil
}

// canonicalTraitMatches resolves recv's impls of methodName and keeps only those whose
// trait **is** the canonical one for kind.
//
// The identity check is the point, and filtering by *name* is what it replaces. An impl
// says `impl Ord for Ver`, and `Ord` there resolves as the impl's own file sees it — so
// with a user `trait Ord` in scope, a by-name filter matched the user's impl against the
// prelude's kind and handed the backend a `compare` returning i64. It failed with
// "Ord::compare must return the prelude's Ordering", which is the backend catching a
// front-end mistake (rule 5) rather than the front end declining to dispatch.
//
// With no canonical declaration at all — a snippet with no prelude — the name is all
// there is, so the filter falls back to it and behaves as it always did.
func (tc *TypeChecker) canonicalTraitMatches(recv types.Type, methodName, kind string) []resolvedTraitMethod {
	canonical := tc.canonicalTraitDecl(kind)
	if canonical == nil {
		return tc.resolveTraitMethod(recv, methodName, kind)
	}
	var kept []resolvedTraitMethod
	for _, m := range tc.resolveTraitMethod(recv, methodName, canonical.Name) {
		if decl, ok := tc.symTable.LookupTraitFrom(m.Impl.TraitName, m.Impl.GetLocation()); ok && decl == canonical {
			kept = append(kept, m)
		}
	}
	return kept
}

// dispatchOrdCompare accepts a comparison whose operands are ordered by an `Ord` impl
// rather than by being numeric, recording the resolution for the backend. Reports
// whether it handled the comparison.
//
// Both operands must be the *same* type: ordering is a relation on one type, and
// admitting a cross-type comparison would need a rule for which side's impl wins.
// A primitive is never routed here — `1 < 2` stays a machine comparison rather than
// becoming a call — so an `Ord` impl cannot change what the operators mean on the
// built-in types.
func (tc *TypeChecker) dispatchOrdCompare(expr *ast.BooleanBinaryOpExpr, left, right types.Type) bool {
	if left == nil || right == nil {
		return false
	}
	if types.IsNumeric(left) || isRuneType(left) || types.IsNumeric(right) || isRuneType(right) {
		return false
	}
	if left.String() != right.String() {
		return false
	}
	matches := tc.canonicalTraitMatches(left, "compare", ordTraitName)
	if len(matches) != 1 {
		return false
	}
	tc.methodTable.SetOperatorResolution(expr, typetable.Resolution{
		Impl:      matches[0].Impl,
		Method:    matches[0].Method,
		Signature: matches[0].Signature,
		Bindings:  matches[0].Bindings,
	})
	return true
}

// eqTraitName is the prelude trait that *overrides* structural equality.
const eqTraitName = "Eq"

// dispatchEq accepts an `==`/`!=` whose operands have an `Eq` impl, recording the
// resolution for the backend. Reports whether it handled the comparison.
//
// **The impl overrides rather than enables.** Equality is structural by default and
// works on every type, including a bare type variable — so unlike `Ord`, which is the
// only way to order a user type, this exists for the minority whose equality is not
// field-wise. That is why the check sits in front of the structural rule rather than
// behind it as a fallback: where an impl exists it is the answer, and the structural
// rule has nothing to say.
//
// Primitives are never routed here, so `1 == 1` stays a machine comparison and an impl
// cannot change what equality means on the built-in types — the same rule dispatchOrdCompare
// follows, and for the same reason.
func (tc *TypeChecker) dispatchEq(expr *ast.BooleanBinaryOpExpr, left, right types.Type) bool {
	if left == nil || right == nil {
		return false
	}
	if types.IsNumeric(left) || isRuneType(left) || types.IsString(left) || types.IsBoolean(left) {
		return false
	}
	if left.String() != right.String() {
		return false
	}
	// Inside a generic body the operand is a type *variable*, which names no impl. The
	// specialization will fix it, so publish a candidate per implementing type and let
	// the backend pick — the arrangement publishBoundCandidates uses for a bound call.
	// Without this an `Eq` impl applied outside a generic and not inside one.
	if _, isVar := left.(types.GenericType); isVar {
		eqTrait := eqTraitName
		if d := tc.canonicalTraitDecl(eqTraitName); d != nil {
			eqTrait = d.Name
		}
		tc.publishOperatorCandidates(expr, eqTrait, "eq")
		return false
	}
	matches := tc.canonicalTraitMatches(left, "eq", eqTraitName)
	if len(matches) != 1 {
		return false
	}
	tc.methodTable.SetOperatorResolution(expr, typetable.Resolution{
		Impl:      matches[0].Impl,
		Method:    matches[0].Method,
		Signature: matches[0].Signature,
		Bindings:  matches[0].Bindings,
	})
	return true
}

// publishOperatorCandidates records the impl an operator would dispatch to for each
// type implementing the trait, for an operand that is still a type variable here.
//
// It returns nothing and the caller falls through to the structural rule: at check
// time a type variable *is* structurally comparable, and the specialization is where
// the impl becomes findable. The backend consults the candidates first.
func (tc *TypeChecker) publishOperatorCandidates(expr ast.Expression, traitName, methodName string) {
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
			continue
		}
		byType[target.String()] = typetable.Resolution{
			Impl: matches[0].Impl, Method: matches[0].Method,
			Signature: matches[0].Signature, Bindings: matches[0].Bindings,
		}
	}
	tc.methodTable.SetOperatorCandidates(expr, byType)
}

// warnFloatEqualityAtInstantiation reports `lyra-W008` for a `==`/`!=` inside a generic
// body whose operands *this instantiation* binds to a float.
//
// The warning already fires on a direct `a == b` between floats. It did not survive
// substitution: `let same<t> = (a: t, b: t) -> bool => a == b` called at `f64` does the
// same IEEE comparison and answered `false` for `same(0.1 + 0.2, 0.3)` with nothing
// said. Genericity silently stripped the safety net, which is worse than never having
// had one — the check looks present and is not.
//
// Reported at the **call**, not at the comparison. The comparison is correct where it
// is written: `t` is not a float there, and the same body is perfectly sensible at every
// other type. What deserves the warning is *this* instantiation, and the call is the
// line the author can change.
func (tc *TypeChecker) warnFloatEqualityAtInstantiation(calleeName string, lambda *ast.LambdaExpr, call *ast.FunctionCallExpr, subst map[string]types.Type) {
	if lambda == nil || lambda.Body == nil || len(subst) == 0 {
		return
	}
	// At most one per call: a body comparing several float-bound variables says the same
	// thing about the same call site, and repeating it is noise.
	reported := false
	ast.WalkExpr(lambda.Body, nil, func(e ast.Expression) bool {
		if reported {
			return false
		}
		cmp, ok := e.(*ast.BooleanBinaryOpExpr)
		if !ok || (cmp.Operator != ast.BooleanBinaryOpEq && cmp.Operator != ast.BooleanBinaryOpNEq) {
			return true
		}
		for _, side := range []ast.Expression{cmp.Left, cmp.Right} {
			t, found := tc.typeTable.Get(side)
			if !found {
				continue
			}
			g, isVar := t.(types.GenericType)
			if !isVar {
				continue
			}
			if bound, ok := subst[g.Name]; ok && isFloatType(bound) {
				tc.addErrorCode(call.GetLocation(), SeverityWarning, diag.CodeImpreciseFloatEquality,
					"%s: %s is %s here, and its body compares values of that type with %s — "+
						"comparing floats this way may give unexpected results due to floating-point precision",
					calleeName, g.Name, bound, cmp.Operator)
				reported = true
				return false
			}
		}
		return true
	})
}

// shadowedOrdHint adds a clause to the "must implement Ord" message when the program
// declares its **own** trait named `Ord`, shadowing the canonical one.
//
// Without it that message reads as a contradiction: the author wrote `impl Ord for Ver`
// and is told Ver must implement Ord. The declaration is the answer, and the
// `ShadowedCanonical` stamp the collector already leaves is what lets the diagnostic
// say so — the same trick `?` uses for a shadowed `Maybe`, and the same reason: a rule
// that is right can still produce a message that names the answer as the problem.
func (tc *TypeChecker) shadowedOrdHint() string {
	if tc.symTable == nil {
		return ""
	}
	for _, decl := range tc.symTable.Traits {
		if decl != nil && decl.ShadowedCanonical == ordTraitName {
			return " (this program declares its own `trait Ord`, which is an ordinary trait — " +
				"the one comparison dispatches through is the prelude's `@builtin(Ord)`)"
		}
	}
	return ""
}
