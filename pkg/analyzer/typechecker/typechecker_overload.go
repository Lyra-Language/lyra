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

// receiverAcceptsValue is receiverAccepts plus the one allowance that depends on **what
// the receiver expression is** rather than on what its type is: an array literal is
// *built* in the shape its context asks for, so `[1, 2, 3]` satisfies a `[]t` receiver
// exactly as it already satisfies a `[]t` parameter.
//
// It closes a gap between the two spellings of one call, which the note above says must
// not exist. `map([1, 2, 3], f)` has always worked — the receiver match fails, the bare
// path keeps the resolved declaration, and the *argument* rung then admits the literal
// through `arrayLiteralAsDeclared`. The UFCS rung has no such second chance: it gathers
// candidates by receiver and a miss is the end, so `[1, 2, 3].map(f)` was refused. Same
// call, same receiver, two answers — decided by which spelling was used.
//
// **This is not the auto-widening the rule forbids.** A `[N]T` is a stack value and a
// `[]T` is a heap box, so widening an existing *value* at a call would allocate where
// nothing asked it to, invisibly to `noalloc`. A literal has no prior shape to widen: it
// is constructed here, in the shape its context asks for, which is the same rule that
// makes `let xs: []i64 = [1, 2, 3]` a heap array. The allocation stays visible because
// the literal's recorded type becomes the `[]T` it was built as, which is exactly what
// `noalloc` reads.
//
// The one answer is shared with the argument rung (arrayLiteralAsDeclared) rather than
// re-derived, so the two positions cannot come to disagree about what a literal fits.
func receiverAcceptsValue(fn *ast.LambdaExpr, recvExpr ast.Expression, recvType types.Type) bool {
	if receiverAccepts(fn, recvType) {
		return true
	}
	recv, ok := ast.ReceiverParam(fn)
	if !ok || recvExpr == nil || recvType == nil {
		return false
	}
	return receiverAccepts(fn, arrayLiteralAsDeclared(recvExpr, recv.Type, promoteToDefault(recvType)))
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
		// The scope chain settled on *this* set, and nothing in it takes this receiver.
		// Another reachable module may still declare the name for it — see
		// receiverFallback for why that is not the scope chain's job to find.
		if fn, ok := tc.receiverFallback(set.Name, recvType, call); ok {
			tc.noteResolvedCallee(call, fn)
			return tc.inferLambdaCall(set.Name, fn, call)
		}
		tc.addError(call.Arguments[0].GetLocation(), SeverityError,
			"no overload of %s takes a %s receiver; it is overloaded on %s",
			set.Name, recvType, overloadReceiverTypes(set))
		return nil
	}
	tc.noteResolvedCallee(call, member)
	return tc.inferLambdaCall(set.Name, member, call)
}

// receiverFallback finds a reachable declaration of name whose `self` parameter accepts
// recvType, for a **bare call** whose name the scope chain resolved to something that does
// not.
//
// This is the call-form half of what `ufcsFunction` does for `m.f(x)`. A method call has
// never gone through the scope chain — it gathers every reachable declaration and picks by
// receiver — while a bare call resolves a *name*, module → prelude → global, and stops at
// the first hit. So the two spellings disagreed: with `map` for `Box` in an imported module
// and `map` in the prelude, `b.map(f)` resolved and `map(b, f)` reported "no overload of
// map takes a Box receiver", because the prelude's scope sits nearer than the global one
// the import exports into. Same call, same receiver, two answers.
//
// **It runs only after the scope chain has failed**, which is what keeps it additive: a
// name that resolves to something accepting the receiver is untouched, so a local
// declaration still wins and nothing that compiles today changes meaning. Only the case
// that was an error becomes a resolution.
//
// Reachability and ambiguity are the method form's rules exactly (`ufcsImportedIn`, and a
// surviving tie is reported rather than broken), because they are the same question — this
// is not a second dispatch mechanism, it is the same one reached from the other spelling.
func (tc *TypeChecker) receiverFallback(name string, recvType types.Type, call *ast.FunctionCallExpr) (*ast.LambdaExpr, bool) {
	loc := call.GetLocation()
	var matches []*ast.LambdaExpr
	for _, fn := range tc.symTable.FunctionsNamed(name) {
		if _, isReceiver := ast.ReceiverParam(fn); !isReceiver {
			continue
		}
		if !tc.ufcsImported(fn, loc) || !receiverAccepts(fn, recvType) {
			continue
		}
		matches = append(matches, fn)
	}
	switch len(matches) {
	case 0:
		return nil, false
	case 1:
		tc.noteUFCSModule(matches[0], loc)
		return matches[0], true
	}
	if local := tc.localCandidate(matches, loc); local != nil {
		return local, true
	}
	tc.addError(loc, SeverityError,
		"%s is ambiguous for a %s receiver: %s each define one — name the one you mean through its module",
		name, recvType, tc.modulesOf(matches))
	return nil, false
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

// bareCalleeFor returns the declaration a bare call should use: the one the scope chain
// resolved, unless that one takes a `self` receiver it does not accept while another
// reachable declaration of the name does.
//
// The single-declaration counterpart of the fallback in inferOverloadedCall, and the same
// asymmetry: with `is_some` for `Box` in an imported module, `b.is_some()` resolved and
// `is_some(b)` reported *"cannot infer type variable t from these arguments"* — the
// prelude's `is_some<t>(self: Maybe<t>)` being what the scope chain reached first, and the
// message describing a unification failure rather than the resolution that went wrong.
//
// Only a **receiver** function gives way. A plain function whose first argument does not
// fit is an ordinary argument-type error and must stay one; dispatching there would turn a
// typo into a call to something else entirely.
func (tc *TypeChecker) bareCalleeFor(name string, resolved *ast.LambdaExpr, call *ast.FunctionCallExpr) *ast.LambdaExpr {
	if _, isReceiver := ast.ReceiverParam(resolved); !isReceiver || len(call.Arguments) == 0 {
		return resolved
	}
	argType := tc.inferExprType(call.Arguments[0])
	if argType == nil {
		return resolved
	}
	argType = tc.resolveType(argType, call.Arguments[0].GetLocation())
	if receiverAccepts(resolved, argType) {
		return resolved
	}
	if fn, ok := tc.receiverFallback(name, argType, call); ok {
		tc.noteResolvedCallee(call, fn)
		return fn
	}
	// No reachable alternative: keep the resolved one so the error is about this call's
	// arguments, which is what it is.
	return resolved
}
