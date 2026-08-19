package llvm

import (
	"fmt"
	"sort"

	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/constant"
	"github.com/llir/llvm/ir/enum"
	lltypes "github.com/llir/llvm/ir/types"
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
func (l *lowerer) lowerTraitMethodCall(block *ir.Block, call *ast.FunctionCallExpr, member *ast.MemberExpr, fn *ir.Func, res typetable.Resolution) (value.Value, *ir.Block, error) {
	// The receiver is signature parameter 0 and each argument i is parameter i+1. Both go
	// by pointer when their parameter is a `mut`/`ref` borrow — the same convention
	// Resolution.Lambda binds the body against, and the two must agree: a body expecting a
	// pointer handed a value (or the reverse) is not a type error the front end can catch,
	// it is a wild load.
	params := l.methodParams(call, res)
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

// methodParams returns the trait signature's parameters for a trait-method call — the
// borrow modes its operands must be passed under.
//
// **The resolution is taken from the caller first, and the table only as a fallback**,
// because a **bound-dispatched** call has no entry in the resolution table: it was
// resolved abstractly at check time and its concrete impl comes from the candidate table
// at lowering, which only the caller has in hand. Reading the table alone returned nil
// modes there, so every operand went by value — and a `mut` receiver, whose emitted
// function takes a pointer, was handed a struct. That is not a diagnosable mismatch, it
// is a wild load: `v.bump(n)` under `where t: Bump` segfaulted, and so did the identical
// call inside a trait default.
//
// Both call forms are resolved, and their operands line up differently: for a `.`-call the
// receiver is parameter 0 and argument i is parameter i+1, while a fully-qualified
// `Trait::method(x, …)` passes the receiver *as* argument 0, so index and position agree.
// The offset is the caller's to apply — see methodOperand.
func (l *lowerer) methodParams(call *ast.FunctionCallExpr, res typetable.Resolution) []types.ParameterType {
	if res.Signature != nil {
		return res.Signature.Parameters
	}
	return l.methodParamModes(call)
}

// methodParamModes returns the parameters recorded for a call in the resolution table, or
// nil for a call that has none. See methodParams, which prefers a resolution in hand.
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
	key, ok := l.candidateKey(member.Object)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: no type recorded for the receiver of %s::%s",
			ref.Trait, ref.Method)
	}
	res, ok := l.res.MethodTable.BoundCandidate(call, key)
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
	return l.lowerTraitMethodCall(block, call, member, fn, res)
}

// lowerOrdComparison lowers a comparison operator that dispatches to a trait impl —
// `Ord::compare` for the ordering operators, `Eq::eq` for `==`/`!=`.
//
// `<=>` *is* that Ordering, so it is the call and nothing more. The relational
// operators are derived from it rather than dispatched separately — which is the
// whole reason `Ord` has one method: an impl cannot make `<` and `<=>` disagree,
// the failure mode C++'s separate `operator<`/`operator<=>` and Java's
// `compareTo`-beside-`equals` both carry.
//
// The Ordering is a tagged union whose discriminant is its first field, and the
// prelude declares `Less | Equal | Greater` in that order — so the tag is 0/1/2 and
// each operator is one `icmp` against it. Reading the tag rather than pattern-matching
// keeps this branchless, for the reason `<=>`'s primitive lowering is: a branching
// comparison returns a merge block, which the temp-release machinery does not handle.
func (l *lowerer) lowerOrdComparison(block *ir.Block, e *ast.BooleanBinaryOpExpr, res typetable.Resolution, left, right value.Value) (value.Value, *ir.Block, error) {
	fn, err := l.traitMethod(res)
	if err != nil {
		return nil, nil, err
	}
	// `==`/`!=` dispatch to an `Eq` impl, whose method already answers a bool: `==` is
	// the call, `!=` its negation. Handled here rather than in a second lowering
	// because the shape is identical — call the impl, read the answer — and the only
	// difference is which trait was resolved.
	if e.Operator == ast.BooleanBinaryOpEq || e.Operator == ast.BooleanBinaryOpNEq {
		eq := block.NewCall(fn, left, right)
		if e.Operator == ast.BooleanBinaryOpEq {
			return eq, block, nil
		}
		return block.NewXor(eq, constant.NewInt(lltypes.I1, 1)), block, nil
	}
	ord := block.NewCall(fn, left, right)
	if e.Operator == ast.BooleanBinaryOpSpaceship {
		return ord, block, nil
	}
	// The variant tags are looked up by *name*, not assumed to be 0/1/2. The prelude
	// happens to declare `Less | Equal | Greater` in that order, but hardcoding it
	// would make reordering that declaration silently invert every comparison in every
	// program — a wrong answer, not a build failure.
	ordTy, err := l.ordDataType(res)
	if err != nil {
		return nil, nil, err
	}
	tagOf := func(name string) (*constant.Int, error) {
		_, idx, ok := findConstructor(ordTy, name)
		if !ok {
			return nil, fmt.Errorf("llvm: the Ordering returned by Ord::compare has no %q variant", name)
		}
		return constant.NewInt(lltypes.I8, int64(idx)), nil
	}
	less, err := tagOf("Less")
	if err != nil {
		return nil, nil, err
	}
	greater, err := tagOf("Greater")
	if err != nil {
		return nil, nil, err
	}
	tag := block.NewExtractValue(ord, 0)
	switch e.Operator {
	case ast.BooleanBinaryOpLT:
		return block.NewICmp(enum.IPredEQ, tag, less), block, nil
	case ast.BooleanBinaryOpGT:
		return block.NewICmp(enum.IPredEQ, tag, greater), block, nil
	case ast.BooleanBinaryOpLTE:
		return block.NewICmp(enum.IPredNE, tag, greater), block, nil
	case ast.BooleanBinaryOpGTE:
		return block.NewICmp(enum.IPredNE, tag, less), block, nil
	}
	// Every operator that can carry a resolution is handled above. Reaching this is a
	// resolution recorded for one that cannot — rule 5, not a guess.
	return nil, nil, fmt.Errorf("llvm: operator %s has a trait resolution but no lowering", e.Operator)
}

