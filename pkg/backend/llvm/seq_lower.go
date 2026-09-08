package llvm

import (
	"fmt"
	"maps"

	"github.com/llir/llvm/ir"
	lltypes "github.com/llir/llvm/ir/types"
	"github.com/llir/llvm/ir/value"

	"github.com/Lyra-Language/lyra/pkg/analyzer/ownership"
	"github.com/Lyra-Language/lyra/pkg/analyzer/typechecker"
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// Lazy sequences, stage 1: **a producer is lowered at its consumer.** Nothing ever
// represents a `Seq<t>` — there is no box, no state machine, no suspension. A `gen`
// function's body is lowered *inside* the loop that walks it, with each `yield e`
// becoming the consumer's body run on `e`, and a chain (`naturals().filter(p).take(n)`)
// fuses into one loop nest by construction: `take`'s body is lowered at the consumer,
// its `for x in self` lowers `filter`'s body in turn, whose `for x in self` lowers
// `naturals`. That is the design todo.md's "Lazy sequences" section recommends, and it
// is what makes `for x in xs.seq().filter(p).map(f)` allocate nothing.
//
// Three rules make it work, and each is a place a reader will look for a bug:
//
//   - **A consumer's `break` leaves the whole producer, and its `continue` resumes it.**
//     The consumer's body is lowered under a loopCtx whose break target is the block
//     after the entire producer and whose continue target is the block the producer
//     resumes in after this `yield` (each yield gets its own). The producer's own loops
//     are pushed on top while its body is lowered, so `take`'s `break` — written in the
//     body *it* runs as a consumer — exits the producer it is consuming, and nothing
//     else. The frame depth and temp base on that loopCtx are the consumer's, so a break
//     also releases everything the producer had live.
//   - **Every body runs in its own environment.** The producer's body sees only its
//     parameters; the consumer's body sees the consumer's locals, loops, substitution,
//     ownership table and enclosing yield handler — captured when the consumer was set
//     up and reinstalled at each yield (seqEnv). A `Seq`-typed argument is not evaluated
//     at all: it is the producer *expression* plus the environment it was written in,
//     and `for x in self` inside the callee lowers it there (seqBinding).
//   - **A terminal is an inlined call.** `sum`, `count`, `first` and `to_array` are plain
//     functions taking a `Seq`, so their `for-in` is behind a call boundary; inlining the
//     call at its site makes the loop local again, with `return` redirected to a result
//     slot and the exit block (inlineReturn), and a bare `return` inside a gen body
//     finishing the sequence the same way.
//
// What stage 1 refuses, loudly: a sequence held in a binding or used as a value, a
// producer that is not a call to a `gen` (or to a function whose whole body is one), a
// lambda literal inside a sequence function's body, a destructuring or `mut` parameter
// on one, and a comprehension with a sequence beside another generator. `zip` is the
// deliberate hole — two producers interleaved need pull — and is the case stage 2's
// state machine exists for.

// seqEnv is everything a body's lowering depends on besides the shared frame and
// temporary stacks: the names in scope, the loops a break may target, the generic
// substitution and the ownership table computed under it, the module names resolve in,
// the sequence parameters bound syntactically, and the two redirect handlers.
type seqEnv struct {
	locals        map[string]value.Value
	loops         []loopCtx
	typeSubst     map[string]types.Type
	specSite      ast.Location
	specOwnership *ownership.Table
	specKey       string
	currentLoc    ast.Location
	seqParams     map[string]seqBinding
	yield         *seqYield
	inlineRet     *inlineReturn
	// pendingBase is the statement's temporary base. It travels with the environment
	// because a body's flushes must reach only its own temporaries: an inlined
	// terminal that flushed from the *caller's* base released a string the caller's
	// statement was still using — `"${xs.join(",")} ${s.sum()}"` copied freed bytes.
	pendingBase int
	coro        *coroCtx
}

func (l *lowerer) captureEnv() seqEnv {
	return seqEnv{
		locals:        maps.Clone(l.locals),
		loops:         append([]loopCtx(nil), l.loops...),
		typeSubst:     l.typeSubst,
		specSite:      l.specSite,
		specOwnership: l.specOwnership,
		specKey:       l.specKey,
		currentLoc:    l.currentLoc,
		seqParams:     maps.Clone(l.seqParams),
		yield:         l.yield,
		inlineRet:     l.inlineRet,
		pendingBase:   l.pendingBase,
		coro:          l.coro,
	}
}

func (l *lowerer) installEnv(e seqEnv) {
	l.locals = maps.Clone(e.locals)
	l.loops = append([]loopCtx(nil), e.loops...)
	l.typeSubst = e.typeSubst
	l.specSite = e.specSite
	l.specOwnership = e.specOwnership
	l.specKey = e.specKey
	l.currentLoc = e.currentLoc
	l.seqParams = maps.Clone(e.seqParams)
	l.yield = e.yield
	l.inlineRet = e.inlineRet
	l.pendingBase = e.pendingBase
	l.coro = e.coro
}

// seqBinding is a `Seq`-typed parameter: the producer expression the caller passed, and
// the environment that expression is written in — the caller's, not the callee's.
type seqBinding struct {
	expr ast.Expression
	env  seqEnv
}

// seqYield is the consumer a producer's `yield` hands each element to.
type seqYield struct {
	consume    func(block *ir.Block, elem value.Value) (*ir.Block, error)
	exit       *ir.Block // after the whole producer: the consumer's `break` target
	env        seqEnv    // the consumer's environment, reinstalled at each yield
	frameDepth int       // len(managedFrames) when the consumer was set up
	tempBase   int       // len(pendingReleases) when the consumer was set up
}

// inlineReturn redirects a `return` inside an inlined body: the value goes to slot (nil
// for a void or gen body), the frames above frameDepth are released, and control goes
// to exit — where the caller picks the result up.
type inlineReturn struct {
	slot       value.Value
	exit       *ir.Block
	frameDepth int
	retType    lltypes.Type
	retSigned  bool
}

// isSeqType reports whether t is a `Seq<…>`, by the name the typechecker knows it under.
func isSeqType(t types.Type) bool {
	p, ok := t.(types.ParameterizedType)
	return ok && p.Name == typechecker.SeqTypeName
}

func errSeqStage1(format string, args ...any) error {
	return fmt.Errorf("llvm: "+format+" (lazy sequences, stage 1: a sequence is lowered at its consumer and has no value of its own; todo.md, Lazy sequences)", args...)
}

// lowerSeqConsume lowers `src`, a `Seq`-typed expression, feeding each element to
// consume, and returns the block control reaches once the producer is done — by
// finishing, or by the consumer breaking.
func (l *lowerer) lowerSeqConsume(block *ir.Block, src ast.Expression, consume func(*ir.Block, value.Value) (*ir.Block, error)) (*ir.Block, error) {
	exit := block.Parent.NewBlock("")
	y := &seqYield{
		consume:    consume,
		exit:       exit,
		env:        l.captureEnv(),
		frameDepth: len(l.managedFrames),
		tempBase:   len(l.pendingReleases),
	}
	end, err := l.lowerSeqProducer(block, src, y)
	if err != nil {
		return nil, err
	}
	if end != nil && end.Term == nil {
		end.NewBr(exit)
	}
	return exit, nil
}

// lowerSeqProducer lowers the producer `src` under the yield handler y and returns the
// block the producer finishes in (nil-terminated when it never falls through).
func (l *lowerer) lowerSeqProducer(block *ir.Block, src ast.Expression, y *seqYield) (*ir.Block, error) {
	switch s := src.(type) {
	case *ast.IdentifierExpr:
		// A sequence parameter of the body being inlined: the producer expression the
		// caller wrote, lowered in the caller's environment.
		if b, ok := l.seqParams[s.Name]; ok {
			saved := l.captureEnv()
			l.installEnv(b.env)
			end, err := l.lowerSeqProducer(block, b.expr, y)
			l.installEnv(saved)
			return end, err
		}
		// A binding holding a sequence value: pulled through its coroutine (stage 2).
		return l.lowerSeqPullExpr(block, s, y)
	case *ast.UnsafeBlockExpr:
		if s.Body != nil {
			return l.lowerSeqProducer(block, s.Body, y)
		}
	case *ast.FunctionCallExpr:
		lambda, subst, key, site, ok := l.seqCallee(s)
		if !ok {
			return nil, errSeqStage1("this sequence is produced by something other than a call to a `gen` function")
		}
		if lambda.IsGenerator {
			_, end, err := l.inlineCallee(block, s, lambda, subst, key, site, y)
			return end, err
		}
		// A plain function whose whole body is a sequence expression — `let primes =
		// () -> Seq<i64> => naturals().filter(is_prime)` — is a name for a producer:
		// its parameters bind as any inlined callee's and its body is lowered as the
		// producer. A block body would need its statements run first and its tail
		// treated as the producer; refused until something needs it.
		if _, isBlock := lambda.Body.(*ast.BlockExpr); isBlock || lambda.Body == nil {
			return nil, errSeqStage1("%q returns a sequence from a block body; only a function whose whole body is the sequence expression is inlined as a producer", lambdaDisplayName(s))
		}
		return l.inlineProducerAlias(block, s, lambda, subst, key, site, y)
	}
	// Anything else that is a sequence — a field, an element, a call's result — is a
	// value, pulled through its coroutine (stage 2).
	return l.lowerSeqPullExpr(block, src, y)
}

// lowerSeqPullExpr lowers a sequence *value* and walks it for y.
func (l *lowerer) lowerSeqPullExpr(block *ir.Block, src ast.Expression, y *seqYield) (*ir.Block, error) {
	srcType, ok := l.recordedType(src)
	if !ok {
		return nil, fmt.Errorf("llvm: no type recorded for a sequence source")
	}
	elem, ok := seqElem(srcType)
	if !ok {
		return nil, errSeqStage1("a %s of type %s cannot be walked as a sequence", src.GetName(), srcType)
	}
	box, block, err := l.lowerSeqValue(block, src)
	if err != nil {
		return nil, err
	}
	if diverged(box, block) {
		return block, nil
	}
	return l.lowerSeqPull(block, box, elem, y)
}

func lambdaDisplayName(call *ast.FunctionCallExpr) string {
	if id, ok := call.Function.(*ast.IdentifierExpr); ok {
		return id.Name
	}
	return "the callee"
}

// seqCallee resolves a call's callee to its declaration and, for a generic one, the
// substitution and key this call site instantiates it at — composed with the active
// substitution, exactly as specializedFuncFor composes. ok is false when the callee is
// not a named top-level function (a local closure, say).
func (l *lowerer) seqCallee(call *ast.FunctionCallExpr) (lambda *ast.LambdaExpr, subst map[string]types.Type, key string, site ast.Location, ok bool) {
	ident, isIdent := call.Function.(*ast.IdentifierExpr)
	if !isIdent {
		return nil, nil, "", ast.Location{}, false
	}
	if _, isLocal := l.locals[ident.Name]; isLocal {
		return nil, nil, "", ast.Location{}, false
	}
	if inst, found := l.res.Instantiations.Get(call); found {
		composed := inst.Substituted(l.typeSubst, types.Substitute)
		site = composed.Site
		if len(l.typeSubst) > 0 {
			site = l.specSite
		}
		return composed.Func, composed.Subst, composed.Key(), site, true
	}
	if lam, found := l.res.TypeTable.Callee(call); found {
		return lam, nil, "", ast.Location{}, true
	}
	if lam, found := l.res.SymbolTable.LookupFunctionFrom(ident.Name, call.GetLocation()); found {
		return lam, nil, "", ast.Location{}, true
	}
	return nil, nil, "", ast.Location{}, false
}

// inlineSeqCall is the call-site half: a call to a function that takes or returns a
// sequence, met in value position. A terminal (`sum`, `first`) is inlined and its
// result is the call's value; a producer used as a value has nowhere to go.
func (l *lowerer) inlineSeqCall(block *ir.Block, call *ast.FunctionCallExpr, lambda *ast.LambdaExpr, subst map[string]types.Type, key string, site ast.Location) (value.Value, *ir.Block, error) {
	// A producer in value position is a sequence *value*: a coroutine (stage 2).
	if lambda.IsGenerator || (lambda.ReturnType.Type != nil && isSeqType(lambda.ReturnType.Type)) {
		return l.lowerSeqValue(block, call)
	}
	return l.inlineCallee(block, call, lambda, subst, key, site, nil)
}

// inlineProducerAlias lowers a plain function whose body is a sequence expression as
// the producer it names: its parameters bound, its body lowered under y.
func (l *lowerer) inlineProducerAlias(block *ir.Block, call *ast.FunctionCallExpr, lambda *ast.LambdaExpr, subst map[string]types.Type, key string, site ast.Location, y *seqYield) (*ir.Block, error) {
	block, calleeBase, restore, err := l.bindInlineParams(block, call, lambda, subst, key, site)
	if err != nil {
		return nil, err
	}
	defer restore()
	l.yield = nil
	l.inlineRet = nil
	end, err := l.lowerSeqProducer(block, lambda.Body, y)
	if err != nil {
		return nil, err
	}
	if end != nil && end.Term == nil {
		if err := l.releaseManagedFramesFrom(end, calleeBase); err != nil {
			return nil, err
		}
	}
	l.popManagedFrame()
	return end, nil
}

// inlineCallee lowers a call to `lambda` in place. With a yield handler it is a
// producer: the body runs for effect and every `yield` feeds y. Without one it is a
// terminal: the body's value — or what a `return` carried — is the result. Either way
// the body's own `return` goes to the exit block rather than out of the enclosing
// function (inlineReturn, honoured by emitReturn).
func (l *lowerer) inlineCallee(block *ir.Block, call *ast.FunctionCallExpr, lambda *ast.LambdaExpr, subst map[string]types.Type, key string, site ast.Location, y *seqYield) (value.Value, *ir.Block, error) {
	if len(nestedLambdasIn(lambda)) > 0 {
		return nil, nil, errSeqStage1("%q holds a lambda literal, which a sequence function cannot yet", lambdaDisplayName(call))
	}
	fn := block.Parent
	entry := fn.Blocks[0]
	block, calleeBase, restore, err := l.bindInlineParams(block, call, lambda, subst, key, site)
	if err != nil {
		return nil, nil, err
	}
	defer restore()

	// The callee's return type, under its substitution: what a `return v` stores and
	// what the exit block loads. A gen body has no result, and neither does a void one.
	var retType lltypes.Type
	var slot value.Value
	retSigned := false
	if !lambda.IsGenerator && lambda.ReturnType.Type != nil {
		if _, isVoid := lambda.ReturnType.Type.(types.VoidType); !isVoid {
			retType, err = l.lowerType(lambda.ReturnType.Type)
			if err != nil {
				return nil, nil, err
			}
			slot = entry.NewAlloca(retType)
			retSigned = returnSigned(lambda)
		}
	}
	done := fn.NewBlock("")
	l.yield = y
	l.inlineRet = &inlineReturn{slot: slot, exit: done, frameDepth: calleeBase, retType: retType, retSigned: retSigned}

	start := block
	var end *ir.Block
	if slot == nil {
		end, err = l.lowerForEffect(block, lambda.Body)
		if err != nil {
			return nil, nil, err
		}
	} else {
		var v value.Value
		v, end, err = l.lowerExpr(block, lambda.Body)
		if err != nil {
			return nil, nil, err
		}
		if end.Term == nil {
			if err := l.flushStmtTemps(start, end); err != nil {
				return nil, nil, err
			}
			l.storeInlineResult(end, v, l.inlineRet)
		}
	}
	if end.Term == nil {
		if err := l.releaseManagedFramesFrom(end, calleeBase); err != nil {
			return nil, nil, err
		}
		end.NewBr(done)
	}
	l.popManagedFrame()
	if slot == nil {
		return nil, done, nil
	}
	return done.NewLoad(retType, slot), done, nil
}

// storeInlineResult stores a body's value into the inline result slot, coerced the way
// emitReturn coerces a returned value to the function's type.
func (l *lowerer) storeInlineResult(block *ir.Block, v value.Value, r *inlineReturn) {
	if r.slot == nil || v == nil {
		return
	}
	switch rt := r.retType.(type) {
	case *lltypes.IntType:
		v = coerceIntWidth(block, v, r.retSigned, rt)
	case *lltypes.FloatType:
		v = coerceFloatWidth(block, v, rt)
	}
	block.NewStore(v, r.slot)
}

// bindInlineParams evaluates the call's arguments in the caller's environment, then
// switches to the callee's — its substitution, ownership table, module, an empty
// scope — and binds each parameter the way bindParameters binds an incoming value: a
// slot per by-value parameter (framed when `own`), the caller's address for a `mut` or
// `ref` one, and the producer expression itself for a `Seq`. It pushes the callee's
// managed frame and returns its base depth; the caller pops the frame and calls
// restore, which reinstates the caller's environment.
func (l *lowerer) bindInlineParams(block *ir.Block, call *ast.FunctionCallExpr, lambda *ast.LambdaExpr, subst map[string]types.Type, key string, site ast.Location) (*ir.Block, int, func(), error) {
	if len(lambda.LambdaClauses) > 0 {
		return nil, 0, nil, errSeqStage1("%q is a multi-clause function, which cannot be inlined as a sequence function yet", lambdaDisplayName(call))
	}
	if len(call.Arguments) != len(lambda.Parameters) {
		return nil, 0, nil, fmt.Errorf("llvm: %s expects %d argument(s), got %d", lambdaDisplayName(call), len(lambda.Parameters), len(call.Arguments))
	}
	type boundArg struct {
		param ast.Parameter
		name  string
		val   value.Value
		byRef bool
		seq   *seqBinding
	}
	callerEnv := l.captureEnv()
	bound := make([]boundArg, 0, len(lambda.Parameters))
	for i, param := range lambda.Parameters {
		ident, isIdent := param.Pattern.(*ast.IdentifierPattern)
		if !isIdent {
			return nil, 0, nil, errSeqStage1("%q destructures a parameter, which a sequence function cannot yet", lambdaDisplayName(call))
		}
		arg := call.Arguments[i]
		b := boundArg{param: param, name: ident.Name}
		switch {
		case isSeqType(param.Type) && !paramUsedAsValue(lambda, ident.Name):
			b.seq = &seqBinding{expr: arg, env: callerEnv}
		case isSeqType(param.Type):
			// Used as a value in the body (`rest.next()`, `var r = other`): the argument
			// becomes a coroutine box, and the body's walks over it pull.
			v, next, err := l.lowerSeqValue(block, arg)
			if err != nil {
				return nil, 0, nil, err
			}
			if diverged(v, next) {
				return nil, 0, nil, errSeqStage1("an argument to %q diverges", lambdaDisplayName(call))
			}
			block, b.val = next, v
		case paramIsByRef(param):
			ptr, next, err := l.argumentAddress(block, arg)
			if err != nil {
				return nil, 0, nil, err
			}
			if diverged(ptr, next) {
				return nil, 0, nil, errSeqStage1("an argument to %q diverges", lambdaDisplayName(call))
			}
			block, b.val, b.byRef = next, ptr, true
		default:
			v, next, err := l.lowerExpr(block, arg)
			if err != nil {
				return nil, 0, nil, err
			}
			if diverged(v, next) {
				return nil, 0, nil, errSeqStage1("an argument to %q diverges", lambdaDisplayName(call))
			}
			block, b.val = next, v
		}
		bound = append(bound, b)
	}

	// The callee's environment. Its ownership table is the instantiation's for a generic
	// callee and the program-wide one otherwise — never the *caller's* specialization
	// table, which knows nothing about these nodes.
	saved := l.captureEnv()
	l.locals = map[string]value.Value{}
	l.loops = nil
	l.seqParams = map[string]seqBinding{}
	l.typeSubst = subst
	l.specSite = site
	l.specKey = key
	if key != "" {
		l.specOwnership = l.res.OwnershipBySpec[key]
	} else {
		l.specOwnership = nil
	}
	l.currentLoc = lambda.GetLocation()
	// The callee's temporaries start here: the arguments' belong to the caller's
	// statement, which flushes them after the inlined body has run.
	l.pendingBase = len(l.pendingReleases)

	calleeBase := len(l.managedFrames)
	l.pushManagedFrame()
	entry := block.Parent.Blocks[0]
	for _, b := range bound {
		switch {
		case b.seq != nil:
			l.seqParams[b.name] = *b.seq
		case b.byRef:
			l.locals[b.name] = b.val
			l.byRefParams[b.val] = true
		default:
			slot := entry.NewAlloca(b.val.Type())
			block.NewStore(b.val, slot)
			l.locals[b.name] = slot
			if b.param.TypeModifier == types.Own {
				pt := l.applyTypeSubst(b.param.Type)
				if l.needsDrop(pt) {
					l.addManagedBinding(slot, pt)
				}
			}
		}
	}
	return block, calleeBase, func() { l.installEnv(saved) }, nil
}

// lowerYieldExpr lowers `yield e`: the value, then the consumer's body on it, then the
// continuation the producer resumes in. The consumer runs in its own environment, with
// a loopCtx whose break leaves this producer and whose continue comes back here.
func (l *lowerer) lowerYieldExpr(block *ir.Block, e *ast.YieldExpr) (value.Value, *ir.Block, error) {
	if l.yield == nil && l.coro == nil {
		return nil, nil, errSeqStage1("a yield outside a producer")
	}
	v, block, err := l.lowerExpr(block, e.Value)
	if err != nil {
		return nil, nil, err
	}
	if diverged(v, block) {
		return nil, block, nil
	}
	cont, err := l.yieldValueTo(block, v)
	return nil, cont, err
}

// lowerYieldFromExpr lowers `yield from s`: every element of s is yielded in turn, so
// s is consumed with this producer's own yield as the consumer.
func (l *lowerer) lowerYieldFromExpr(block *ir.Block, e *ast.YieldFromExpr) (value.Value, *ir.Block, error) {
	if l.yield == nil && l.coro == nil {
		return nil, nil, errSeqStage1("a yield outside a producer")
	}
	srcType, ok := l.recordedType(e.Generator)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: no type recorded for a yield-from source")
	}
	if isSeqType(srcType) {
		end, err := l.lowerSeqConsume(block, e.Generator, func(b *ir.Block, elem value.Value) (*ir.Block, error) {
			return l.yieldValueTo(b, elem)
		})
		return nil, end, err
	}
	// An array, string or range: walk it as a loop whose body yields — the same shape
	// as `for x in s { yield x }`, which is what the spelling abbreviates.
	return nil, nil, errSeqStage1("`yield from` over %s is not lowered yet; write `for x in s { yield x }`", srcType)
}

