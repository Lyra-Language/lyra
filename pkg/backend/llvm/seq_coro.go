package llvm

import (
	"fmt"
	"strings"

	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/constant"
	"github.com/llir/llvm/ir/enum"
	lltypes "github.com/llir/llvm/ir/types"
	"github.com/llir/llvm/ir/value"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// Lazy sequences, stage 2: **a sequence as a value.** Stage 1 (seq_lower.go) lowers a
// producer at its consumer and needs no representation; this is the representation
// for everything stage 1 cannot do — a sequence held in a binding, passed to a
// function that is not inlined, stepped with `next()`, or walked in lockstep with
// another (`zip`).
//
// A `gen` body used as a value is emitted as an **LLVM coroutine** (the switched-resume
// ABI: `llvm.coro.id/begin/suspend/end`), so LLVM's own split pass does the hard part —
// hoisting every value live across a `yield` into a heap frame, and turning the body
// into a state machine resumed through the handle. The body is lowered by the same code
// that lowers any function; only `yield` differs (lowerCoroYield), suspending after
// leaving the element in the **promise**, a `{ i1 has, t value }` the consumer reads
// through `llvm.coro.promise`. The coroutine suspends before its first statement, so
// creating a sequence runs none of its body; each `resume` runs to the next yield or to
// the end, where `has` goes false.
//
// The value itself is a ref-counted box around the handle (`seqBoxPtrType`), managed
// exactly as a `shared` box: copies share the cursor, and the last release destroys the
// coroutine, which — through the destroy branch at whatever suspend it sits in —
// releases everything the body still held. A managed parameter is retained on entry and
// framed, because the frame outlives the call that made it.
//
// The split pass only touches a function carrying `presplitcoroutine`, an attribute
// newer than the IR library this backend emits through, so it is added to the text of
// each coroutine's `define` line on the way out (addCoroutineAttributes).

// coroCtx is the coroutine currently being lowered: what a yield stores into and
// where its suspend's three outcomes go.
type coroCtx struct {
	id        value.Value // the token from llvm.coro.id
	hdl       value.Value // the handle from llvm.coro.begin
	promise   value.Value // *promiseTy, the alloca llvm.coro.id was told about
	promiseTy *lltypes.StructType
	elemLyra  types.Type
	cleanup   *ir.Block // frees the frame (llvm.coro.free) and falls into suspend
	suspend   *ir.Block // llvm.coro.end and `ret` of the handle
	final     *ir.Block // has = false, then the final suspend
}

// coroWork is one coroutine body waiting to be emitted, queued when its function is
// first needed and lowered after every ordinary function — never re-entrantly, since
// the per-function lowering state is not.
type coroWork struct {
	fn     *ir.Func
	lambda *ast.LambdaExpr
	subst  map[string]types.Type
	key    string
	site   ast.Location
	name   string
}

const promiseAlign = 16

func seqBoxPtrType() lltypes.Type {
	return lltypes.NewPointer(SharedBoxType(lltypes.NewPointer(lltypes.I8)))
}

func (l *lowerer) coroIntrinsic(name string, ret lltypes.Type, params ...lltypes.Type) *ir.Func {
	if fn, ok := l.coroIntrinsics[name]; ok {
		return fn
	}
	ps := make([]*ir.Param, len(params))
	for i, t := range params {
		ps[i] = ir.NewParam("", t)
	}
	fn := l.module.NewFunc(name, ret, ps...)
	l.coroIntrinsics[name] = fn
	return fn
}

func noneToken() constant.Constant { return &constant.NoneToken{} }

// seqCoroFunc declares (once) and queues the coroutine for a `gen` at one
// instantiation, and answers the function a caller creates it through: the gen's own
// parameters in, the handle out.
func (l *lowerer) seqCoroFunc(lambda *ast.LambdaExpr, subst map[string]types.Type, key string, site ast.Location, name string) (*ir.Func, error) {
	ck := fmt.Sprintf("%p|%s", lambda, key)
	if fn, ok := l.seqCoros[ck]; ok {
		return fn, nil
	}
	restore := l.pushTypeSubst(subst)
	defer restore()
	defer l.pushSpecSite(site)()
	defer l.enterModuleOf(lambda.GetLocation())()
	params := make([]*ir.Param, 0, len(lambda.Parameters))
	for i, param := range lambda.Parameters {
		if param.TypeModifier == types.Mut {
			return nil, errSeqStage1("%q takes a `mut` parameter, which a sequence used as a value cannot: its frame outlives the call", name)
		}
		p, err := l.lowerParameter(param, i)
		if err != nil {
			return nil, err
		}
		params = append(params, p)
	}
	l.seqCoroCount++
	symbol := fmt.Sprintf("lyra.seq.%d.%s", l.seqCoroCount, mangleTypeKey(name))
	fn := l.module.NewFunc(symbol, lltypes.NewPointer(lltypes.I8), params...)
	l.seqCoros[ck] = fn
	l.seqCoroSymbols = append(l.seqCoroSymbols, symbol)
	l.pendingCoros = append(l.pendingCoros, coroWork{fn: fn, lambda: lambda, subst: subst, key: key, site: site, name: name})
	return fn, nil
}

// defineSeqCoroutines lowers every queued coroutine body, including the ones a body
// queues while it is lowered.
func (l *lowerer) defineSeqCoroutines() error {
	for len(l.pendingCoros) > 0 {
		w := l.pendingCoros[0]
		l.pendingCoros = l.pendingCoros[1:]
		if err := l.defineSeqCoroutine(w); err != nil {
			return err
		}
	}
	return nil
}

// defineSeqCoroutine emits one coroutine body: the prologue that sets the frame up, an
// initial suspend, the gen body with yields suspending, and the final suspend and
// cleanup. Frames the body pushed are released on the normal path before the final
// suspend, by a `return` before it branches there, and on the destroy branch of every
// suspend the body may be sitting in.
func (l *lowerer) defineSeqCoroutine(w coroWork) error {
	if len(nestedLambdasIn(w.lambda)) > 0 {
		return errSeqStage1("%q holds a lambda literal, which a sequence function cannot yet", w.name)
	}
	restoreSubst := l.pushTypeSubst(w.subst)
	defer restoreSubst()
	defer l.pushSpecSite(w.site)()
	defer l.pushSpecKey(w.key)()
	prevOwn := l.specOwnership
	if w.key != "" {
		l.specOwnership = l.res.OwnershipBySpec[w.key]
	} else {
		l.specOwnership = nil
	}
	defer func() { l.specOwnership = prevOwn }()
	defer l.enterModuleOf(w.lambda.GetLocation())()
	l.ensureRCRuntime()

	i8ptr := lltypes.NewPointer(lltypes.I8)
	elemLyra, ok := seqElem(w.lambda.ReturnType.Type)
	if !ok {
		return fmt.Errorf("llvm: %s is a gen function whose return is not a Seq", w.name)
	}
	elemLL, err := l.lowerType(elemLyra)
	if err != nil {
		return err
	}
	promiseTy := lltypes.NewStruct(lltypes.I1, elemLL)
	fn := w.fn
	l.beginFunction(i8ptr, nil, false, false)
	entry := fn.NewBlock("entry")
	promise := entry.NewAlloca(promiseTy)
	promise.Align = promiseAlign
	promiseI8 := entry.NewBitCast(promise, i8ptr)
	coroID := l.coroIntrinsic("llvm.coro.id", lltypes.Token, lltypes.I32, i8ptr, i8ptr, i8ptr)
	coroAlloc := l.coroIntrinsic("llvm.coro.alloc", lltypes.I1, lltypes.Token)
	coroSize := l.coroIntrinsic("llvm.coro.size.i64", lltypes.I64)
	coroBegin := l.coroIntrinsic("llvm.coro.begin", i8ptr, lltypes.Token, i8ptr)
	coroSuspend := l.coroIntrinsic("llvm.coro.suspend", lltypes.I8, lltypes.Token, lltypes.I1)
	coroEnd := l.coroIntrinsic("llvm.coro.end", lltypes.I1, i8ptr, lltypes.I1, lltypes.Token)
	coroFree := l.coroIntrinsic("llvm.coro.free", i8ptr, lltypes.Token, i8ptr)
	null := constant.NewNull(i8ptr)
	id := entry.NewCall(coroID, i32c(0), promiseI8, null, null)
	need := entry.NewCall(coroAlloc, id)
	allocBlk := fn.NewBlock("")
	begin := fn.NewBlock("")
	entry.NewCondBr(need, allocBlk, begin)
	mem := allocBlk.NewCall(l.malloc, allocBlk.NewCall(coroSize))
	allocBlk.NewBr(begin)
	memPhi := begin.NewPhi(ir.NewIncoming(null, entry), ir.NewIncoming(mem, allocBlk))
	hdl := begin.NewCall(coroBegin, id, memPhi)

	// The parameters, bound as any function binds them — and every managed one retained
	// and framed, because the frame outlives the call that supplied it.
	if err := l.bindParameters(entry, fn, w.lambda.Parameters, 0); err != nil {
		return err
	}
	for _, param := range w.lambda.Parameters {
		ident, isIdent := param.Pattern.(*ast.IdentifierPattern)
		if !isIdent || param.TypeModifier == types.Own {
			continue
		}
		pt := l.applyTypeSubst(param.Type)
		if !l.needsDrop(pt) {
			continue
		}
		slot := l.locals[ident.Name]
		v := begin.NewLoad(slot.(*ir.InstAlloca).ElemType, slot)
		if _, err := l.emitOwnedValue(begin, v, pt, retainWalk); err != nil {
			return err
		}
		l.addManagedBinding(slot, pt)
	}

	cleanup := fn.NewBlock("")
	suspend := fn.NewBlock("")
	final := fn.NewBlock("")
	ctx := &coroCtx{id: id, hdl: hdl, promise: promise, promiseTy: promiseTy, elemLyra: elemLyra, cleanup: cleanup, suspend: suspend, final: final}
	prevCoro := l.coro
	l.coro = ctx
	defer func() { l.coro = prevCoro }()

	// Suspend before the first statement, so creating a sequence runs nothing.
	bodyStart := fn.NewBlock("")
	s0 := begin.NewCall(coroSuspend, noneToken(), constant.False)
	initialDestroy := fn.NewBlock("")
	begin.NewSwitch(s0, suspend, ir.NewCase(constant.NewInt(lltypes.I8, 0), bodyStart), ir.NewCase(constant.NewInt(lltypes.I8, 1), initialDestroy))
	if err := l.releaseManagedFramesFrom(initialDestroy, 0); err != nil {
		return err
	}
	initialDestroy.NewBr(cleanup)

	// A `return` inside the body ends the sequence: frames released, then final.
	l.inlineRet = &inlineReturn{slot: nil, exit: final, frameDepth: 0}
	l.yield = nil
	end, err := l.lowerForEffect(bodyStart, w.lambda.Body)
	if err != nil {
		return err
	}
	if end.Term == nil {
		if err := l.releaseManagedFramesFrom(end, 0); err != nil {
			return err
		}
		end.NewBr(final)
	}

	hasPtr := final.NewGetElementPtr(promiseTy, promise, i32c(0), i32c(0))
	final.NewStore(constant.False, hasPtr)
	sf := final.NewCall(coroSuspend, noneToken(), constant.True)
	trap := fn.NewBlock("")
	trap.NewUnreachable()
	final.NewSwitch(sf, suspend, ir.NewCase(constant.NewInt(lltypes.I8, 0), trap), ir.NewCase(constant.NewInt(lltypes.I8, 1), cleanup))

	freed := cleanup.NewCall(coroFree, id, hdl)
	cleanup.NewCall(l.free, freed)
	cleanup.NewBr(suspend)
	suspend.NewCall(coroEnd, hdl, constant.False, noneToken())
	suspend.NewRet(hdl)
	return l.resolveExitReleases(fn)
}

// lowerCoroYield is `yield e` inside a coroutine body: the element goes into the
// promise with a +1 for the consumer, the coroutine suspends, and the three outcomes
// are resumed here, destroyed — releasing everything live — or handed back.
func (l *lowerer) lowerCoroYield(block *ir.Block, v value.Value) (*ir.Block, error) {
	c := l.coro
	fn := block.Parent
	if _, err := l.emitOwnedValue(block, v, c.elemLyra, retainWalk); err != nil {
		return nil, err
	}
	block.NewStore(constant.True, block.NewGetElementPtr(c.promiseTy, c.promise, i32c(0), i32c(0)))
	block.NewStore(v, block.NewGetElementPtr(c.promiseTy, c.promise, i32c(0), i32c(1)))
	coroSuspend := l.coroIntrinsic("llvm.coro.suspend", lltypes.I8, lltypes.Token, lltypes.I1)
	s := block.NewCall(coroSuspend, noneToken(), constant.False)
	resume := fn.NewBlock("")
	destroy := fn.NewBlock("")
	block.NewSwitch(s, c.suspend, ir.NewCase(constant.NewInt(lltypes.I8, 0), resume), ir.NewCase(constant.NewInt(lltypes.I8, 1), destroy))
	// Destroyed while suspended here: whatever the body still holds is released — the
	// frames, and the temporaries of the statements this suspend sits inside.
	l.recordExitReleases(destroy, 0)
	if err := l.releaseManagedFramesFrom(destroy, 0); err != nil {
		return nil, err
	}
	destroy.NewBr(c.cleanup)
	return resume, nil
}

// lowerSeqValue lowers a `Seq`-typed expression to its box: a call to a `gen`
// creates the coroutine; anything else already is a value.
func (l *lowerer) lowerSeqValue(block *ir.Block, expr ast.Expression) (value.Value, *ir.Block, error) {
	if call, ok := expr.(*ast.FunctionCallExpr); ok {
		if lambda, subst, key, site, ok := l.seqCallee(call); ok && lambda.IsGenerator {
			return l.emitSeqCoroutineCall(block, call, lambda, subst, key, site)
		}
		if lambda, _, _, _, ok := l.seqCallee(call); ok && mentionsSeq(lambda) {
			return nil, nil, errSeqStage1("%q returns a sequence from a plain function; only a `gen` can produce a sequence held as a value", lambdaDisplayName(call))
		}
	}
	if ub, ok := expr.(*ast.UnsafeBlockExpr); ok && ub.Body != nil {
		return l.lowerSeqValue(block, ub.Body)
	}
	return l.lowerExpr(block, expr)
}

// emitSeqCoroutineCall creates the coroutine for a `gen` call and boxes its handle.
func (l *lowerer) emitSeqCoroutineCall(block *ir.Block, call *ast.FunctionCallExpr, lambda *ast.LambdaExpr, subst map[string]types.Type, key string, site ast.Location) (value.Value, *ir.Block, error) {
	fn, err := l.seqCoroFunc(lambda, subst, key, site, lambdaDisplayName(call))
	if err != nil {
		return nil, nil, err
	}
	if len(call.Arguments) != len(lambda.Parameters) {
		return nil, nil, fmt.Errorf("llvm: %s expects %d argument(s), got %d", lambdaDisplayName(call), len(lambda.Parameters), len(call.Arguments))
	}
	args := make([]value.Value, 0, len(call.Arguments))
	for i, arg := range call.Arguments {
		var v value.Value
		if types.IsSeq(lambda.Parameters[i].Type) {
			v, block, err = l.lowerSeqValue(block, arg)
		} else {
			v, block, err = l.lowerExpr(block, arg)
		}
		if err != nil {
			return nil, nil, err
		}
		if diverged(v, block) {
			return nil, block, nil
		}
		args = append(args, v)
	}
	hdl := block.NewCall(fn, args...)
	l.ensureRCRuntime()
	boxI8 := block.NewCall(l.rcAlloc, i64c(int64(rcHeaderSize+pointerSize)))
	boxTy := SharedBoxType(lltypes.NewPointer(lltypes.I8))
	box := block.NewBitCast(boxI8, lltypes.NewPointer(boxTy))
	block.NewStore(hdl, boxPayloadPtr(block, boxTy, box))
	return box, block, nil
}

// seqHandle reads the coroutine handle out of a sequence box.
func seqHandle(block *ir.Block, box value.Value) value.Value {
	boxTy := SharedBoxType(lltypes.NewPointer(lltypes.I8))
	return block.NewLoad(lltypes.NewPointer(lltypes.I8), boxPayloadPtr(block, boxTy, box))
}

// seqAdvance resumes the coroutine behind box once and answers the promise's `has` and
// a pointer to its value. Reading the value transfers the +1 the producer left there.
func (l *lowerer) seqAdvance(block *ir.Block, box value.Value, elemLL lltypes.Type) (has value.Value, valuePtr value.Value) {
	i8ptr := lltypes.NewPointer(lltypes.I8)
	promiseTy := lltypes.NewStruct(lltypes.I1, elemLL)
	hdl := seqHandle(block, box)
	block.NewCall(l.coroIntrinsic("llvm.coro.resume", lltypes.Void, i8ptr), hdl)
	pp := block.NewCall(l.coroIntrinsic("llvm.coro.promise", i8ptr, i8ptr, lltypes.I32, lltypes.I1), hdl, i32c(promiseAlign), constant.False)
	promise := block.NewBitCast(pp, lltypes.NewPointer(promiseTy))
	has = block.NewLoad(lltypes.I1, block.NewGetElementPtr(promiseTy, promise, i32c(0), i32c(0)))
	valuePtr = block.NewGetElementPtr(promiseTy, promise, i32c(0), i32c(1))
	return has, valuePtr
}

// lowerSeqPull walks a sequence *value* for the consumer y: resume, read, run the
// consumer, again — until `has` is false or the consumer breaks. Each element is the
// consumer's to release once its body has run, on the resumed path here and on a break
// through the consumer's temp base.
func (l *lowerer) lowerSeqPull(block *ir.Block, box value.Value, elemLyra types.Type, y *seqYield) (*ir.Block, error) {
	elemLL, err := l.lowerType(elemLyra)
	if err != nil {
		return nil, err
	}
	fn := block.Parent
	loop := fn.NewBlock("")
	body := fn.NewBlock("")
	cont := fn.NewBlock("") // the consumer's continue target: release the element, resume
	done := fn.NewBlock("")
	block.NewBr(loop)
	has, valuePtr := l.seqAdvance(loop, box, elemLL)
	loop.NewCondBr(has, body, done)
	v := body.NewLoad(elemLL, valuePtr)
	if l.needsDrop(elemLyra) {
		l.pendingReleases = append(l.pendingReleases, pendingTemp{val: v, block: body, ty: elemLyra})
	}
	if _, err := l.runConsumer(body, y, v, cont); err != nil {
		return nil, err
	}
	if l.needsDrop(elemLyra) {
		if err := l.deepRelease(cont, v, elemLyra); err != nil {
			return nil, err
		}
		l.pendingReleases = l.pendingReleases[:len(l.pendingReleases)-1]
	}
	cont.NewBr(loop)
	return done, nil
}

// lowerSeqNext is `s.next()`: one resume, then `Some` of the element or `None`. The
// value is read unconditionally — its bytes are stale when `has` is false, and the
// `None` arm never looks at them — so the two arms build without a branch, which is
// what keeps the temporary-release machinery straight (rule 17).
func (l *lowerer) lowerSeqNext(block *ir.Block, call *ast.FunctionCallExpr, member *ast.MemberExpr) (value.Value, *ir.Block, error) {
	recvType, ok := l.recordedType(member.Object)
	if !ok || !types.IsSeq(recvType) {
		return nil, nil, fmt.Errorf("llvm: next() on a non-sequence receiver")
	}
	elemLyra := recvType.(types.ParameterizedType).TypeArguments[0]
	elemLL, err := l.lowerType(elemLyra)
	if err != nil {
		return nil, nil, err
	}
	resultType, ok := l.recordedType(call)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: next() call has no recorded type")
	}
	dt, ok := l.resolveShape(resultType).(types.DataType)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: next() must return a Maybe, got %s", resultType)
	}
	someC, someTag, hasSome := findConstructor(dt, "Some")
	noneC, noneTag, hasNone := findConstructor(dt, "None")
	if !hasSome || !hasNone {
		return nil, nil, fmt.Errorf("llvm: next()'s return type %q is not a canonical Maybe", dt.Name)
	}
	box, block, err := l.lowerExpr(block, member.Object)
	if err != nil {
		return nil, nil, err
	}
	if diverged(box, block) {
		return nil, block, nil
	}
	has, valuePtr := l.seqAdvance(block, box, elemLL)
	v := block.NewLoad(elemLL, valuePtr)
	some, err := l.buildDataValue(block, dt, someTag, someC, []value.Value{v})
	if err != nil {
		return nil, nil, err
	}
	none, err := l.buildDataValue(block, dt, noneTag, noneC, nil)
	if err != nil {
		return nil, nil, err
	}
	return block.NewSelect(has, some, none), block, nil
}