// ordDataType is the `Ordering` data type an `Ord::compare` returns, taken from the
// impl's own resolved signature rather than looked up by name — the impl is what the
// call actually runs, so its return type is the union whose tags are being read.
func (l *lowerer) ordDataType(res typetable.Resolution) (types.DataType, error) {
	if res.Signature == nil {
		return types.DataType{}, fmt.Errorf("llvm: Ord::compare has no resolved signature")
	}
	ret := l.resolveNamedType(res.Signature.ReturnType.Type)
	dt, ok := ret.(types.DataType)
	if !ok {
		return types.DataType{}, fmt.Errorf(
			"llvm: Ord::compare must return the prelude's Ordering, got %s", res.Signature.ReturnType.Type)
	}
	return dt, nil
}

// lowerOperatorImplCall emits the call an overloaded arithmetic or bitwise operator
// dispatches to: `a + b` becomes the impl's `(_+_)` with the operands as arguments,
// receiver first.
//
// It is this short because an operator method *is* an ordinary trait method — same
// emitted function, same argument order, same specialization key. The only thing the
// operator adds is that the resolution arrives through the TypeTable rather than
// through a call node, which is what `SetOperatorResolution` exists for.
//
// Unlike `lowerOrdComparison` there is nothing to interpret afterwards: `Ord` returns
// an `Ordering` that four of the five operators have to read a tag out of, whereas an
// arithmetic impl's return value *is* the expression's value, whatever its type.
func (l *lowerer) lowerOperatorImplCall(block *ir.Block, res typetable.Resolution, operands ...value.Value) (value.Value, *ir.Block, error) {
	fn, err := l.traitMethod(res)
	if err != nil {
		return nil, nil, err
	}
	args := make([]value.Value, len(operands))
	copy(args, operands)
	return block.NewCall(fn, args...), block, nil
}

// operatorCandidate finds the impl an operator dispatches to when the typechecker could
// not name one — the receiver was a type *variable* there, and only this specialization
// fixes it.
//
// The key is the receiver's **substituted** type, in the typechecker's own spelling —
// candidateKey, not recordedType; see its comment. It is a separate step from
// `OperatorResolution` rather than a fallback inside it, because the two answer different
// questions: one is "which impl did the checker pick", the other "which impl does this
// instantiation name".
func (l *lowerer) operatorCandidate(expr ast.Expression, receiver ast.Expression) (typetable.Resolution, bool) {
	key, ok := l.candidateKey(receiver)
	if !ok {
		return typetable.Resolution{}, false
	}
	return l.res.MethodTable.OperatorCandidate(expr, key)
}

// lowerTraitPathCall lowers the fully-qualified call form, `Trait::method(receiver, …)`.
//
// It type-checked and then died in the backend as `no type recorded for the callee of an
// indirect call`, because lowerFunctionCallExpr knows a `MemberExpr` callee and an
// identifier and nothing else, so a TraitMethodPathExpr fell through to the
// function-value path — where the callee is a *name of a trait method*, which is not a
// value and has no recorded type. That made the compiler's own advice unbuildable: the
// ambiguity diagnostic for a name two traits provide says "use TraitName::method(...) to
// disambiguate".
//
// Everything it needs is already published — dispatch records the full Resolution for
// this call exactly as it does for a `.`-call. The one difference is the operand layout:
// the receiver is argument 0 here rather than a separate expression, so arguments and
// signature parameters are index-aligned and the loop needs no offset.
func (l *lowerer) lowerTraitPathCall(block *ir.Block, call *ast.FunctionCallExpr, path *ast.TraitMethodPathExpr) (value.Value, *ir.Block, error) {
	res, ok := l.res.MethodTable.GetResolution(call)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: no resolution recorded for %s::%s",
			path.TraitName, path.Method.Name)
	}
	fn, err := l.traitMethod(res)
	if err != nil {
		return nil, nil, err
	}
	params := l.methodParams(call, res)
	args := make([]value.Value, 0, len(call.Arguments))
	for i, arg := range call.Arguments {
		v, next, err := l.methodOperand(block, arg, params, i)
		if err != nil {
			return nil, nil, err
		}
		block = next
		args = append(args, v)
	}
	if len(args) != len(fn.Params) {
		return nil, nil, fmt.Errorf("llvm: %s::%s expects %d argument(s) including the receiver, got %d",
			path.TraitName, path.Method.Name, len(fn.Params), len(args))
	}
	return block.NewCall(fn, args...), block, nil
}