// yieldValueTo hands v to the current consumer and returns the block the producer
// resumes in.
//
// The consumer is lowered under *its* environment and a loopCtx of its own; the frame
// depth and temp base are the consumer's, so a `break` releases what the producer had
// live as well as the consumer's own. This producer's environment is put back after.
func (l *lowerer) yieldValueTo(block *ir.Block, v value.Value) (*ir.Block, error) {
	// Inside a coroutine body the consumer is whoever resumes it: suspend (stage 2).
	if l.yield == nil {
		return l.lowerCoroYield(block, v)
	}
	cont := block.Parent.NewBlock("")
	if _, err := l.runConsumer(block, l.yield, v, cont); err != nil {
		return nil, err
	}
	// The producer resumes in cont. A statement-level flush after this yield lands
	// there, and cont is dominated by every block above it, so a temporary produced
	// for the yielded value is released on the resumed path — and, through the
	// consumer's tempBase, on a break.
	return cont, nil
}

// runConsumer runs y's consumer on v under the consumer's own environment, with a
// loopCtx whose break leaves the producer and whose continue goes to `next` — the
// yield's continuation for an inlined producer, the loop head for a pulled one. The
// current environment is put back after; the returned block is the consumer's end,
// unterminated when its body fell through.
func (l *lowerer) runConsumer(block *ir.Block, y *seqYield, v value.Value, next *ir.Block) (*ir.Block, error) {
	saved := l.captureEnv()
	l.installEnv(y.env)
	l.loops = append(l.loops, loopCtx{breakTarget: y.exit, continueTarget: next, frameDepth: y.frameDepth, tempBase: y.tempBase})
	end, err := y.consume(block, v)
	l.installEnv(saved)
	if err != nil {
		return nil, err
	}
	if end.Term == nil {
		end.NewBr(next)
		return next, nil
	}
	return next, nil
}

