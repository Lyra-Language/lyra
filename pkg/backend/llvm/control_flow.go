package llvm

import (
	"fmt"
	"slices"

	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/constant"
	"github.com/llir/llvm/ir/enum"
	lltypes "github.com/llir/llvm/ir/types"
	"github.com/llir/llvm/ir/value"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// loopCtx records the blocks a break/continue in the current loop jumps to. It's
// pushed while lowering a loop body and popped after, so a labeled break/continue
// can walk the stack for its target.
type loopCtx struct {
	breakTarget    *ir.Block // where `break` transfers control (the loop's exit block)
	continueTarget *ir.Block // where `continue` transfers control (the loop's post block)
	label          string    // the loop's label, "" if unlabeled
	frameDepth     int       // len(managedFrames) at loop entry; break/continue release frames from here up
	// tempBase is len(pendingReleases) at loop entry — the temporary-side mirror of
	// frameDepth. break/continue owe releases for temporaries recorded from here up
	// (the statements they jump out of never reach their own flush); one recorded
	// *below* it belongs to a statement enclosing the whole loop, which still flushes
	// after the loop exits, so releasing it at the jump too would be a double free.
	tempBase int
}

// loopTarget returns the loop a break/continue refers to: the innermost loop for
// an empty label, or the nearest enclosing loop with a matching label. The
// typechecker already validates that break/continue sit inside a loop and that
// any label resolves, so a miss here is a backend invariant violation.
func (l *lowerer) loopTarget(label string) (loopCtx, error) {
	if len(l.loops) == 0 {
		return loopCtx{}, fmt.Errorf("llvm: break/continue outside a loop")
	}
	if label == "" {
		return l.loops[len(l.loops)-1], nil
	}
	for _, v := range slices.Backward(l.loops) {
		if v.label == label {
			return v, nil
		}
	}
	return loopCtx{}, fmt.Errorf("llvm: no enclosing loop labeled %q", label)
}

// lowerBlockStmts lowers a block's statements, threading the current block (a
// nested `if`/loop moves control onward) and returning the value of the last
// statement — nil when that's a binding/reassignment/loop or the block sealed
// early via break/continue/return. Break/continue/return terminate a block
// mid-stream, so iteration stops once the block has a terminator rather than
// lowering into a sealed block (invalid IR).
//
// It also manages the ownership bookkeeping for the scope: it pushes a managed
// frame on entry (holding the scope's managed bindings) and, on the fall-through
// exit, releases that frame before popping it. After each statement it runs
// dropLastUsesInStmt (drop fusion — free this scope's bindings whose last use was
// in that statement). Managed temporaries flush at each statement boundary too —
// except, when flushTail is false (a value block), the *last* statement's
// temporaries are held so they propagate to the enclosing statement: the block's
// value may itself be such a temporary, and releasing it here would free it before
// the outer expression consumes it. An effect block (flushTail true, a
// loop/one-armed-if body) discards its tail, so its temporaries are released here
// — they must be, since their SSA values live in per-iteration blocks.
func (l *lowerer) lowerBlockStmts(block *ir.Block, be *ast.BlockExpr, flushTail bool) (value.Value, *ir.Block, error) {
	defer l.pushLocalScope()()
	l.pushManagedFrame()
	defer l.popManagedFrame()
	// Protect any temporaries of an enclosing statement still in flight (this block
	// may be a call argument / `if`-`match` operand): a flush here only touches the
	// temporaries produced at or after this point. Restored on exit.
	savedBase := l.pendingBase
	l.pendingBase = len(l.pendingReleases)
	defer func() { l.pendingBase = savedBase }()

	var v value.Value
	last := len(be.Statements) - 1
	for i, stmt := range be.Statements {
		if block.Term != nil {
			break // a prior break/continue/return sealed this block; the rest is unreachable
		}
		stmtStart := block // the block this statement begins in (before any branch it contains)
		var err error
		switch s := stmt.(type) {
		case *ast.ExpressionStmt:
			v, block, err = l.lowerExpr(block, s.Expression)
		case *ast.VarDeclStmt:
			block, err = l.lowerVarDecl(block, s)
			v = nil // a binding is not itself the block's value
		case *ast.VarReassignmentStmt:
			block, err = l.lowerVarReassignment(block, s)
			v = nil
		case *ast.LValueAssignmentStmt:
			block, err = l.lowerLValueAssignment(block, s)
			v = nil
		case *ast.DerefAssignmentStmt:
			block, err = l.lowerDerefAssignment(block, s)
			v = nil
		case *ast.DestructuringDeclStmt:
			block, err = l.lowerDestructuringDecl(block, s)
			v = nil
		case *ast.IfDestructuringStmt:
			block, err = l.lowerIfDestructuring(block, s)
			v = nil
		case *ast.ElseDestructuringStmt:
			block, err = l.lowerElseDestructuring(block, s)
			v = nil
		case *ast.BreakStmt:
			err = l.lowerBreak(block, s)
			v = nil
		case *ast.ContinueStmt:
			err = l.lowerContinue(block, s)
			v = nil
		case *ast.ReturnStmt:
			block, err = l.lowerReturn(block, s)
			v = nil
		case *ast.WithStmt:
			// Arenas are unimplemented and the typechecker refuses `with` (lyra-E050),
			// so a build never reaches this — an arm of its own only so the message
			// names the diagnostic instead of reading as a lowering merely pending.
			// See the E050 comment for what the phantom cost.
			return nil, nil, fmt.Errorf(
				"llvm: `with` (arena allocation) is not implemented — see lyra-E050")
		default:
			return nil, nil, fmt.Errorf("llvm: block statement lowering not implemented for %T", stmt)
		}
		if err != nil {
			return nil, nil, err
		}
		if block.Term == nil {
			// Drop fusion: release-and-retire this scope's bindings whose last use is
			// a borrow within the statement just lowered (in `block`, which post-
			// dominates the statement). A statement that sealed (an early return) is
			// skipped — the seal's frame release frees its bindings on that path.
			if err := l.dropLastUsesInStmt(block, stmt); err != nil {
				return nil, nil, err
			}
			// Temporaries are held for the tail of a value block (flushTail false)
			// since the tail may itself be the escaping block value. A temp produced on
			// this statement's start block is released at `block` (where the statement
			// ends) so it outlives a later use across a branch; see flushStmtTemps.
			if i != last || flushTail {
				if err := l.flushStmtTemps(stmtStart, block); err != nil {
					return nil, nil, err
				}
			}
		}
	}
	if block.Term == nil {
		if err := l.releaseTopManagedFrame(block); err != nil { // fall-through scope exit
			return nil, nil, err
		}
	}
	return v, block, nil
}

// lowerBlock lowers a value-position block: its value is the value of its last
// statement (matching the typechecker's inferBlockType). It requires that value
// to exist — a block used where a value is needed must end in an expression. The
// tail statement's managed temporaries are not flushed here (flushTail false) —
// the value flows to the enclosing statement, which releases them.
func (l *lowerer) lowerBlock(block *ir.Block, be *ast.BlockExpr) (value.Value, *ir.Block, error) {
	v, end, err := l.lowerBlockStmts(block, be, false)
	if err != nil {
		return nil, nil, err
	}
	if v == nil && end.Term == nil {
		return nil, nil, fmt.Errorf("llvm: block has no value (empty, or last statement is not an expression)")
	}
	return v, end, nil
}

// lowerForEffect lowers an expression for its side effects, discarding any value.
// A block goes through lowerBlockStmts (flushTail true — the discarded tail's
// temporaries are released here, not propagated); any other expression through
// lowerExpr, followed by a temp flush since its value is discarded too.
func (l *lowerer) lowerForEffect(block *ir.Block, expr ast.Expression) (*ir.Block, error) {
	if be, ok := expr.(*ast.BlockExpr); ok {
		_, end, err := l.lowerBlockStmts(block, be, true)
		return end, err
	}
	// An `unsafe` block is its body, here as everywhere: the keyword changes what the
	// front end permits inside it and has no runtime meaning of its own, so a discarded
	// tail's temporaries are released exactly as a plain block's are.
	if ub, ok := expr.(*ast.UnsafeBlockExpr); ok && ub.Body != nil {
		_, end, err := l.lowerBlockStmts(block, ub.Body, true)
		return end, err
	}
	start := block
	_, end, err := l.lowerExpr(block, expr)
	if err != nil {
		return nil, err
	}
	if end.Term == nil {
		if err := l.flushStmtTemps(start, end); err != nil {
			return nil, err
		}
	}
	return end, nil
}

// lowerBreak / lowerContinue transfer control to the target loop's exit / post
// block. The block is sealed by the resulting `br`; lowerBlockStmts stops after.
func (l *lowerer) lowerBreak(block *ir.Block, s *ast.BreakStmt) error {
	if s.Value != nil {
		return fmt.Errorf("llvm: break with a value (loop as expression) not implemented")
	}
	ctx, err := l.loopTarget(s.Label)
	if err != nil {
		return err
	}
	// Before the jump seals the block: release this iteration's pending temporaries
	// and the managed bindings the loop body introduced (the frames from the loop's
	// entry depth up), so break no longer leaks them.
	if err := l.flushTemps(); err != nil {
		return err
	}
	// The temporaries of the statements this jump leaves behind — an `if` condition's,
	// say — are not in [pendingBase:] and so are untouched by the flush above: they
	// belong to statements still being lowered, whose own flushes this jump will never
	// reach. Whether each is live *here* is a dominance question the CFG cannot answer
	// yet, so the obligation is recorded and settled once the body is complete.
	l.recordExitReleases(block, ctx.tempBase)
	if err := l.releaseManagedFramesFrom(block, ctx.frameDepth); err != nil {
		return err
	}
	block.NewBr(ctx.breakTarget)
	return nil
}

func (l *lowerer) lowerContinue(block *ir.Block, s *ast.ContinueStmt) error {
	ctx, err := l.loopTarget(s.Label)
	if err != nil {
		return err
	}
	// Same as break: the current iteration's managed bindings are released before
	// looping back (they're rebuilt next iteration), and the skipped statements'
	// temporaries are recorded for the post-body dominance pass.
	if err := l.flushTemps(); err != nil {
		return err
	}
	l.recordExitReleases(block, ctx.tempBase)
	if err := l.releaseManagedFramesFrom(block, ctx.frameDepth); err != nil {
		return err
	}
	block.NewBr(ctx.continueTarget)
	return nil
}

// lowerReturn lowers an explicit `return [value]`, emitting the `ret` via
// emitReturn (which coerces to the current function's return type) and sealing
// the block. Returns the block the value evaluation ended in — a value
// containing an `if` moves control onward before the `ret` — so lowerBlockStmts
// sees a sealed block and stops.
func (l *lowerer) lowerReturn(block *ir.Block, s *ast.ReturnStmt) (*ir.Block, error) {
	start := block // where the returned expression begins (before any branch it contains)
	if s.Value == nil {
		return block, l.emitReturn(start, block, nil)
	}
	v, block, err := l.lowerExpr(block, s.Value)
	if err != nil {
		return nil, err
	}
	return block, l.emitReturn(start, block, v)
}

// lowerForLoop lowers a C-style `for` loop to the standard cond/body/post/exit
// CFG with a back-edge:
//
//	init (once, current block)
//	br cond
//	cond: <condition> ; cond_br body, exit   (nil condition → br body: infinite loop)
//	body: <body for effect> ; br post
//	post: <post> ; br cond                    (continue targets post, so it runs)
//	exit: ...                                 (break targets exit; control continues here)
//
// A loop is a statement (no value), so it returns a nil value and the exit block.
//
// Every fall-through `br` is guarded by `end.Term == nil`: a body (or post) that
// ended in break/continue/return has already sealed its block, and emitting a
// second terminator would be invalid IR.
//
// All three forms reach here: infinite (`for {}`, nil condition → an
// unconditional branch into the body), condition-only (`for cond {}`), and the
// three-clause `for var i = 0; i < n; i += 1` (Init via lowerVarDecl, Post a
// MathAssignOpExpr via lowerMathAssignOp).
func (l *lowerer) lowerForLoop(block *ir.Block, e *ast.ForLoopExpr) (value.Value, *ir.Block, error) {
	// The loop variable (and a C-style init's counter) belongs to the loop, not to
	// whatever follows it: scope its binding here so it cannot outlive the loop or
	// permanently shadow an outer binding of the same name.
	defer l.pushLocalScope()()
	if e.Init != nil {
		var err error
		if block, err = l.lowerVarDecl(block, e.Init); err != nil {
			return nil, nil, err
		}
	}

	fn := block.Parent
	condBlock := fn.NewBlock("")
	bodyBlock := fn.NewBlock("")
	postBlock := fn.NewBlock("") // continue target; brs to cond (with the post effect, if any)
	exitBlock := fn.NewBlock("") // break target; where control continues after the loop
	block.NewBr(condBlock)

	// Condition. A nil condition is an infinite loop (exit only via break), so it
	// branches unconditionally into the body.
	if e.Condition != nil {
		condVal, condEnd, err := l.lowerExpr(condBlock, *e.Condition)
		if err != nil {
			return nil, nil, err
		}
		condEnd.NewCondBr(condVal, bodyBlock, exitBlock)
	} else {
		condBlock.NewBr(bodyBlock)
	}

	// Body, lowered for effect with this loop pushed so break/continue resolve.
	// frameDepth is the managed-frame stack height here (before the body pushes its
	// own frame), so a break/continue releases exactly the frames the loop body (and
	// any nested block) introduced — the current iteration's managed bindings —
	// without touching the loop variable or enclosing scopes.
	l.loops = append(l.loops, loopCtx{breakTarget: exitBlock, continueTarget: postBlock, label: e.Label, frameDepth: len(l.managedFrames), tempBase: len(l.pendingReleases)})
	bodyEnd, err := l.lowerForEffect(bodyBlock, e.Body)
	l.loops = l.loops[:len(l.loops)-1]
	if err != nil {
		return nil, nil, err
	}
	if bodyEnd.Term == nil {
		bodyEnd.NewBr(postBlock)
	}

	// Post, then back to the condition.
	if e.Post != nil {
		postEnd, err := l.lowerForEffect(postBlock, *e.Post)
		if err != nil {
			return nil, nil, err
		}
		if postEnd.Term == nil {
			postEnd.NewBr(condBlock)
		}
	} else {
		postBlock.NewBr(condBlock)
	}

	return nil, exitBlock, nil
}

// lowerForInLoop lowers `for x in <array>` as an index-counter loop over the
// array's elements: `i = 0; while i < len { x = arr[i]; <body>; i++ }`. It handles
// a fixed-size array (`[N]T`, stack or `shared`) and a dynamic array (`[]T`); the
// length is the compile-time size or the box's runtime `len` accordingly.
//
// The element variable is a **borrow** of the element — the array still owns it — so
// it is bound into l.locals but *not* framed for release (reading an element consumes
// no reference; for a managed element type the array frees it when the array itself
// dies). Managed values *declared inside* the body are framed/released per iteration
// by the ordinary block machinery, exactly as in the C-style loop.
//
// The two-variable form `for i, x in xs` binds the loop counter as the index `i`
// (i64) in addition to the element `x`; the collector puts the first name in Key
// (the index) and the second in Value (the element), and the single-variable form
// leaves Value empty (Key is then the element).
//
// A numeric range iterable (`for i in 0..<n`) is delegated to lowerForInRange (a
// counter loop) and a string iterable to lowerForInString (yields runes).
func (l *lowerer) lowerForInLoop(block *ir.Block, e *ast.ForInLoopExpr) (value.Value, *ir.Block, error) {
	// The loop variable (and a C-style init's counter) belongs to the loop, not to
	// whatever follows it: scope its binding here so it cannot outlive the loop or
	// permanently shadow an outer binding of the same name.
	defer l.pushLocalScope()()
	iterType, ok := l.recordedType(e.Iterable)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: no type recorded for for-in iterable")
	}
	// A numeric range (`for i in 0..<n`) is a counter loop, not an element walk.
	if _, ok := iterType.(types.RangeType); ok {
		return l.lowerForInRange(block, e)
	}
	// A string yields its runes (UTF-8 decoded), not array elements.
	if types.IsString(iterType) {
		return l.lowerForInString(block, e)
	}

	// Resolve the element/index variable names from the one/two-variable form.
	elemVar, indexVar := e.Key, ""
	if e.Value != "" {
		indexVar, elemVar = e.Key, e.Value
	}

	// Materialize the element source and length *once*, before the loop; arrGep(i)
	// returns a `T*` to the i-th element for a runtime index i (valid in the body
	// because `block` dominates it).
	var length value.Value
	var elemLL lltypes.Type
	var arrGep func(b *ir.Block, i value.Value) value.Value

	switch it := iterType.(type) {
	case types.StaticArrayType:
		var arrPtr value.Value
		var arrayTy *lltypes.ArrayType
		var err error
		if types.AllocationOf(iterType) == types.Shared {
			arrPtr, arrayTy, block, err = l.sharedArrayPayloadPtr(block, e.Iterable)
		} else {
			arrPtr, arrayTy, block, err = l.arrayLValue(block, e.Iterable)
		}
		if err != nil {
			return nil, nil, err
		}
		length = constant.NewInt(lltypes.I64, int64(it.Size))
		elemLL = arrayTy.ElemType
		arrGep = func(b *ir.Block, i value.Value) value.Value {
			return b.NewGetElementPtr(arrayTy, arrPtr, constant.NewInt(lltypes.I64, 0), i)
		}
	case types.DynamicArrayType:
		elem, err := l.lowerType(it.ElementType)
		if err != nil {
			return nil, nil, err
		}
		boxTy := DynArrayBoxType(elem)
		var box value.Value
		box, block, err = l.lowerExpr(block, e.Iterable)
		if err != nil {
			return nil, nil, err
		}
		length = block.NewLoad(lltypes.I64, dynArrayLenPtr(block, boxTy, box))
		elemLL = elem
		arrGep = func(b *ir.Block, i value.Value) value.Value {
			return dynArrayElemPtr(b, boxTy, box, i)
		}
	default:
		return nil, nil, fmt.Errorf("llvm: for-in over %s not implemented yet (arrays only)", iterType)
	}

	fn := block.Parent
	entry := fn.Blocks[0]
	iSlot := entry.NewAlloca(lltypes.I64)
	xSlot := entry.NewAlloca(elemLL)
	block.NewStore(constant.NewInt(lltypes.I64, 0), iSlot)
	l.locals[elemVar] = xSlot // borrow of the element — bound, not framed
	var idxSlot value.Value
	if indexVar != "" {
		idxSlot = entry.NewAlloca(lltypes.I64) // the index `i` (a copy of the counter)
		l.locals[indexVar] = idxSlot
	}

	condBlock := fn.NewBlock("")
	bodyBlock := fn.NewBlock("")
	incBlock := fn.NewBlock("")  // continue target: advance the counter
	exitBlock := fn.NewBlock("") // break target
	block.NewBr(condBlock)

	i := condBlock.NewLoad(lltypes.I64, iSlot)
	condBlock.NewCondBr(condBlock.NewICmp(enum.IPredSLT, i, length), bodyBlock, exitBlock)

	// Bind the element (and, for the two-variable form, the index) to this
	// iteration's values, then lower the body for effect.
	ib := bodyBlock.NewLoad(lltypes.I64, iSlot)
	bodyBlock.NewStore(bodyBlock.NewLoad(elemLL, arrGep(bodyBlock, ib)), xSlot)
	if idxSlot != nil {
		bodyBlock.NewStore(ib, idxSlot)
	}
	l.loops = append(l.loops, loopCtx{breakTarget: exitBlock, continueTarget: incBlock, label: e.Label, frameDepth: len(l.managedFrames), tempBase: len(l.pendingReleases)})
	bodyEnd, err := l.lowerForEffect(bodyBlock, e.Body)
	l.loops = l.loops[:len(l.loops)-1]
	if err != nil {
		return nil, nil, err
	}
	if bodyEnd.Term == nil {
		bodyEnd.NewBr(incBlock)
	}

	ic := incBlock.NewLoad(lltypes.I64, iSlot)
	incBlock.NewStore(incBlock.NewAdd(ic, constant.NewInt(lltypes.I64, 1)), iSlot)
	incBlock.NewBr(condBlock)

	return nil, exitBlock, nil
}

