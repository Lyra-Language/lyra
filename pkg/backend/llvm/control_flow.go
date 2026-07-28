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
	l.pushManagedFrame()
	defer l.popManagedFrame()

	var v value.Value
	last := len(be.Statements) - 1
	for i, stmt := range be.Statements {
		if block.Term != nil {
			break // a prior break/continue/return sealed this block; the rest is unreachable
		}
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
		case *ast.BreakStmt:
			err = l.lowerBreak(block, s)
			v = nil
		case *ast.ContinueStmt:
			err = l.lowerContinue(block, s)
			v = nil
		case *ast.ReturnStmt:
			block, err = l.lowerReturn(block, s)
			v = nil
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
			// since the tail may itself be the escaping block value.
			if i != last || flushTail {
				if err := l.flushTemps(); err != nil {
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
	_, end, err := l.lowerExpr(block, expr)
	if err != nil {
		return nil, err
	}
	if end.Term == nil {
		if err := l.flushTemps(); err != nil {
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
	// looping back (they're rebuilt next iteration).
	if err := l.flushTemps(); err != nil {
		return err
	}
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
	if s.Value == nil {
		return block, l.emitReturn(block, nil)
	}
	v, block, err := l.lowerExpr(block, s.Value)
	if err != nil {
		return nil, err
	}
	return block, l.emitReturn(block, v)
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
	l.loops = append(l.loops, loopCtx{breakTarget: exitBlock, continueTarget: postBlock, label: e.Label, frameDepth: len(l.managedFrames)})
	bodyEnd, err := l.lowerForEffect(bodyBlock, &e.Body)
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
// counter loop). Deferred, loud error: a string iterable.
func (l *lowerer) lowerForInLoop(block *ir.Block, e *ast.ForInLoopExpr) (value.Value, *ir.Block, error) {
	iterType, ok := l.res.TypeTable.Get(e.Iterable)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: no type recorded for for-in iterable")
	}
	// A numeric range (`for i in 0..<n`) is a counter loop, not an element walk.
	if _, ok := iterType.(types.RangeType); ok {
		return l.lowerForInRange(block, e)
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
		length = block.NewLoad(lltypes.I64, block.NewGetElementPtr(boxTy, box, i32c(0), i32c(1)))
		elemLL = elem
		arrGep = func(b *ir.Block, i value.Value) value.Value {
			return b.NewGetElementPtr(boxTy, box, i32c(0), i32c(2), i)
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
	incBlock := fn.NewBlock("") // continue target: advance the counter
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
	l.loops = append(l.loops, loopCtx{breakTarget: exitBlock, continueTarget: incBlock, label: e.Label, frameDepth: len(l.managedFrames)})
	bodyEnd, err := l.lowerForEffect(bodyBlock, &e.Body)
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

// lowerForInRange lowers `for i in START..<END` (and `..=`, with an optional
// `step`) as a counter loop: `i = START; while i </<= END { <body>; i += step }`.
// The counter is the loop variable (a plain integer value, not a borrow). `..<` is
// an exclusive end (`i < END`), `..=` inclusive (`i <= END`).
//
// The counter width is the first concrete-integer bound's type (else i64), matching
// the typechecker's iterableElementType; the bounds and step are coerced to it. The
// increment is a plain (wrapping) add — a range whose end is the counter type's max
// with an inclusive `..=` therefore loops forever (the increment wraps past it), the
// one edge to keep in mind. There is no two-variable form over a range.
func (l *lowerer) lowerForInRange(block *ir.Block, e *ast.ForInLoopExpr) (value.Value, *ir.Block, error) {
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
	var pred enum.IPred
	switch {
	case rng.EndOperator == "<" && signed:
		pred = enum.IPredSLT
	case rng.EndOperator == "<":
		pred = enum.IPredULT
	case signed:
		pred = enum.IPredSLE
	default:
		pred = enum.IPredULE
	}
	condBlock.NewCondBr(condBlock.NewICmp(pred, iv, end), bodyBlock, exitBlock)

	l.loops = append(l.loops, loopCtx{breakTarget: exitBlock, continueTarget: incBlock, label: e.Label, frameDepth: len(l.managedFrames)})
	bodyEnd, err := l.lowerForEffect(bodyBlock, &e.Body)
	l.loops = l.loops[:len(l.loops)-1]
	if err != nil {
		return nil, nil, err
	}
	if bodyEnd.Term == nil {
		bodyEnd.NewBr(incBlock)
	}

	iv2 := incBlock.NewLoad(iType, iSlot)
	incBlock.NewStore(incBlock.NewAdd(iv2, step), iSlot) // plain (wrapping) increment
	incBlock.NewBr(condBlock)

	return nil, exitBlock, nil
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
		t, ok := l.res.TypeTable.Get(ex)
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

func (l *lowerer) lowerVarDecl(block *ir.Block, vds *ast.VarDeclStmt) (*ir.Block, error) {
	init, block, err := l.lowerExpr(block, vds.Value)
	if err != nil {
		return nil, err
	}
	// Alloca in the *entry* block (mem2reg only promotes entry-block allocas).
	entry := block.Parent.Blocks[0]
	slot := entry.NewAlloca(init.Type())
	block.NewStore(init, slot)
	l.locals[vds.Name] = slot // later re-declaration of the same name just overwrites
	// A managed binding owns a reference-counted value (its initializer was
	// coerced to +1 by the ownership pass); record it so this scope's exit
	// releases it, along with the Lyra type that says what its payload owns.
	if isManagedSlot(slot) {
		l.addManagedBinding(slot, l.bindingType(vds))
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
	t, _ := l.res.TypeTable.Get(vds.Value)
	return t
}

func (l *lowerer) lowerVarReassignment(block *ir.Block, vrs *ast.VarReassignmentStmt) (*ir.Block, error) {
	rhsVal, block, err := l.lowerExpr(block, vrs.Value)
	if err != nil {
		return nil, err
	}
	slot := l.locals[vrs.Name]
	// Reassigning a managed binding drops the old value's reference before the new
	// one overwrites it. The new value was coerced to +1 by the ownership pass, and
	// it's computed *before* this release (so `s = s ++ x`, which reads the old s,
	// is safe: the concat has already happened).
	if isManagedSlot(slot) {
		a := slot.(*ir.InstAlloca)
		old := block.NewLoad(a.ElemType, slot)
		// The old and new values have the same type, so the RHS's recorded type
		// selects the right drop glue for the value being dropped.
		oldTy, _ := l.res.TypeTable.Get(vrs.Value)
		if err := l.lowerManagedRelease(block, old, oldTy); err != nil {
			return nil, err
		}
	}
	// Store into the existing alloca; the locals entry stays the alloca slot
	// (a pointer), NOT the stored value — a later read loads from it. Overwriting
	// it with rhsVal would break the next IdentifierExpr load (slot.(*InstAlloca)).
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

	thenVal, thenEnd, err := l.lowerExpr(thenBlock, e.Then)
	if err != nil {
		return nil, nil, err
	}
	thenEnd.NewBr(mergeBlock)

	elseVal, elseEnd, err := l.lowerExpr(elseBlock, e.Else)
	if err != nil {
		return nil, nil, err
	}
	elseEnd.NewBr(mergeBlock)

	// Both incoming values must share an LLVM type for a well-formed phi. The
	// typechecker's branchCommonType guarantees the branches are type-
	// compatible; a genuine width mismatch that slipped through would produce
	// invalid IR that clang rejects (loud), not silently-wrong code.
	phi := mergeBlock.NewPhi(
		ir.NewIncoming(thenVal, thenEnd),
		ir.NewIncoming(elseVal, elseEnd),
	)
	return phi, mergeBlock, nil
}
