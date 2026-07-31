package llvm

import (
	"fmt"

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
// trait, and the method. All three, because neither pair alone is unique — one type may
// implement two traits that both declare `show`, and one trait may be implemented by
// many types.
func traitMethodSymbol(implType types.Type, traitName, method string) string {
	return typetable.TypeSymbol(implType.GetName()+"$"+traitName, nil) + "$" + method
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
	name := traitMethodSymbol(res.Impl.Type, res.Impl.TraitName, res.Method.GetName())
	if fn, ok := l.traitMethods[name]; ok {
		return fn, nil
	}

	lambda, err := traitMethodLambda(res)
	if err != nil {
		return nil, err
	}
	fn, err := l.declareFunctionAs(name, lambda)
	if err != nil {
		return nil, err
	}
	// Cached before the body is lowered, so a method that calls itself finds the
	// declaration instead of recursing into this again.
	l.traitMethods[name] = fn
	l.pendingTraitMethods = append(l.pendingTraitMethods, pendingTraitMethod{fn: fn, lambda: lambda})
	return fn, nil
}

// pendingTraitMethod is a declared-but-not-yet-defined impl method. Bodies are lowered
// after the function currently being emitted finishes, never re-entrantly in the middle
// of an expression — the lowerer carries per-function state (locals, loops, the managed
// frame stack) that lowering a second body underneath would corrupt. Closures are lifted
// for the same reason.
type pendingTraitMethod struct {
	fn     *ir.Func
	lambda *ast.LambdaExpr
}

// defineePendingTraitMethods lowers the bodies queued during this module's emission,
// looping because defining one may itself queue another.
func (l *lowerer) definePendingTraitMethods() error {
	for len(l.pendingTraitMethods) > 0 {
		pending := l.pendingTraitMethods
		l.pendingTraitMethods = nil
		for _, p := range pending {
			if err := l.defineFunctionInto(p.fn, p.lambda, p.fn.GlobalIdent.Ident()); err != nil {
				return err
			}
		}
	}
	return nil
}

// traitMethodLambda synthesizes the function an impl method *is*: the trait's signature
// supplies the types, the impl's clause supplies the parameter names and the body.
//
// Building a LambdaExpr rather than a bespoke lowering path is deliberate — it means
// parameter binding, `own`-parameter framing, and the void/typed return split all come
// from defineFunctionInto, the same code a plain function and a generic specialization
// go through, and cannot drift between them.
//
// The receiver is simply the first parameter. `self` has no special status at run time;
// what makes it the receiver is that dispatch put the receiver's type in Signature's
// first position, which is exactly where the call site passes it.
func traitMethodLambda(res typetable.Resolution) (*ast.LambdaExpr, error) {
	clause := res.Method.Clause
	sig := res.Signature
	if len(clause.Patterns) != len(sig.Parameters) {
		return nil, fmt.Errorf(
			"llvm: trait method %q takes %d parameter(s) but its impl binds %d",
			res.Method.GetName(), len(sig.Parameters), len(clause.Patterns))
	}
	params := make([]ast.Parameter, len(clause.Patterns))
	for i, pat := range clause.Patterns {
		// The signature's borrow modifier travels with the parameter. This is the line
		// the old comment here warned about: carry it, or the call site and the body
		// disagree about who owns the receiver. Everything downstream — paramIsByRef,
		// the owning-binding frame in defineFunctionInto — reads ast.Parameter, so
		// carrying it here is what makes a `ref Self` a pointer on both sides.
		params[i] = ast.Parameter{
			AstBase:      ast.AstBase{Location: clause.GetLocation()},
			Pattern:      pat,
			Type:         sig.Parameters[i].Type,
			TypeModifier: sig.Parameters[i].Borrow,
		}
	}
	return &ast.LambdaExpr{
		ExprBase:   ast.ExprBase{AstBase: ast.AstBase{Location: clause.GetLocation()}},
		Parameters: params,
		ReturnType: sig.ReturnType,
		Body:       clause.Body,
	}, nil
}

// lowerTraitMethodCall emits the call, passing the receiver as the first argument.
//
// A `.`-call writes the receiver as the object rather than as an argument, so it is
// prepended here; a fully-qualified `Trait::method(x)` already has it in the argument
// list and does not reach this path.
func (l *lowerer) lowerTraitMethodCall(block *ir.Block, call *ast.FunctionCallExpr, member *ast.MemberExpr, fn *ir.Func) (value.Value, *ir.Block, error) {
	// The receiver is signature parameter 0 and each argument i is parameter i+1. Both go
	// by pointer when their parameter is a `mut`/`ref` borrow — the same convention
	// traitMethodLambda binds the body against, and the two must agree: a body expecting a
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