// lowerForInRange lowers `for i in START..<END` (and `..<=`, with an optional
// `step`) as a counter loop: `i = START; while i </<= END { <body>; i += step }`.
// The counter is the loop variable (a plain integer value, not a borrow). `..<` is
// an exclusive end (`i < END`), `..<=` inclusive (`i <= END`).
//
// The counter width is the first concrete-integer bound's type (else i64), matching
// the typechecker's iterableElementType; the bounds and step are coerced to it. The
// advance is guarded rather than a plain add (see the inc block below): a step that
// would cross the end bound exits the loop instead of wrapping at the type's edge, so
// `for i in 0..<=hi` terminates when hi is the counter type's max — until 08/12 the
// increment wrapped past it and the loop ran forever, a silent infinite loop in the
// language whose arithmetic traps on exactly that wrap. A step of zero or less that is
// only known at run time traps for the same reason (the constant form is refused at
// check time). There is no two-variable form over a range.
func (l *lowerer) lowerForInRange(block *ir.Block, e *ast.ForInLoopExpr) (value.Value, *ir.Block, error) {
	// The loop variable (and a C-style init's counter) belongs to the loop, not to
	// whatever follows it: scope its binding here so it cannot outlive the loop or
	// permanently shadow an outer binding of the same name.
	defer l.pushLocalScope()()
	if e.Value != "" {
		return nil, nil, fmt.Errorf("llvm: a range has no index/value pair — `for i, x in <range>` is not valid")
	}
	rng, ok := e.Iterable.(*ast.RangeExpr)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: for-in range iterable is not a range expression (%T)", e.Iterable)
	}
	iType, signed := l.rangeIntType(rng)

	// Lower each bound / the step and width-normalize it to the counter type.
	coerce := func(b *ir.Block, ex ast.Expression) (value.Value, *ir.Block, error) {
		v, b, err := l.lowerExpr(b, ex)
		if err != nil {
			return nil, nil, err
		}
		vi, ok := v.Type().(*lltypes.IntType)
		if !ok {
			return nil, nil, fmt.Errorf("llvm: range bound is not an integer (%s)", v.Type())
		}
		if vi.BitSize != iType.BitSize {
			vs, _ := l.getIntSignedness(ex)
			v = coerceIntWidth(b, v, vs, iType)
		}
		return v, b, nil
	}
	start, block, err := coerce(block, rng.Start)
	if err != nil {
		return nil, nil, err
	}
	end, block, err := coerce(block, rng.End)
	if err != nil {
		return nil, nil, err
	}
	var step value.Value = constant.NewInt(iType, 1)
	if rng.Step != nil {
		if step, block, err = coerce(block, rng.Step); err != nil {
			return nil, nil, err
		}
		// A non-positive step never advances, so the loop below would spin forever —
		// silently, which is the answer this language exists to rule out. The rule is
		// the one a shift amount already rides: provable at check time → compile error
		// (types.InvalidStepReason refuses a constant zero or negative step), only
		// knowable at run time → trap. A non-positive *constant* reaching this far is a
		// front-end failure, reported loudly rather than guarded around (rule 5).
		if ci, isConst := step.(*constant.Int); isConst {
			if ci.X.Sign() <= 0 {
				return nil, nil, fmt.Errorf("llvm: a non-positive constant range step (%s) survived the typechecker", ci.X)
			}
		} else {
			var bad value.Value
			if signed {
				bad = block.NewICmp(enum.IPredSLE, step, constant.NewInt(iType, 0))
			} else {
				// An unsigned step cannot be negative; zero is the only bad value.
				bad = block.NewICmp(enum.IPredEQ, step, constant.NewInt(iType, 0))
			}
			block = l.emitTrapIf(block, bad, l.panicRangeStepFunc())
		}
	}

	fn := block.Parent
	entry := fn.Blocks[0]
	iSlot := entry.NewAlloca(iType)
	block.NewStore(start, iSlot)
	l.locals[e.Key] = iSlot // the counter *is* the loop variable (immutable, so never re-stored by the body)

	condBlock := fn.NewBlock("")
	bodyBlock := fn.NewBlock("")
	incBlock := fn.NewBlock("")  // continue target
	exitBlock := fn.NewBlock("") // break target
	block.NewBr(condBlock)

	iv := condBlock.NewLoad(iType, iSlot)
	// The operator decides the comparison in both axes: which way the range runs, and
	// whether the end bound is inside it. A descending range tests `>`/`>=` against the
	// end and steps *down* — the step is a magnitude, so the sign lives here rather than
	// in the value the author wrote (types.InvalidStepReason refuses a negative one).
	pred := rangeLoopPredicate(rng.EndOperator, signed)
	condBlock.NewCondBr(condBlock.NewICmp(pred, iv, end), bodyBlock, exitBlock)

	l.loops = append(l.loops, loopCtx{breakTarget: exitBlock, continueTarget: incBlock, label: e.Label, frameDepth: len(l.managedFrames), tempBase: len(l.pendingReleases)})
	bodyEnd, err := l.lowerForEffect(bodyBlock, e.Body)
	l.loops = l.loops[:len(l.loops)-1]
	if err != nil {
		return nil, nil, err
	}
	if bodyEnd.Term == nil {
		bodyEnd.NewBr(incBlock)
	}

	// The advance is guarded: the counter moves only when it can move by `step` and
	// stay inside the range, and the loop exits otherwise — an unguarded add wraps at
	// the type's edge, which turned `for i in 0..<=hi` with hi at the type's max into
	// a silent infinite loop (255 → 0 over u8), and a large step did the same to an
	// *exclusive* end by leaping the bound entirely (`0..<250:100` over u8: 200 + 100
	// wraps to 44, still under 250).
	//
	// `dist` is the distance to the end bound measured along the iteration direction.
	// The cond block has already held, so the counter is on the range's side of the
	// end and the raw two's-complement difference *is* that distance; the comparison
	// is unsigned at every counter type, because a signed subtraction could itself
	// overflow (end = MAX, i = MIN spans the whole domain). An exclusive end continues
	// on step < dist — the next value must land strictly inside — and an inclusive
	// one on step <= dist, since it may land on the end itself.
	iv2 := incBlock.NewLoad(iType, iSlot)
	descending := types.RangeDescends(rng.EndOperator)
	var dist value.Value
	if descending {
		dist = incBlock.NewSub(iv2, end)
	} else {
		dist = incBlock.NewSub(end, iv2)
	}
	contPred := enum.IPredULE
	if types.RangeExcludesEnd(rng.EndOperator) {
		contPred = enum.IPredULT
	}
	advBlock := fn.NewBlock("")
	incBlock.NewCondBr(incBlock.NewICmp(contPred, step, dist), advBlock, exitBlock)
	// The step is a magnitude — its direction is the operator's, not the value's.
	if descending {
		advBlock.NewStore(advBlock.NewSub(iv2, step), iSlot)
	} else {
		advBlock.NewStore(advBlock.NewAdd(iv2, step), iSlot)
	}
	advBlock.NewBr(condBlock)

	return nil, exitBlock, nil
}

