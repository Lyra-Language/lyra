package llvm

import (
	"slices"

	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/value"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// This file lowers the ownership model's scope-exit releases. The per-expression
// retains and temporary releases live in lowerExpr / flushTempReleases (llvm.go);
// this is the other half — releasing a managed binding's owning reference when
// its scope exits.
//
// managedFrames is a stack of scope frames. beginFunction seeds it with one root
// frame (for `own` params); lowerBlockStmts pushes a frame per block. A managed
// (string) binding's alloca is recorded in the current top frame when it's
// declared. A frame is released — each binding's box refcount dropped — at its
// scope's normal (fall-through) exit; a `return` releases *every* live frame
// before it seals, because it leaves all enclosing scopes at once (a value that
// escapes via the return was retained by the ownership pass, so releasing the
// local reference is safe). break/continue conservatively leak the current
// iteration's bindings (safe — never a double free); refining that is future work.

// pushManagedFrame / popManagedFrame bracket a lexical scope.
func (l *lowerer) pushManagedFrame() {
	l.managedFrames = append(l.managedFrames, nil)
}

func (l *lowerer) popManagedFrame() {
	l.managedFrames = l.managedFrames[:len(l.managedFrames)-1]
}

// addManagedBinding records a managed binding's alloca in the current top frame,
// so its scope exit releases it. slot must be a managed (string or `shared` box)
// alloca; lyraType is what it holds, carried so the release can select the value's
// drop glue.
func (l *lowerer) addManagedBinding(slot value.Value, lyraType types.Type) {
	top := len(l.managedFrames) - 1
	l.managedFrames[top] = append(l.managedFrames[top], managedSlot{slot, lyraType})
}

// retireManagedSlot removes a binding's alloca from whichever frame holds it, so
// no later frame release touches it — the Perceus **transfer fusion** (stage 2):
// a last-use transfer moves the reference to the consumer, so the binding's
// scope-exit release would be a pure no-op, and dropping it from the frame here
// removes it entirely (no sentinel, no no-op release). Safe because a transfer is
// unconditional (the ownership pass only marks a non-branch use) and this runs at
// the use, compile-after any earlier seal — so an earlier return still saw the
// binding in-frame and released it on its own path.
func (l *lowerer) retireManagedSlot(slot value.Value) {
	for i := range l.managedFrames {
		for j, s := range l.managedFrames[i] {
			if s.slot == slot {
				l.managedFrames[i] = append(l.managedFrames[i][:j], l.managedFrames[i][j+1:]...)
				return
			}
		}
	}
}

// topFrameSlot returns the current (innermost) scope's managed entry for slot —
// i.e. the binding was declared in the block being lowered — and whether it's
// there at all.
func (l *lowerer) topFrameSlot(slot value.Value) (managedSlot, bool) {
	top := l.managedFrames[len(l.managedFrames)-1]
	i := slices.IndexFunc(top, func(m managedSlot) bool { return m.slot == slot })
	if i < 0 {
		return managedSlot{}, false
	}
	return top[i], true
}

// dropLastUsesInStmt is the Perceus **drop fusion** (stage 2): after a statement
// is lowered, release-and-retire every current-scope binding whose *last use* is a
// borrow within that statement. It emits into `block` — the statement's end block,
// which post-dominates the statement's internal branches — so the drop runs on
// every path through the statement (a conditional last use is handled correctly),
// and retiring the slot means the scope-exit frame release skips it: no sentinel,
// no residual no-op. Bindings from enclosing scopes are left to their own block
// (the top-frame restriction); a statement that itself sealed (an early return) is
// skipped by the caller, so its bindings are freed by the seal's frame release on
// that path instead.
func (l *lowerer) dropLastUsesInStmt(block *ir.Block, stmt ast.Statement) error {
	if len(l.managedFrames[len(l.managedFrames)-1]) == 0 {
		return nil // no bindings declared in this scope — nothing here can be dropped
	}
	seen := map[value.Value]bool{}
	var walkErr error
	ast.WalkStmt(stmt, nil, func(e ast.Expression) bool {
		id, ok := e.(*ast.IdentifierExpr)
		if !ok {
			return true
		}
		if transfer, isLast := l.res.Ownership.LastUse(e); !isLast || transfer {
			return true // not a last use, or a transfer (retired at the move)
		}
		slot, found := l.locals[id.Name]
		if !found || seen[slot] {
			return true
		}
		m, inTop := l.topFrameSlot(slot)
		if !inTop {
			return true
		}
		seen[slot] = true
		a := slot.(*ir.InstAlloca)
		if err := l.deepRelease(block, block.NewLoad(a.ElemType, slot), m.ty); err != nil {
			walkErr = err
			return false
		}
		l.retireManagedSlot(slot)
		return true
	})
	return walkErr
}

// releaseSlots emits a release for each recorded binding: load the current value
// from its alloca and drop a reference from everything it owns — its own box's
// refcount for a managed value, or each managed field for a stack aggregate (which
// goes through the per-type glue, so this stays straight-line and the frame release
// never has to split the caller's block).
func (l *lowerer) releaseSlots(block *ir.Block, slots []managedSlot) error {
	for _, m := range slots {
		a := m.slot.(*ir.InstAlloca)
		v := block.NewLoad(a.ElemType, m.slot)
		if err := l.deepRelease(block, v, m.ty); err != nil {
			return err
		}
	}
	return nil
}

// releaseTopManagedFrame releases the current scope's managed bindings — a
// block's fall-through exit.
func (l *lowerer) releaseTopManagedFrame(block *ir.Block) error {
	return l.releaseSlots(block, l.managedFrames[len(l.managedFrames)-1])
}

// releaseAllManagedFrames releases every live managed binding, innermost scope
// first — the cleanup a `return` runs before it leaves the function. Frames are
// not popped: the block lowering that owns each frame still pops it (and, being
// sealed by the return, skips re-releasing), so each binding is released exactly
// once per control-flow path.
func (l *lowerer) releaseAllManagedFrames(block *ir.Block) error {
	return l.releaseManagedFramesFrom(block, 0)
}

// releaseManagedFramesFrom releases the managed bindings in every frame from
// index `depth` up (innermost first), without popping — the cleanup a `break`/
// `continue` runs for the loop-body scopes it exits. A slot already handled by a
// last-use drop/transfer was retired from its frame, so it isn't released twice.
func (l *lowerer) releaseManagedFramesFrom(block *ir.Block, depth int) error {
	for i := len(l.managedFrames) - 1; i >= depth; i-- {
		if err := l.releaseSlots(block, l.managedFrames[i]); err != nil {
			return err
		}
	}
	return nil
}
