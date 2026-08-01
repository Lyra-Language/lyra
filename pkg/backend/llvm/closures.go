package llvm

import (
	"fmt"

	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/constant"
	"github.com/llir/llvm/ir/enum"
	lltypes "github.com/llir/llvm/ir/types"
	"github.com/llir/llvm/ir/value"

	"github.com/Lyra-Language/lyra/pkg/analyzer/captures"
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// Closures, dev tier: a function value is a **boxed closure** — a fat pointer
// `{ i8* fn, i8* env }` pairing the code with its captured environment.
//
// This is the first of the two tiers decided in todo.md: a uniform, stable
// closure ABI for development (which is what makes hot reloading possible — the
// representation of a function value does not depend on which lambdas can flow
// through it), with Lambda Set Specialization as the release-tier lowering later.
// Every function value has this one shape, so a `(i64) -> i64` parameter accepts
// a top-level function, a captureless lambda, and a capturing closure alike,
// with no per-call-site specialization.
//
//   - `fn` is the lifted body, always `ret (i8* env, params...)` — the
//     environment is an implicit leading parameter, so one indirect call shape
//     serves every function value.
//   - `env` points at the *payload* of a ref-counted box `{ i64 rc, envPayload }`,
//     exactly rcHeaderSize past its header — the same relationship a string's
//     data pointer has to its box. That uniformity is the point: managedBox
//     recovers the box by subtracting the header, so retain/release/drop work on
//     a closure with no new machinery.
//   - envPayload is `{ i8* dropFn, captures... }`. The leading slot is what makes
//     one release site able to free any closure's captures: a release knows only
//     the static type `(i64) -> i64`, never which lambda produced the value, so
//     the per-capture-set glue is stored *in* the environment and reached through
//     one generic trampoline (closureEnvDropFn).
//
// A **captureless** closure — a top-level function used as a value, or a lambda
// that reads nothing from its enclosing scope — points at a single pinned static
// environment with a null dropFn. Pinned means retain and release are no-ops on
// it (the same trick string literals use), so a function value costs no
// allocation and still flows through the ownership model unchanged.

// ClosureLLVMType is the representation of any function value: { i8* fn, i8* env }.
func ClosureLLVMType() *lltypes.StructType {
	return lltypes.NewStruct(lltypes.NewPointer(lltypes.I8), lltypes.NewPointer(lltypes.I8))
}

// isClosureLLVMType reports whether t is the closure fat pointer. Both fields are
// i8*, which distinguishes it from a string's { i8*, i64 }.
func isClosureLLVMType(t lltypes.Type) bool {
	st, ok := t.(*lltypes.StructType)
	if !ok || len(st.Fields) != 2 {
		return false
	}
	return isI8Ptr(st.Fields[0]) && isI8Ptr(st.Fields[1])
}

func isI8Ptr(t lltypes.Type) bool {
	p, ok := t.(*lltypes.PointerType)
	return ok && p.ElemType.Equal(lltypes.I8)
}

// closureFnSignature is the LLVM signature of a lifted lambda body: the declared
// return type, then `i8* env` followed by the declared parameters. Shared by the
// definition of a closure body, the thunk that adapts a top-level function, and
// the bitcast an indirect call needs — they must agree exactly or the call is
// undefined behavior.
func (l *lowerer) closureFnSignature(params []types.Type, ret types.Type) (lltypes.Type, []lltypes.Type, error) {
	retTy, err := l.lowerType(ret)
	if err != nil {
		return nil, nil, err
	}
	paramTys := []lltypes.Type{lltypes.NewPointer(lltypes.I8)} // env first
	for _, p := range params {
		ty, err := l.lowerType(p)
		if err != nil {
			return nil, nil, err
		}
		paramTys = append(paramTys, ty)
	}
	return retTy, paramTys, nil
}

// collectNestedLambdas returns every lambda that is *not* a top-level function
// body, in a deterministic (source) order.
//
// They are gathered up front and lowered as their own top-level functions after
// the ordinary ones, rather than recursively at each creation site. Lowering a
// body re-entrantly would mean saving and restoring the whole per-function state
// (locals, loop stack, managed frames, pending temporaries) mid-expression, and
// getting one field wrong there is the kind of bug that shows up as a leak three
// features later. Nested-in-nested lambdas are in the same flat list, since the
// walk does not stop at a lambda boundary.
func collectNestedLambdas(program *ast.Program, entry *ast.LambdaExpr) []*ast.LambdaExpr {
	topLevel := map[*ast.LambdaExpr]bool{entry: true}
	for _, node := range program.Statements {
		if decl, ok := node.(*ast.VarDeclStmt); ok {
			if lam, ok := decl.Value.(*ast.LambdaExpr); ok {
				topLevel[lam] = true
			}
		}
	}
	var out []*ast.LambdaExpr
	onExpr := func(e ast.Expression) bool {
		if lam, ok := e.(*ast.LambdaExpr); ok && !topLevel[lam] {
			out = append(out, lam)
		}
		return true
	}
	for _, node := range program.Statements {
		switch n := node.(type) {
		case ast.Statement:
			ast.WalkStmt(n, nil, onExpr)
		case ast.Expression:
			ast.WalkExpr(n, nil, onExpr)
		}
	}
	return out
}

// declareClosure emits the signature of a lifted lambda body and records it, so a
// creation site can reference the function before its body exists (the same
// two-pass shape declareFunction/defineFunction use for named functions).
func (l *lowerer) declareClosure(fn *ast.LambdaExpr) error {
	if len(fn.LambdaClauses) > 0 {
		return fmt.Errorf("llvm: multi-clause lambdas are not implemented yet")
	}
	if fn.ReturnType.Type == nil {
		return fmt.Errorf("llvm: a lambda used as a value needs a return type annotation")
	}
	retTy, err := l.lowerType(fn.ReturnType.Type)
	if err != nil {
		return err
	}
	params := []*ir.Param{ir.NewParam("env", lltypes.NewPointer(lltypes.I8))}
	for i, p := range fn.Parameters {
		if p.DefaultValue != nil {
			// Defaults are filled at the *call site* from the callee's declaration
			// (typechecker/default_args.go), and an indirect call through a function
			// value has no declaration to read: a `types.LambdaType` records that a
			// parameter has a default but not what it is. So this one stays refused,
			// and specifically for that reason rather than for want of lowering.
			return fmt.Errorf("llvm: a default parameter value on a lambda used as a value is not implemented yet — a function type does not carry the default expression")
		}
		if paramIsByRef(p) {
			// A function *type* carries no borrow mode, so an indirect call site can
			// only pass by value while this body would expect a pointer. Refuse rather
			// than emit two disagreeing halves of one call.
			return fmt.Errorf("llvm: a `mut`/`ref` parameter on a lambda value is not implemented yet — the function type does not carry the borrow mode")
		}
		irParam, err := l.lowerParameter(p, i)
		if err != nil {
			return err
		}
		params = append(params, irParam)
	}
	l.closureCount++
	l.closures[fn] = l.module.NewFunc(fmt.Sprintf("lyra_closure_%d", l.closureCount), retTy, params...)
	return nil
}

// defineClosure lowers a lifted lambda body. It mirrors defineFunction, with one
// addition: the environment parameter is unpacked into ordinary locals first, so
// the body reads a captured name exactly like any other binding.
//
// Captured bindings are **borrows**, deliberately not framed for release. The
// environment box owns its captures — it retained each managed one when the
// closure was created and drops them when its own count reaches zero — so a body
// that also released them would double-free. This matches how a match-arm binding
// and a for-in loop variable are treated, and it is what makes an *owning* use of
// a capture inside the body (returning it, passing it to an `own` parameter)
// record a plain retain: a capture is never last-use-eligible, having no
// declaration inside the lambda.
func (l *lowerer) defineClosure(fn *ast.LambdaExpr) error {
	irFn := l.closures[fn]
	retTy, err := l.lowerType(fn.ReturnType.Type)
	if err != nil {
		return err
	}
	l.beginFunction(retTy, fn.ReturnType.Type, returnSigned(fn), false)

	entry := irFn.NewBlock("entry")
	caps := l.res.Captures.Of(fn)
	if len(caps) > 0 {
		envTy, err := l.envPayloadType(caps)
		if err != nil {
			return err
		}
		typed := entry.NewBitCast(irFn.Params[0], lltypes.NewPointer(envTy))
		for i, c := range caps {
			ptr := entry.NewGetElementPtr(envTy, typed,
				constant.NewInt(lltypes.I32, 0), constant.NewInt(lltypes.I32, int64(i+1))) // +1: dropFn slot
			slot := entry.NewAlloca(envTy.Fields[i+1])
			entry.NewStore(entry.NewLoad(envTy.Fields[i+1], ptr), slot)
			l.locals[c.Name] = slot
		}
	}
	// Parameters follow the environment, so their ir.Param index is offset by one.
	if err := l.bindParameters(entry, irFn, fn.Parameters, 1); err != nil {
		return err
	}

	if _, isVoid := fn.ReturnType.Type.(types.VoidType); isVoid {
		end, err := l.lowerForEffect(entry, fn.Body)
		if err != nil {
			return err
		}
		if end.Term == nil {
			return l.emitReturn(entry, end, nil)
		}
		return nil
	}
	val, end, err := l.lowerExpr(entry, fn.Body)
	if err != nil {
		return err
	}
	if end.Term == nil {
		return l.emitReturn(entry, end, val)
	}
	return nil
}

// envPayloadType is the environment's payload layout: `{ i8* dropFn, captures... }`.
// The dropFn slot is first so the generic trampoline can read it without knowing
// anything about the captures.
func (l *lowerer) envPayloadType(caps []captures.Capture) (*lltypes.StructType, error) {
	fields := []lltypes.Type{lltypes.NewPointer(lltypes.I8)}
	for _, c := range caps {
		if c.Type == nil {
			return nil, fmt.Errorf("llvm: no type recorded for captured binding %q", c.Name)
		}
		ty, err := l.lowerType(c.Type)
		if err != nil {
			return nil, fmt.Errorf("llvm: cannot lower captured binding %q: %w", c.Name, err)
		}
		fields = append(fields, ty)
	}
	return lltypes.NewStruct(fields...), nil
}

// lowerLambdaExpr builds a closure value at the point the lambda is written: pair
// the (already declared) lifted body with a fresh environment holding a copy of
// each captured binding.
//
// Each managed capture is **retained** into the environment, because the box now
// holds a reference of its own — the ownership pass does not see capture reads
// (it analyzes a nested lambda as a separate function, so the enclosing function's
// walk never visits them), which is exactly why the +1 is minted here instead.
func (l *lowerer) lowerLambdaExpr(block *ir.Block, e *ast.LambdaExpr) (value.Value, *ir.Block, error) {
	irFn, ok := l.closures[e]
	if !ok {
		return nil, nil, fmt.Errorf("llvm: lambda was not lifted (an unsupported form, or a lambda outside a function body)")
	}
	caps := l.res.Captures.Of(e)
	var env value.Value
	if len(caps) == 0 {
		env = l.emptyEnv()
	} else {
		var err error
		env, block, err = l.buildEnv(block, caps)
		if err != nil {
			return nil, nil, err
		}
	}
	return l.closureValue(block, irFn, env), block, nil
}

// closureValue assembles the fat pointer from a lifted function and an
// environment payload pointer.
func (l *lowerer) closureValue(block *ir.Block, fn *ir.Func, env value.Value) value.Value {
	ty := ClosureLLVMType()
	fnPtr := constant.NewBitCast(fn, lltypes.NewPointer(lltypes.I8))
	withFn := block.NewInsertValue(constant.NewUndef(ty), fnPtr, 0)
	return block.NewInsertValue(withFn, env, 1)
}

// buildEnv allocates the environment box and stores the drop glue plus a copy of
// each captured value into it, returning a pointer to the payload.
func (l *lowerer) buildEnv(block *ir.Block, caps []captures.Capture) (value.Value, *ir.Block, error) {
	envTy, err := l.envPayloadType(caps)
	if err != nil {
		return nil, nil, err
	}
	size, err := l.envPayloadSize(caps)
	if err != nil {
		return nil, nil, err
	}
	_, payload := l.rcAllocPayload(block, constant.NewInt(lltypes.I64, int64(size)))
	typed := block.NewBitCast(payload, lltypes.NewPointer(envTy))

	dropFn, err := l.envDropFnFor(caps, envTy)
	if err != nil {
		return nil, nil, err
	}
	block.NewStore(dropFn, block.NewGetElementPtr(envTy, typed,
		constant.NewInt(lltypes.I32, 0), constant.NewInt(lltypes.I32, 0)))

	for i, c := range caps {
		slot, ok := l.locals[c.Name]
		if !ok {
			return nil, nil, fmt.Errorf("llvm: captured binding %q is not in scope where the closure is created", c.Name)
		}
		elem, err := slotElemType(slot)
		if err != nil {
			return nil, nil, err
		}
		v := block.NewLoad(elem, slot)
		// The environment takes its own reference: the enclosing binding is still
		// live and will release its own at scope exit.
		if l.needsDrop(c.Type) {
			if err := l.deepRetain(block, v, c.Type); err != nil {
				return nil, nil, err
			}
		}
		block.NewStore(v, block.NewGetElementPtr(envTy, typed,
			constant.NewInt(lltypes.I32, 0), constant.NewInt(lltypes.I32, int64(i+1))))
	}
	return payload, block, nil
}

// envPayloadSize is the byte size of `{ i8* dropFn, captures... }`, laid out the
// way SizeAndAlign lays out a struct: each field aligned to its own requirement,
// the whole rounded to the widest. Computed from the *Lyra* types, which is where
// SizeAndAlign's knowledge lives.
func (l *lowerer) envPayloadSize(caps []captures.Capture) (int, error) {
	size, maxAlign := pointerSize, pointerSize // the dropFn slot
	for _, c := range caps {
		s, a, ok := SizeAndAlign(l.resolveForLayout(c.Type))
		if !ok {
			return 0, fmt.Errorf("llvm: cannot size captured binding %q (type %s)", c.Name, c.Type)
		}
		size = alignUp(size, a) + s
		if a > maxAlign {
			maxAlign = a
		}
	}
	return alignUp(size, maxAlign), nil
}

// envDropFnFor returns the drop glue to store in an environment: the function
// that releases whatever the *captures* own, or a null pointer when they own
// nothing. It is stored in the environment rather than passed at the release
// site because a release knows only the closure's static type — two closures of
// type `(i64) -> i64` can capture entirely different things.
func (l *lowerer) envDropFnFor(caps []captures.Capture, envTy *lltypes.StructType) (value.Value, error) {
	managed := false
	for _, c := range caps {
		if l.needsDrop(c.Type) {
			managed = true
			break
		}
	}
	if !managed {
		return constant.NewNull(lltypes.NewPointer(lltypes.I8)), nil
	}
	key := envTy.String()
	fn, ok := l.envDropFns[key]
	if !ok {
		l.envDropCount++
		payload := ir.NewParam("payload", lltypes.NewPointer(lltypes.I8))
		fn = l.module.NewFunc(fmt.Sprintf("lyra_env_drop_%d", l.envDropCount), lltypes.Void, payload)
		l.envDropFns[key] = fn

		entry := fn.NewBlock("entry")
		typed := entry.NewBitCast(payload, lltypes.NewPointer(envTy))
		block := entry
		for i, c := range caps {
			if !l.needsDrop(c.Type) {
				continue
			}
			ptr := block.NewGetElementPtr(envTy, typed,
				constant.NewInt(lltypes.I32, 0), constant.NewInt(lltypes.I32, int64(i+1)))
			v := block.NewLoad(envTy.Fields[i+1], ptr)
			var err error
			block, err = l.emitDropValue(block, v, c.Type)
			if err != nil {
				return nil, err
			}
		}
		block.NewRet(nil)
	}
	return constant.NewBitCast(fn, lltypes.NewPointer(lltypes.I8)), nil
}

// closureEnvDropFn is the drop_fn every closure release passes: one generic
// trampoline that reads the per-capture-set glue out of the environment's first
// slot and calls it (doing nothing when that slot is null, which is the common
// case of a closure capturing only scalars, and always the case for the pinned
// empty environment).
//
// This indirection is what lets a single release site free any closure's
// captures. The alternative — choosing the glue at the release site — cannot
// work: the site knows the type `(i64) -> i64` and nothing about which lambda
// produced the value.
func (l *lowerer) closureEnvDropFn() value.Value {
	if l.closureEnvDrop == nil {
		i8ptr := lltypes.NewPointer(lltypes.I8)
		payload := ir.NewParam("payload", i8ptr)
		fn := l.module.NewFunc("lyra_closure_env_drop", lltypes.Void, payload)
		entry := fn.NewBlock("entry")
		call := fn.NewBlock("call")
		done := fn.NewBlock("done")

		slot := entry.NewBitCast(payload, lltypes.NewPointer(i8ptr))
		glue := entry.NewLoad(i8ptr, slot)
		entry.NewCondBr(entry.NewICmp(enum.IPredEQ, glue, constant.NewNull(i8ptr)), done, call)

		dropTy := lltypes.NewPointer(lltypes.NewFunc(lltypes.Void, i8ptr))
		call.NewCall(call.NewBitCast(glue, dropTy), payload)
		call.NewBr(done)

		done.NewRet(nil)
		l.closureEnvDrop = fn
	}
	return constant.NewBitCast(l.closureEnvDrop, lltypes.NewPointer(lltypes.I8))
}

// closureEnvBox recovers a closure's environment box from its fat pointer: the
// env field points rcHeaderSize past the box header, exactly as a string's data
// pointer does, so the box is env - rcHeaderSize.
func (l *lowerer) closureEnvBox(block *ir.Block, closure value.Value) value.Value {
	env := block.NewExtractValue(closure, 1)
	return block.NewGetElementPtr(lltypes.I8, env, constant.NewInt(lltypes.I64, -rcHeaderSize))
}

// emptyEnv is the shared environment of every captureless function value: a
// pinned static box holding a null dropFn. Pinned (rc = PinnedRC) makes retain
// and release no-ops on it, so a plain function used as a value costs no
// allocation while still being a fully ordinary managed value everywhere else —
// the same device string literals use.
func (l *lowerer) emptyEnv() value.Value {
	if l.emptyEnvPtr == nil {
		payloadTy := lltypes.NewStruct(lltypes.NewPointer(lltypes.I8))
		boxConst, boxTy := pinnedBoxConstant(
			constant.NewStruct(payloadTy, constant.NewNull(lltypes.NewPointer(lltypes.I8))))
		g := l.module.NewGlobalDef(".closure.empty_env", boxConst)
		g.Immutable = true
		g.Linkage = enum.LinkagePrivate
		l.emptyEnvPtr = constant.NewBitCast(
			constant.NewGetElementPtr(boxTy, g,
				constant.NewInt(lltypes.I32, 0), constant.NewInt(lltypes.I32, boxPayloadField)),
			lltypes.NewPointer(lltypes.I8))
	}
	return l.emptyEnvPtr
}

// closureThunk adapts a named top-level function to the closure calling
// convention: `@name.closure(i8* env, args...)` ignores the environment and tail-
// calls `@name(args...)`.
//
// A thunk rather than giving every function the environment parameter outright:
// a direct call by name is by far the common case, and it should keep its plain
// signature — both for the IR a reader sees and because every existing call site
// and test pins that shape. The cost is one forwarding call, only for functions
// actually used as values, and only under -O0 (the inliner removes it).
// name is the *key* into l.funcs (module-qualified for a private function), not
// necessarily the source name.
func (l *lowerer) closureThunk(name string) (*ir.Func, error) {
	if fn, ok := l.closureThunks[name]; ok {
		return fn, nil
	}
	target, ok := l.funcs[name]
	if !ok {
		return nil, fmt.Errorf("llvm: no such function %q to use as a value", name)
	}
	params := []*ir.Param{ir.NewParam("env", lltypes.NewPointer(lltypes.I8))}
	for i, p := range target.Params {
		params = append(params, ir.NewParam(fmt.Sprintf("a%d", i), p.Typ))
	}
	thunk := l.module.NewFunc(name+".closure", target.Sig.RetType, params...)
	entry := thunk.NewBlock("entry")
	args := make([]value.Value, 0, len(params)-1)
	for _, p := range params[1:] {
		args = append(args, p)
	}
	call := entry.NewCall(target, args...)
	if _, isVoid := target.Sig.RetType.(*lltypes.VoidType); isVoid {
		entry.NewRet(nil)
	} else {
		entry.NewRet(call)
	}
	l.closureThunks[name] = thunk
	return thunk, nil
}

// namedFunctionValue turns a reference to a top-level function into a closure
// value — `apply(double, 3)` passes `double` as a value. Its environment is the
// shared pinned empty one, since a top-level function captures nothing.
func (l *lowerer) namedFunctionValue(block *ir.Block, name string) (value.Value, error) {
	thunk, err := l.closureThunk(name)
	if err != nil {
		return nil, err
	}
	return l.closureValue(block, thunk, l.emptyEnv()), nil
}

// memberIsFunctionField reports whether `obj.name` names a struct field whose
// declared type is a function — the one member form that is an indirect call
// rather than a method dispatch.
func (l *lowerer) memberIsFunctionField(member *ast.MemberExpr) bool {
	objType, ok := l.recordedType(member.Object)
	if !ok {
		return false
	}
	fields, ok := l.namedStructFields(objType)
	if !ok {
		return false
	}
	for _, f := range fields {
		if f.Name != member.Property.Name {
			continue
		}
		_, isFn := l.stripNewtype(f.Type).(*types.LambdaType)
		return isFn
	}
	return false
}

// lowerCallThroughValue routes a call whose callee is a function *value* rather
// than a name: it recovers the callee's static LambdaType (which carries the
// signature to call through) and hands off to lowerIndirectCall.
func (l *lowerer) lowerCallThroughValue(block *ir.Block, e *ast.FunctionCallExpr) (value.Value, *ir.Block, error) {
	calleeType, ok := l.recordedType(e.Function)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: no type recorded for the callee of an indirect call")
	}
	lt, ok := calleeType.(*types.LambdaType)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: callee of type %s is not callable", calleeType)
	}
	return l.lowerIndirectCall(block, e, lt)
}