// rangeLoopPredicate is the comparison a range loop continues on, from the end operator's
// two axes: direction picks `<`/`>`, and whether the end is included picks the `=`.
//
// Signedness comes from the counter's type, not the operator — an unsigned descending range
// is perfectly ordinary (`for i in 5u8..>=0`), and using a signed predicate for it would
// make the loop's last iteration depend on a wrap the author never wrote.
func rangeLoopPredicate(endOperator string, signed bool) enum.IPred {
	descending := types.RangeDescends(endOperator)
	exclusive := types.RangeExcludesEnd(endOperator)
	switch {
	case descending && exclusive && signed:
		return enum.IPredSGT
	case descending && exclusive:
		return enum.IPredUGT
	case descending && signed:
		return enum.IPredSGE
	case descending:
		return enum.IPredUGE
	case exclusive && signed:
		return enum.IPredSLT
	case exclusive:
		return enum.IPredULT
	case signed:
		return enum.IPredSLE
	default:
		return enum.IPredULE
	}
}

// rangeIntType returns the LLVM integer type and signedness a range for-in's
// counter uses: the first concrete-integer bound's type (End, then Start, then
// Step), or i64/signed when every bound is an untyped literal — mirroring the
// typechecker's iterableElementType so the counter matches the loop variable's type.
func (l *lowerer) rangeIntType(rng *ast.RangeExpr) (*lltypes.IntType, bool) {
	for _, ex := range []ast.Expression{rng.End, rng.Start, rng.Step} {
		if ex == nil {
			continue
		}
		t, ok := l.recordedType(ex)
		if !ok {
			continue
		}
		p, ok := t.(types.PrimitiveType)
		if !ok || p.Name == types.UntypedInt || p.Name == types.UntypedSignedInt {
			continue
		}
		if ll, ok := LLVMPrimitive(p.Name); ok {
			if it, ok := ll.(*lltypes.IntType); ok {
				return it, IsSignedInt(p.Name)
			}
		}
	}
	return lltypes.I64, true
}

