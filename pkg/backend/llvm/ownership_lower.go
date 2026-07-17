package llvm

import (
	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/value"
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
// so its scope exit releases it. slot must be a string alloca.
func (l *lowerer) addManagedBinding(slot value.Value) {
	top := len(l.managedFrames) - 1
	l.managedFrames[top] = append(l.managedFrames[top], slot)
}

// isManagedSlot reports whether an alloca holds a managed (string) value — the
// signal that its binding participates in refcounting.
func isManagedSlot(slot value.Value) bool {
	a, ok := slot.(*ir.InstAlloca)
	return ok && isStringLLVMType(a.ElemType)
}

// releaseSlots emits a release for each recorded binding: load the current value
// from its alloca and drop its box's refcount.
func (l *lowerer) releaseSlots(block *ir.Block, slots []value.Value) {
	for _, slot := range slots {
		a := slot.(*ir.InstAlloca)
		v := block.NewLoad(a.ElemType, slot)
		l.lowerStringRelease(block, v)
	}
}

// releaseTopManagedFrame releases the current scope's managed bindings — a
// block's fall-through exit.
func (l *lowerer) releaseTopManagedFrame(block *ir.Block) {
	l.releaseSlots(block, l.managedFrames[len(l.managedFrames)-1])
}

// releaseAllManagedFrames releases every live managed binding, innermost scope
// first — the cleanup a `return` runs before it leaves the function. Frames are
// not popped: the block lowering that owns each frame still pops it (and, being
// sealed by the return, skips re-releasing), so each binding is released exactly
// once per control-flow path.
func (l *lowerer) releaseAllManagedFrames(block *ir.Block) {
	for i := len(l.managedFrames) - 1; i >= 0; i-- {
		l.releaseSlots(block, l.managedFrames[i])
	}
}