// lowerForInSeq lowers `for x in s { body }` over a sequence: the loop variable is a
// slot the consumer stores each element into, and the body is the consumer. The
// two-variable form counts elements as it goes, since a sequence has no index of its
// own.
func (l *lowerer) lowerForInSeq(block *ir.Block, e *ast.ForInLoopExpr, elemLyra types.Type) (value.Value, *ir.Block, error) {
	defer l.pushLocalScope()()
	elemLL, err := l.lowerType(elemLyra)
	if err != nil {
		return nil, nil, err
	}
	elemVar, indexVar := e.Key, ""
	if e.Value != "" {
		indexVar, elemVar = e.Key, e.Value
	}
	entry := block.Parent.Blocks[0]
	xSlot := entry.NewAlloca(elemLL)
	l.locals[elemVar] = xSlot // a borrow of the element — bound, not framed
	var idxSlot, counter value.Value
	if indexVar != "" {
		idxSlot = entry.NewAlloca(lltypes.I64)
		counter = entry.NewAlloca(lltypes.I64)
		block.NewStore(i64c(0), counter)
		l.locals[indexVar] = idxSlot
	}
	exit, err := l.lowerSeqConsume(block, e.Iterable, func(b *ir.Block, elem value.Value) (*ir.Block, error) {
		b.NewStore(elem, xSlot)
		if idxSlot != nil {
			n := b.NewLoad(lltypes.I64, counter)
			b.NewStore(n, idxSlot)
			b.NewStore(b.NewAdd(n, i64c(1)), counter)
		}
		return l.lowerForEffect(b, e.Body)
	})
	if err != nil {
		return nil, nil, err
	}
	return nil, exit, nil
}