// lowerForInString lowers `for c in <string>` as a rune walk over the string's UTF-8
// bytes: `bi = 0; while bi < byteLen { c = decode(data, bi); <body>; bi += n }`,
// where each iteration decodes one rune (lyra_utf8_decode → the code point + the
// byte count n) and advances the byte index by that count. The loop variable is the
// rune (an i32 value, not a borrow — nothing to free). `n` is computed at the top of
// the body block, which dominates the continue/increment block, so advancing by it
// is valid on both the fall-through and `continue` paths.
//
// The two-variable form `for i, c in s` (08/12) binds the **rune index** alongside
// the rune — a counter incremented with the byte cursor, so the pair costs the same
// single linear walk. This is the indexed traversal that replaces
// `for i in 0..<s.len() { s[i] }`, whose every `s[i]` decodes from the start (O(n²))
// — the loop the docs used to hold up as what rune-count `len` protects, and the
// audit's last standing tension. The convention matches arrays: first name is the
// index, second the element.
func (l *lowerer) lowerForInString(block *ir.Block, e *ast.ForInLoopExpr) (value.Value, *ir.Block, error) {
	// The loop variable (and a C-style init's counter) belongs to the loop, not to
	// whatever follows it: scope its binding here so it cannot outlive the loop or
	// permanently shadow an outer binding of the same name.
	defer l.pushLocalScope()()
	str, block, err := l.lowerExpr(block, e.Iterable)
	if err != nil {
		return nil, nil, err
	}
	if !isStringLLVMType(str.Type()) {
		return nil, nil, fmt.Errorf("llvm: string for-in iterable did not lower to a string (%s)", str.Type())
	}
	data := block.NewExtractValue(str, 0)
	length := block.NewExtractValue(str, 1) // byte length
	decode := l.utf8DecodeFunc()

	fn := block.Parent
	entry := fn.Blocks[0]
	biSlot := entry.NewAlloca(lltypes.I64) // byte index
	cSlot := entry.NewAlloca(lltypes.I32)  // the rune loop variable
	cpSlot := entry.NewAlloca(lltypes.I32) // decode out-param
	block.NewStore(constant.NewInt(lltypes.I64, 0), biSlot)
	runeVar := e.Key
	var riSlot value.Value
	if e.Value != "" {
		// Two-variable form: Key is the rune index, Value the rune (the array
		// convention). The index is its own counter — the byte cursor cannot stand
		// in for it, since a multi-byte rune advances the bytes by more than one.
		riSlot = entry.NewAlloca(lltypes.I64)
		block.NewStore(constant.NewInt(lltypes.I64, 0), riSlot)
		l.locals[e.Key] = riSlot
		runeVar = e.Value
	}
	l.locals[runeVar] = cSlot // the rune value (immutable, non-managed — no ownership)

	condBlock := fn.NewBlock("")
	bodyBlock := fn.NewBlock("")
	incBlock := fn.NewBlock("")  // continue target
	exitBlock := fn.NewBlock("") // break target
	block.NewBr(condBlock)

	bi := condBlock.NewLoad(lltypes.I64, biSlot)
	condBlock.NewCondBr(condBlock.NewICmp(enum.IPredULT, bi, length), bodyBlock, exitBlock)

	// Decode the rune at the current byte index, bind it, keep the byte advance `n`.
	biB := bodyBlock.NewLoad(lltypes.I64, biSlot)
	n := bodyBlock.NewCall(decode, data, biB, cpSlot)
	bodyBlock.NewStore(bodyBlock.NewLoad(lltypes.I32, cpSlot), cSlot)
	l.loops = append(l.loops, loopCtx{breakTarget: exitBlock, continueTarget: incBlock, label: e.Label, frameDepth: len(l.managedFrames), tempBase: len(l.pendingReleases)})
	bodyEnd, err := l.lowerForEffect(bodyBlock, e.Body)
	l.loops = l.loops[:len(l.loops)-1]
	if err != nil {
		return nil, nil, err
	}
	if bodyEnd.Term == nil {
		bodyEnd.NewBr(incBlock)
	}

	biI := incBlock.NewLoad(lltypes.I64, biSlot)
	incBlock.NewStore(incBlock.NewAdd(biI, n), biSlot) // advance by the decoded byte count
	if riSlot != nil {
		ri := incBlock.NewLoad(lltypes.I64, riSlot)
		incBlock.NewStore(incBlock.NewAdd(ri, constant.NewInt(lltypes.I64, 1)), riSlot)
	}
	incBlock.NewBr(condBlock)

	return nil, exitBlock, nil
}