// seqDropFn is the drop glue every sequence box shares: destroy the coroutine.
func (l *lowerer) seqDropFn() value.Value {
	i8ptr := lltypes.NewPointer(lltypes.I8)
	if l.seqDrop != nil {
		return constant.NewBitCast(l.seqDrop, i8ptr)
	}
	fn := l.module.NewFunc("lyra_seq_drop", lltypes.Void, ir.NewParam("payload", i8ptr))
	entry := fn.NewBlock("entry")
	hdl := entry.NewLoad(i8ptr, entry.NewBitCast(fn.Params[0], lltypes.NewPointer(i8ptr)))
	entry.NewCall(l.coroIntrinsic("llvm.coro.destroy", lltypes.Void, i8ptr), hdl)
	entry.NewRet(nil)
	l.seqDrop = fn
	return constant.NewBitCast(fn, i8ptr) // the release shim takes its drop_fn as an i8*
}

// seqElem answers a `Seq<t>`'s element, resolved under the active substitution.
func seqElem(t types.Type) (types.Type, bool) {
	p, ok := t.(types.ParameterizedType)
	if !ok || !types.IsSeq(p) || len(p.TypeArguments) != 1 {
		return nil, false
	}
	return p.TypeArguments[0], true
}

// addCoroutineAttributes marks every coroutine's `define` with `presplitcoroutine`, the
// attribute LLVM's split pass requires and the IR library cannot spell.
func addCoroutineAttributes(irText string, symbols []string) string {
	for _, sym := range symbols {
		needle := "@" + sym + "("
		lines := strings.Split(irText, "\n")
		for i, line := range lines {
			if strings.HasPrefix(line, "define ") && strings.Contains(line, needle) && strings.HasSuffix(line, " {") {
				lines[i] = strings.TrimSuffix(line, " {") + " presplitcoroutine {"
			}
		}
		irText = strings.Join(lines, "\n")
	}
	return irText
}

// paramUsedAsValue reports whether the body refers to parameter `name` other than as
// the source of a `for-in`, a comprehension generator or a `yield from` — the three
// positions a sequence parameter can be lowered at without a value. Any other use
// needs the parameter as a value (a coroutine box).
func paramUsedAsValue(lambda *ast.LambdaExpr, name string) bool {
	sourceUses := map[*ast.IdentifierExpr]bool{}
	mark := func(e ast.Expression) {
		if id, ok := e.(*ast.IdentifierExpr); ok && id.Name == name {
			sourceUses[id] = true
		}
	}
	used := false
	ast.WalkExpr(lambda.Body, nil, func(e ast.Expression) bool {
		switch ex := e.(type) {
		case *ast.ForInLoopExpr:
			mark(ex.Iterable)
		case *ast.YieldFromExpr:
			mark(ex.Generator)
		case *ast.ArrayCompExpr:
			for i := range ex.Generators {
				mark(ex.Generators[i].Value)
			}
		case *ast.IdentifierExpr:
			if ex.Name == name && !sourceUses[ex] {
				used = true
			}
		}
		return true
	})
	return used
}

var _ = enum.IPredEQ