// lowerSeqComp lowers `[x in s | guards | r]` over a sequence. A sequence has no length
// to size the box from, so the box starts empty and every surviving element is pushed —
// the one place a comprehension grows rather than fills, and the reason `to_array` is a
// prelude function over brackets rather than the other way round.
func (l *lowerer) lowerSeqComp(block *ir.Block, e *ast.ArrayCompExpr, gen *ast.Generator, dynType types.DynamicArrayType) (value.Value, *ir.Block, error) {
	elemLL, err := l.lowerType(dynType.ElementType)
	if err != nil {
		return nil, nil, err
	}
	elemSize, elemAlign, ok := SizeAndAlign(l.resolveForLayout(dynType.ElementType))
	if !ok {
		return nil, nil, fmt.Errorf("llvm: cannot size array comprehension element type %s", dynType.ElementType)
	}
	stride := int64(alignUp(elemSize, elemAlign))
	srcElem, ok := l.recordedType(gen.Value)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: no type recorded for a comprehension generator's source")
	}
	srcElemLL, err := l.lowerType(srcElem.(types.ParameterizedType).TypeArguments[0])
	if err != nil {
		return nil, nil, err
	}
	defer l.pushLocalScope()()
	boxTy := DynArrayBoxType(elemLL)
	box := l.dynArrayAlloc(block, boxTy, elemLL, i64c(0), i64c(0), stride)
	fn := block.Parent
	entry := fn.Blocks[0]
	slot := entry.NewAlloca(srcElemLL)
	l.locals[gen.Identifier] = slot

	exit, err := l.lowerSeqConsume(block, gen.Value, func(b *ir.Block, elem value.Value) (*ir.Block, error) {
		b.NewStore(elem, slot)
		skip := fn.NewBlock("")
		cur := b
		for _, guard := range e.Guards {
			v, next, err := l.lowerExpr(cur, guard)
			if err != nil {
				return nil, err
			}
			pass := fn.NewBlock("")
			next.NewCondBr(v, pass, skip)
			cur = pass
		}
		result, cur, err := l.lowerExpr(cur, e.Result)
		if err != nil {
			return nil, err
		}
		if diverged(result, cur) {
			return skip, nil
		}
		result, err = l.coerceAggregateElem(cur, result, elemLL, e.Result)
		if err != nil {
			return nil, err
		}
		cur = l.emitDynArrayPush(cur, boxTy, box, result, stride)
		cur.NewBr(skip)
		return skip, nil
	})
	if err != nil {
		return nil, nil, err
	}
	return box, exit, nil
}