func (l *lowerer) lowerVarDecl(block *ir.Block, vds *ast.VarDeclStmt) (*ir.Block, error) {
	init, block, err := l.lowerExpr(block, vds.Value)
	if err != nil {
		return nil, err
	}
	// `let x = panic("…")`: the initializer diverged, so there is no value to store
	// and nothing after this binding can run. Returning early leaves the block sealed
	// for lowerBlockStmts to stop on; without it, init.Type() dereferenced a nil.
	if diverged(init, block) {
		return block, nil
	}
	// A **void** initializer is the other way a value can be missing, and it is not
	// the same case: `diverged` above means control never gets here, whereas this
	// means control does get here with nothing to store — `let r = if c { x = 1 }
	// else { x = 2 }`, or the same shape written as a `match`. The typechecker does
	// not reject binding a void expression today, so the backend has to, and it must
	// do so as an error rather than by dereferencing the nil: `init.Type()` below
	// segfaulted the compiler on exactly this input, which is the "never panic on a
	// well-typed program" invariant rather than a missing feature.
	if init == nil {
		return nil, fmt.Errorf("llvm: %q is bound to an expression that produces no value "+
			"(its branches or arms end in a statement rather than an expression)", vds.Name)
	}
	// Alloca in the *entry* block (mem2reg only promotes entry-block allocas).
	entry := block.Parent.Blocks[0]
	slot := entry.NewAlloca(init.Type())
	block.NewStore(init, slot)
	l.locals[vds.Name] = slot // later re-declaration of the same name just overwrites
	// An *owning* binding holds a +1 on everything it owns — its initializer was
	// coerced to +1 by the ownership pass (bindingOwnsManaged makes the initializer an
	// owning position, so a copy there was deep-retained). Record it so this scope's
	// exit deep-releases it, along with the Lyra type saying what that is. This covers
	// a plain stack aggregate with a managed field, not just a managed value: the two
	// must be framed on the same condition the pass grants the +1 on, or they'd
	// disagree.
	if ty := l.bindingType(vds); l.needsDrop(ty) {
		l.addManagedBinding(slot, ty)
	}
	return block, nil
}

