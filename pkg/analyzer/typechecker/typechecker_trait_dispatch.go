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
// or `TraitName::name()` syntax, so they're never a candidate here). Generic
// impls (`impl<T> Show for Box<T>`) are not matched: types.TypesEqual
// correctly returns false against a concrete receiver since the impl's
// GenericType type argument can never equal a concrete one — resolving a
// call against a generic impl is a separate, larger feature.
func (tc *TypeChecker) resolveTraitMethod(receiverType types.Type, methodName string, requiredTrait string) []resolvedTraitMethod {
	var matches []resolvedTraitMethod
	for _, impl := range tc.traitImpls {
		if requiredTrait != "" && impl.TraitName != requiredTrait {
			continue
		}
		implType := tc.resolveTypeIfKnown(impl.Type)
		if !types.TypesEqual(implType, receiverType) {
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
				sig = substituteSelf(traitMethod.Signature, impl.Type)
			}
			matches = append(matches, resolvedTraitMethod{Impl: impl, Method: m, Signature: sig})
		}
	}
	return matches
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
