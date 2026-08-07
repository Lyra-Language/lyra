package llvm

import (
	"fmt"
	"sort"

	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/value"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
	"github.com/Lyra-Language/lyra/pkg/typetable"
)

// Trait-impl methods lower to ordinary functions taking the receiver first, and a
// method call lowers to a direct call to one. Dispatch is entirely static: the
// typechecker already decided which impl a call resolves to, so there are no vtables and
// nothing is looked up at run time.
//
// They are emitted **lazily, at the first call** — the same shape as generic types
// (generic_types.go) and for the same two reasons. A method that is never called costs
// nothing, and, more importantly, the *call site* is where the receiver-substituted
// signature is available: `impl Show<t> for Box<t>` has no single type until a receiver
// picks one, and dispatch has already worked that out. Walking the impls up front would
// mean re-deriving Self substitution in the backend — a second implementation of "what
// is this method's type", free to disagree with the one that type-checked the call.
//
// The declare-then-define split matters here too: a method that calls itself re-enters
// this and must find the declaration rather than recursing forever.

// traitMethodSymbol is the emitted name for one impl method: the implementing type, the
// trait, the method, and — for a generic impl — the bindings this specialization was
// emitted at.
//
// All four, because no subset is unique. One type may implement two traits that both
// declare `show`, one trait may be implemented by many types, and **one generic impl
// serves many receivers**. That last one was missing until 08/03, and it was not a
// cosmetic collision: `impl Sized for Box<t>` emitted a single
// `@Box$Sized$size(%Box$i64)` that both `Box<i64>` and `Box<bool>` call sites called, so
// the second passed a `%Box$boolean` into an i64-shaped parameter. Apple clang accepts
// that — opaque pointers make the two function types indistinguishable — which is what
// let it stand as a silent miscompile rather than a build failure.
func traitMethodSymbol(res typetable.Resolution) string {
	base := typetable.TypeSymbol(res.Impl.Type.GetName()+"$"+res.Impl.TraitName, nil) + "$" + res.Method.GetName()
	if len(res.Bindings) == 0 {
		return base
	}
	names := make([]string, 0, len(res.Bindings))
	for n := range res.Bindings {
		names = append(names, n)
	}
	sort.Strings(names)
	args := make([]types.Type, 0, len(names))
	for _, n := range names {
		args = append(args, res.Bindings[n])
	}
	// Mangled through the same helper a generic function's symbol uses, so the two
	// families of specialization read alike in the IR (`Box$Sized$size$i64`).
	return typetable.TypeSymbol(base, args)
}

// traitMethodCallee returns the emitted function for a resolved method call, emitting it
// on first use. ok is false when the call is not a resolved trait-method call at all, so
// the caller can go on to try the builtin methods.
func (l *lowerer) traitMethodCallee(call *ast.FunctionCallExpr) (*ir.Func, bool, error) {
	res, ok := l.res.MethodTable.GetResolution(call)
	if !ok || res.Method == nil || res.Impl == nil {
		return nil, false, nil
	}
	if res.Signature == nil {
		return nil, true, fmt.Errorf(
			"llvm: trait method %q has no resolved signature — a trait method must declare one to be lowered",
			res.Method.GetName())
	}
	fn, err := l.traitMethod(res)
	return fn, true, err
}

// traitMethod declares and defines one impl method, caching it by symbol so two call
// sites reaching the same method share one emitted function.
func (l *lowerer) traitMethod(res typetable.Resolution) (*ir.Func, error) {
	name := traitMethodSymbol(res)
	if fn, ok := l.traitMethods[name]; ok {
		return fn, nil
	}

	lambda, err := res.Lambda()
	if err != nil {
		return nil, fmt.Errorf("llvm: %w", err)
	}
	// The signature is declared under this specialization's bindings. Dispatch has
	// already substituted Self and the trait's own parameters, so the signature is
	// usually concrete before it gets here — but an impl whose *method* mentions the
	// impl's variable elsewhere still needs them, and installing the substitution costs
	// nothing when it is empty.
	restore := l.pushTypeSubst(res.Bindings)
	fn, err := l.declareFunctionAs(name, lambda)
	restore()
	if err != nil {
		return nil, err
	}
	// Cached before the body is lowered, so a method that calls itself finds the
	// declaration instead of recursing into this again.
	l.traitMethods[name] = fn
	// The bindings travel with the queued body. Bodies are lowered after the current
	// function finishes (see below), by which point a substitution pushed here is long
	// gone — and lowering a generic body without one is exactly the `match on Maybe<t>`
	// failure this whole path existed to hit.
	l.pendingTraitMethods = append(l.pendingTraitMethods, pendingTraitMethod{
		fn:      fn,
		lambda:  lambda,
		subst:   res.Bindings,
		specKey: res.SpecKey(),
	})
	return fn, nil
}

// pendingTraitMethod is a declared-but-not-yet-defined impl method. Bodies are lowered
// after the function currently being emitted finishes, never re-entrantly in the middle
// of an expression — the lowerer carries per-function state (locals, loops, the managed
// frame stack) that lowering a second body underneath would corrupt. Closures are lifted
// for the same reason.
type pendingTraitMethod struct {
	fn      *ir.Func
	lambda  *ast.LambdaExpr
	subst   map[string]types.Type // this specialization's bindings; empty for a non-generic impl
	specKey string                // typetable.Resolution.SpecKey, for the body's ownership table
}