// lowerIndirectCall calls a function *value*: unpack the fat pointer, bitcast the
// code pointer to the signature the call site expects, and call it with the
// environment as the leading argument.
//
// The signature comes from the callee's static LambdaType, which is the whole
// reason the environment is an explicit parameter rather than, say, a field the
// body reads: every function value of a given type is called identically,
// whatever lambda is inside it.
func (l *lowerer) lowerIndirectCall(block *ir.Block, e *ast.FunctionCallExpr, lt *types.LambdaType) (value.Value, *ir.Block, error) {
	callee, block, err := l.lowerExpr(block, e.Function)
	if err != nil {
		return nil, nil, err
	}
	if !isClosureLLVMType(callee.Type()) {
		return nil, nil, fmt.Errorf("llvm: callee is not a function value (%s)", callee.Type())
	}
	retTy, paramTys, err := l.closureFnSignature(lambdaTypeParams(lt), lt.ReturnType.Type)
	if err != nil {
		return nil, nil, err
	}
	fnPtr := block.NewExtractValue(callee, 0)
	env := block.NewExtractValue(callee, 1)
	typed := block.NewBitCast(fnPtr, lltypes.NewPointer(lltypes.NewFunc(retTy, paramTys...)))

	args := []value.Value{env}
	for _, argExpr := range e.Arguments {
		v, blk, err := l.lowerExpr(block, argExpr)
		if err != nil {
			return nil, nil, err
		}
		block = blk
		args = append(args, v)
	}
	return block.NewCall(typed, args...), block, nil
}

// lambdaTypeParams is a LambdaType's parameter types, the only thing an indirect
// call's signature needs.
//
// A lambda *type* carries no borrow modes — the collector never populates one for
// a lambda-type parameter, so `(Pt) -> i64` says nothing about `mut`/`ref`. The
// call site can therefore only pass by value, which is why declareClosure rejects
// a borrow modifier on a lambda that becomes a function value: the two sides
// would disagree about whether an argument is a value or a pointer, and that
// disagreement is a miscompile rather than an error.
func lambdaTypeParams(lt *types.LambdaType) []types.Type {
	out := make([]types.Type, 0, len(lt.Parameters))
	for _, p := range lt.Parameters {
		out = append(out, p.Type)
	}
	return out
}
