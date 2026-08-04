package typechecker

import (
	"sort"
	"strings"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// Receiver-keyed overloading — resolution.
//
// A name declared more than once in a module (each declaration taking a `self` receiver
// of a different type head) resolves at the call site, against the type of the receiver.
// **There is one such site**, because UFCS desugars `m.f(x)` into `f(m, x)` before
// anything else runs: whichever way the call was written, by the time a member is chosen
// the receiver is argument 0. So `receiverAccepts` below is asked from exactly two
// places — the bare-call path and the UFCS rung that decides whether `m.f` is a method
// call at all — and they must agree, or a call would resolve differently depending on how
// it was spelled.
//
// The predicate is the same one trait dispatch uses to match an impl against a receiver
// (`unifyGenericTarget`): the function's own type variables act as wildcards binding to
// the receiver's subterms, everything else must match structurally. Sharing it is what
// makes `map` on a `Maybe<i64>` pick the `Maybe` overload for the same reason
// `impl Show for Maybe<t>` matches one.
//
// **Overlap is refused at the declaration, not here** (ast.OverloadableWith): two members
// with the same receiver head could both match, and ranking them needs a specificity
// ordering the language does not have. That is why the ambiguity branch below reports a
// compiler-side inconsistency rather than asking the user to disambiguate — reaching it
// means a set was admitted that should not have been.

// receiverAccepts reports whether fn's `self` parameter admits a receiver of recvType.
func receiverAccepts(fn *ast.LambdaExpr, recvType types.Type) bool {
	recv, ok := ast.ReceiverParam(fn)
	if !ok || recvType == nil {
		return false
	}
	return unifyGenericTarget(recv.Type, recvType, lambdaTypeVars(fn), map[string]types.Type{})
}

// resolveOverload picks the member of set that accepts a receiver of recvType.
//
// A miss returns nil and leaves the diagnostic to the caller, which knows whether the
// call was written as a method or as a plain call and can therefore phrase it in the
// reader's own terms.
func (tc *TypeChecker) resolveOverload(set *ast.OverloadSet, recvType types.Type) *ast.LambdaExpr {
	if set == nil {
		return nil
	}
	var match *ast.LambdaExpr
	for _, member := range set.Members {
		lam, ok := member.Value.(*ast.LambdaExpr)
		if !ok || !receiverAccepts(lam, recvType) {
			continue
		}
		if match != nil {
			// Unreachable for a set that passed registration; see the header.
			tc.addError(member.GetLocation(), SeverityError,
				"internal: %q has two overloads accepting a %s receiver", set.Name, recvType)
			return nil
		}
		match = lam
	}
	return match
}

// overloadReceiverTypes renders a set's receiver types for a diagnostic, sorted so the
// message does not depend on declaration order in a way a reader would have to know.
func overloadReceiverTypes(set *ast.OverloadSet) string {
	seen := make([]string, 0, len(set.Members))
	for _, member := range set.Members {
		lam, ok := member.Value.(*ast.LambdaExpr)
		if !ok {
			continue
		}
		if recv, ok := ast.ReceiverParam(lam); ok && recv.Type != nil {
			seen = append(seen, recv.Type.String())
		}
	}
	sort.Strings(seen)
	return strings.Join(seen, ", ")
}

// inferOverloadedCall type-checks a call whose callee names an overload set: the member
// is chosen by the first argument's type, then the call is checked against that member
// exactly as a call to a singly-declared function is.
//
// The receiver is argument 0 whether or not the call was written method-style, so this
// runs after the UFCS desugar and needs no notion of it.
func (tc *TypeChecker) inferOverloadedCall(set *ast.OverloadSet, call *ast.FunctionCallExpr) types.Type {
	if len(call.Arguments) == 0 {
		tc.addError(call.GetLocation(), SeverityError,
			"%s is overloaded on its receiver (%s), so it needs one to pick between them — call it as `receiver.%s(…)` or pass the receiver as the first argument",
			set.Name, overloadReceiverTypes(set), set.Name)
		return nil
	}
	recvType := tc.inferExprType(call.Arguments[0])
	if recvType == nil {
		return nil
	}
	recvType = tc.resolveType(recvType, call.Arguments[0].GetLocation())
	member := tc.resolveOverload(set, recvType)
	if member == nil {
		tc.addError(call.Arguments[0].GetLocation(), SeverityError,
			"no overload of %s takes a %s receiver; it is overloaded on %s",
			set.Name, recvType, overloadReceiverTypes(set))
		return nil
	}
	tc.noteResolvedCallee(call, member)
	return tc.inferLambdaCall(set.Name, member, call)
}

// noteResolvedCallee records which declaration a call resolved to, for the passes that
// run after this one.
//
// Ownership and use-after-move both re-resolve a callee by name in order to read its
// parameter modes, and an overloaded name cannot be resolved that way — the name alone
// does not say which member. Recording the answer here is the same move the MethodTable
// makes for trait dispatch: the pass that *did* the resolution publishes it, rather than
// three passes each re-deriving it and drifting.
func (tc *TypeChecker) noteResolvedCallee(call *ast.FunctionCallExpr, fn *ast.LambdaExpr) {
	if tc.typeTable == nil || call == nil || fn == nil {
		return
	}
	tc.typeTable.SetCallee(call, fn)
}