// bindingType is the Lyra type a `let`/`var` binding holds: its annotation when
// there is one (most reliable), else the type recorded for its initializer. It
// feeds the drop glue selection at the binding's release.
func (l *lowerer) bindingType(vds *ast.VarDeclStmt) types.Type {
	if vds.Type != nil {
		return vds.Type
	}
	t, _ := l.recordedType(vds.Value)
	return t
}

func (l *lowerer) lowerVarReassignment(block *ir.Block, vrs *ast.VarReassignmentStmt) (*ir.Block, error) {
	rhsVal, block, err := l.lowerExpr(block, vrs.Value)
	if err != nil {
		return nil, err
	}
	// `x = panic("…")`: nothing to assign, and the old value's drop is moot — control
	// does not reach past the panic. Same shape as lowerVarDecl's guard.
	if diverged(rhsVal, block) {
		return block, nil
	}
	slot, _ := l.slotFor(vrs.Name)
	// Reassigning an *owning* binding drops what the old value owned before the new one
	// overwrites it. The new value was coerced to +1 by the ownership pass, and it's
	// computed *before* this release (so `s = s ++ x`, which reads the old s, is safe:
	// the concat has already happened). Deep — a stack aggregate holding a string
	// releases that string here, sound now that every copy carries its own +1.
	//
	// The slotIsOwning guard is what makes "owning" real rather than assumed. This used
	// to release whenever the type needed a drop, which is wrong for a **borrowed**
	// binding: a by-value `string` parameter is a borrow whose copy shares the caller's
	// reference, so `(s: string) => { s = "l" ++ "1"  s }` freed the argument while the
	// caller still owned it and was about to release it again — an ASan-confirmed
	// heap-use-after-free (07/30). The comment here already claimed the release happened
	// "on the same condition lowerVarDecl framed the binding"; it simply wasn't checked.
	// A by-reference `mut` parameter *is* owning (its slot is the caller's storage), so
	// writing through one still releases, which is what makes that path balanced.
	oldTy, _ := l.recordedType(vrs.Value)
	if l.needsDrop(oldTy) && l.slotIsOwning(slot) {
		elem, err := slotElemType(slot)
		if err != nil {
			return nil, err
		}
		// The old and new values have the same type, so the RHS's recorded type
		// selects the right glue for the value being dropped.
		old := block.NewLoad(elem, slot)
		if err := l.deepRelease(block, old, oldTy); err != nil {
			return nil, err
		}
	}
	// Store into the existing slot (a pointer), NOT the stored value — a later read
	// loads from it. For a by-reference `mut` parameter the slot is the caller's
	// storage, so this store is exactly how a whole-binding reassignment reaches
	// the caller.
	block.NewStore(rhsVal, slot)
	return block, nil
}