// defineePendingTraitMethods lowers the bodies queued during this module's emission,
// looping because defining one may itself queue another.
func (l *lowerer) definePendingTraitMethods() error {
	for len(l.pendingTraitMethods) > 0 {
		pending := l.pendingTraitMethods
		l.pendingTraitMethods = nil
		for _, p := range pending {
			// Both halves of the specialization, installed together: the bindings so
			// the body's types are concrete, and the ownership table computed for
			// *this* binding set so its retains and releases are the ones the
			// concrete types call for. Consulting the program-wide table instead is
			// what made `t = string` a double free for generic functions.
			restoreSubst := l.pushTypeSubst(p.subst)
			restoreOwn := l.pushMethodOwnership(p.specKey)
			err := l.defineFunctionInto(p.fn, p.lambda, p.fn.GlobalIdent.Ident())
			restoreOwn()
			restoreSubst()
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// lowerTraitMethodCall emits the call, passing the receiver as the first argument.
//
// A `.`-call writes the receiver as the object rather than as an argument, so it is
// prepended here; a fully-qualified `Trait::method(x)` already has it in the argument
// list and does not reach this path.
func (l *lowerer) lowerTraitMethodCall(block *ir.Block, call *ast.FunctionCallExpr, member *ast.MemberExpr, fn *ir.Func) (value.Value, *ir.Block, error) {
	// The receiver is signature parameter 0 and each argument i is parameter i+1. Both go
	// by pointer when their parameter is a `mut`/`ref` borrow — the same convention
	// Resolution.Lambda binds the body against, and the two must agree: a body expecting a
	// pointer handed a value (or the reverse) is not a type error the front end can catch,
	// it is a wild load.
	params := l.methodParamModes(call)
	recv, block, err := l.methodOperand(block, member.Object, params, 0)
	if err != nil {
		return nil, nil, err
	}
	args := []value.Value{recv}
	for i, arg := range call.Arguments {
		v, next, err := l.methodOperand(block, arg, params, i+1)
		if err != nil {
			return nil, nil, err
		}
		block = next
		args = append(args, v)
	}
	if len(args) != len(fn.Params) {
		return nil, nil, fmt.Errorf("llvm: %s expects %d argument(s) including the receiver, got %d",
			fn.GlobalIdent.Ident(), len(fn.Params), len(args))
	}
	return block.NewCall(fn, args...), block, nil
}

// methodParamModes returns the trait signature's parameters for a resolved `.`-call, or nil
// when there is no resolution (a fully-qualified call, or an unresolved one) — in which case
// every operand goes by value, which is what this path did before signatures carried modes.
func (l *lowerer) methodParamModes(call *ast.FunctionCallExpr) []types.ParameterType {
	res, ok := l.res.MethodTable.GetResolution(call)
	if !ok || res.Signature == nil {
		return nil
	}
	return res.Signature.Parameters
}

// methodOperand lowers the receiver or one argument of a method call, passing its *address*
// when the matching parameter is a by-reference borrow.
//
// It mirrors lowerDirectCall's argument loop rather than sharing it, because that loop walks
// call.Arguments and a method call's receiver is not in there — the offset this function
// takes as `idx` is the whole difference.
func (l *lowerer) methodOperand(block *ir.Block, operand ast.Expression, params []types.ParameterType, idx int) (value.Value, *ir.Block, error) {
	if idx < len(params) && paramIsByRef(ast.Parameter{Type: params[idx].Type, TypeModifier: params[idx].Borrow}) {
		return l.argumentAddress(block, operand)
	}
	return l.lowerExpr(block, operand)
}

// lowerBoundMethodCall lowers a call dispatched through a `where` bound —
// `v.show()` where `v: t` and the enclosing declaration says `where t: Show`.
//
// The typechecker resolved it *abstractly*, to a trait and a method name, because at
// check time the receiver is a type variable and every implementing type answers the
// same. Here it is concrete: a specialization is being lowered, so `l.typeSubst` maps
// `t` to the type this instantiation fixed it at, and the call must name a real
// function. The candidate table (MethodTable.SetBoundCandidates) supplies the
// resolution for that type, so the impl matching stays in the typechecker where
// dispatch already does it.
//
// The receiver's *substituted* type is the key, not the declared one: the declared
// type is the variable, which names no impl.
func (l *lowerer) lowerBoundMethodCall(block *ir.Block, call *ast.FunctionCallExpr, member *ast.MemberExpr, ref typetable.BoundMethodRef) (value.Value, *ir.Block, error) {
	recvT, ok := l.recordedType(member.Object)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: no type recorded for the receiver of %s::%s",
			ref.Trait, ref.Method)
	}
	res, ok := l.res.MethodTable.BoundCandidate(call, recvT.String())
	if !ok {
		// A bound satisfied through the *enclosing* declaration's bounds rather than by
		// an impl — the receiver is still a type variable here because this body is
		// being analyzed generically rather than lowered at an instantiation. Rule 5: a
		// hard error naming what is missing, never a guess at which impl was meant.
		return nil, nil, fmt.Errorf(
			"llvm: cannot lower %s::%s on a receiver of type %s: no impl of %s for it. "+
				"A bound call lowers only where a specialization has fixed the receiver's "+
				"type variable to a concrete type",
			ref.Trait, ref.Method, recvT, ref.Trait)
	}
	fn, err := l.traitMethod(res)
	if err != nil {
		return nil, nil, err
	}
	return l.lowerTraitMethodCall(block, call, member, fn)
}