// lowerIf lowers an if/else expression to the standard four-block diamond with
// a phi at the merge:
//
//	         cond br
//	current ─────────┬──> then ──br──┐
//	                 └──> else ──br──┴──> merge: phi [thenVal, thenEnd], [elseVal, elseEnd]
//
// Each branch computes its value, then jumps to a shared merge block whose phi
// selects the result based on which predecessor control arrived from — and that
// phi is the if-expression's value.
//
// A two-armed `if` is a value (both branches feed the phi). A one-armed `if`
// (no `else`) only reaches here as a statement — the typechecker rejects a
// one-armed `if` in value position (checkIfExpr) — so it produces no value: the
// false edge goes straight to merge and there is no phi. This is what lets
// `if cond { break }` lower inside a loop body.
//
// The phi's incoming predecessors and the branches into merge use the block
// each branch *ends in* (thenEnd/elseEnd), NOT the block we started it in: a
// branch whose body contains its own `if` will have moved control into a
// different block by the time it produces its value. Using the start block here
// would be the classic phi/branch bug.
func (l *lowerer) lowerIf(block *ir.Block, e *ast.IfExpr) (value.Value, *ir.Block, error) {
	if e.Condition == nil || e.Then == nil {
		return nil, nil, fmt.Errorf("llvm: if lowering requires a condition and a then branch")
	}
	cond, block, err := l.lowerExpr(block, e.Condition)
	if err != nil {
		return nil, nil, err
	}
	fn := block.Parent

	// One-armed `if` as a statement: no value, no phi. The then branch is lowered
	// for effect and may seal its own block (a `break`/`continue`), so the
	// fall-through to merge is guarded.
	if e.Else == nil {
		thenBlock := fn.NewBlock("")
		mergeBlock := fn.NewBlock("")
		block.NewCondBr(cond, thenBlock, mergeBlock)
		thenEnd, err := l.lowerForEffect(thenBlock, e.Then)
		if err != nil {
			return nil, nil, err
		}
		if thenEnd.Term == nil {
			thenEnd.NewBr(mergeBlock)
		}
		return nil, mergeBlock, nil
	}

	thenBlock := fn.NewBlock("")
	elseBlock := fn.NewBlock("")
	mergeBlock := fn.NewBlock("")
	block.NewCondBr(cond, thenBlock, elseBlock)

	// Each branch is lowered value-optionally (lowerBranchValue): a branch that ends
	// in `return`/`break`/`continue` seals its own block and reaches neither the
	// merge nor the phi; a *void* branch — both branches side-effecting, `if c { a() }
	// else { b() }` used as a statement — reaches the merge but yields no value.
	// Guarding `NewBr` on `Term == nil` is what keeps a sealed block's real
	// terminator from being clobbered and a nil from being fed into `NewPhi` (which
	// panicked on a diverging branch like `if c { return x } else { y }`).
	thenVal, thenEnd, err := l.lowerBranchValue(thenBlock, e.Then)
	if err != nil {
		return nil, nil, err
	}
	elseVal, elseEnd, err := l.lowerBranchValue(elseBlock, e.Else)
	if err != nil {
		return nil, nil, err
	}

	thenReaches := thenEnd.Term == nil
	if thenReaches {
		thenEnd.NewBr(mergeBlock)
	}
	elseReaches := elseEnd.Term == nil
	if elseReaches {
		elseEnd.NewBr(mergeBlock)
	}

	// No value when a reaching branch is void (the `if` is a statement), or when
	// neither branch reaches (both diverged — the merge is unreachable, terminated by
	// downstream lowering). In value position the typechecker guarantees both
	// branches produce a compatible value, so only the phi cases below remain.
	//
	// **"Void" has two spellings here and both have to be caught.** A branch that
	// produces nothing hands back a nil, which is what a builtin like `set_raw_mode`
	// does — but a call to a *user-defined* `void` function hands back the `ir.Call`
	// itself, non-nil with a void type. Testing only for nil built `phi void` from
	// `if b { f() } else { g() }`, six ordinary lines that clang rejects outright
	// ("void type only allowed for function results"). The comment above already
	// anticipated void branches; it just did not anticipate that one of them is not nil.
	if (thenReaches && isVoidResult(thenVal)) || (elseReaches && isVoidResult(elseVal)) {
		return nil, mergeBlock, nil
	}
	var incomings []*ir.Incoming
	if thenReaches {
		incomings = append(incomings, ir.NewIncoming(thenVal, thenEnd))
	}
	if elseReaches {
		incomings = append(incomings, ir.NewIncoming(elseVal, elseEnd))
	}
	if len(incomings) == 0 {
		return nil, mergeBlock, nil
	}
	// The incoming values (when both reach) share an LLVM type — the typechecker's
	// branchCommonType guarantees the branches are type-compatible; a width mismatch
	// that slipped through yields invalid IR clang rejects (loud), not wrong code.
	return mergeBlock.NewPhi(incomings...), mergeBlock, nil
}

// isVoidResult reports whether v carries no usable value — either nothing at all, or a
// call whose LLVM result type is void.
//
// The two are the same thing to a caller and different things to `llir`, which is what
// made the distinction worth a named predicate rather than an inline test: anything that
// feeds a branch result into a phi, a store or an aggregate has to ask this question, and
// asking it as `== nil` is right until the day the branch calls a `void` function.
func isVoidResult(v value.Value) bool {
	if v == nil {
		return true
	}
	_, isVoid := v.Type().(*lltypes.VoidType)
	return isVoid
}

// lowerBranchValue lowers a two-armed `if` branch, tolerating a *void* branch (a
// block whose last statement produces no value — both branches side-effecting, the
// `if` used as a statement) by returning a nil value instead of erroring. A block
// goes through lowerBlockStmts with flushTail false, exactly like a value block: a
// value branch's tail escapes to the phi, and a void branch's tail temporaries are
// released by the enclosing statement's flush in their (conditional) production
// block. A non-block branch goes through lowerExpr.
func (l *lowerer) lowerBranchValue(block *ir.Block, branch ast.Expression) (value.Value, *ir.Block, error) {
	if be, ok := branch.(*ast.BlockExpr); ok {
		return l.lowerBlockStmts(block, be, false)
	}
	return l.lowerExpr(block, branch)
}
