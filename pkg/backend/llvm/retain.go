package llvm

import (
	"fmt"

	"github.com/llir/llvm/ir"
	lltypes "github.com/llir/llvm/ir/types"
	"github.com/llir/llvm/ir/value"

	"github.com/Lyra-Language/lyra/pkg/analyzer/ownership"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// Recursive retain glue — the mirror image of drop.go, and the other half of
// **deep-retain-on-copy**.
//
// A plain `stack` aggregate is a *value*: `let q = p` copies it, and the copy's
// managed fields (a string, a `shared X`, a `[]T`) are the *same* boxes the original
// points at. Until this file existed no reference was added for that copy, so the
// aggregate's fields were owned by nobody in particular — which was not merely a
// leak. Whoever *did* own them (a `shared` box's drop glue, an interior assignment)
// would free them while the copy was still live: an ASan-confirmed use-after-free
// (`let q = ps[0]` on a `[]Person`, then let the array die).
//
// The fix is symmetry with drop.go. For a payload type T this generates (once,
// cached)
//
//	void @lyra_retain_T(i8* payload)
//
// which retains every managed reference reachable *by value* from T — the exact set
// @lyra_drop_T releases. So a copy of T is a genuine +1 on each of T's managed
// fields, and T's death (a scope exit, a frame release, a box's drop_fn) balances it.
//
// "By value" is the same stopping rule and the same termination argument as the drop
// side: a managed field is retained with a single lyra_rc_retain and never walked
// into (its own box owns whatever *it* holds), so the walk only descends through
// inline stack aggregates, and a recursive type's cycle must pass through a `shared`
// field (lyra-E014), which is exactly where it stops. The function is registered in
// the cache *before* its body is built, so a self-referential type emits one function
// rather than recursing forever during generation.
//
// Why a glue *function* rather than inlining the retains at each copy site: a `data`
// value's retain has to switch on the tag, which would split the caller's block. Copy
// sites are everywhere (bindings, arguments, returns, field initializers, array
// elements), and the ownership hook in lowerExpr has no way to hand a new block back
// to every one of them. Routing through a call keeps every site straight-line, and
// the branching lives inside the glue where it costs nothing structurally.

// deepRetain adds one reference to every managed value reachable by value from v (of
// Lyra type t) — what a *copy* of v owes. Emits nothing when t owns nothing managed.
//
// A directly-managed value is retained inline (one call, no glue — this is the
// overwhelmingly common case and keeps the IR of string-only programs unchanged); an
// aggregate goes through its per-type glue, so this never splits the caller's block.
func (l *lowerer) deepRetain(block *ir.Block, v value.Value, t types.Type) error {
	if !l.needsDrop(t) {
		return nil
	}
	t = l.stripNewtype(t) // a newtype is its base at run time
	if ownership.IsManaged(t) {
		l.lowerManagedRetain(block, v, t)
		return nil
	}
	fn, err := l.retainFuncFor(t)
	if err != nil || fn == nil {
		return err
	}
	return l.callGlueOnValue(block, fn, v, t)
}

// deepRelease is deepRetain's inverse: it drops one reference from every managed
// value reachable by value from v — what the *death* of v owes. Emits nothing when t
// owns nothing managed.
//
// Like deepRetain it never splits the caller's block, which is what lets the frame
// releases, last-use drops, and temporary flushes stay straight-line even for a
// `data` value whose drop has to switch on a tag.
func (l *lowerer) deepRelease(block *ir.Block, v value.Value, t types.Type) error {
	if !l.needsDrop(t) {
		return nil
	}
	t = l.stripNewtype(t) // mirrors deepRetain
	if ownership.IsManaged(t) {
		return l.lowerManagedRelease(block, v, t)
	}
	fn, err := l.dropFuncFor(t)
	if err != nil || fn == nil {
		return err
	}
	return l.callGlueOnValue(block, fn, v, t)
}

// callGlueOnValue calls a per-type glue function (retain or drop) on the first-class
// value v. The glue takes an `i8*` to the payload — the same calling convention
// lyra_rc_release uses for a box's drop_fn — so v is spilled to an entry-block alloca
// and its address passed. mem2reg cleans the spill up when it can.
func (l *lowerer) callGlueOnValue(block *ir.Block, fn *ir.Func, v value.Value, t types.Type) error {
	payloadTy, err := l.lowerType(t)
	if err != nil {
		return fmt.Errorf("llvm: cannot pass %s to its glue: %w", t, err)
	}
	slot := block.Parent.Blocks[0].NewAlloca(payloadTy)
	block.NewStore(v, slot)
	block.NewCall(fn, block.NewBitCast(slot, lltypes.NewPointer(lltypes.I8)))
	return nil
}

// retainFuncFor returns the retain glue for a payload of Lyra type t, or nil when t
// owns nothing managed. Generated once per type and cached, exactly like dropFnFor.
func (l *lowerer) retainFuncFor(t types.Type) (*ir.Func, error) {
	if !l.needsDrop(t) {
		return nil, nil
	}
	t = l.stripNewtype(t) // one glue per base, as in dropFuncFor
	key := t.String()
	if fn, ok := l.retainFns[key]; ok {
		return fn, nil
	}

	payloadTy, err := l.lowerType(t)
	if err != nil {
		return nil, fmt.Errorf("llvm: cannot generate retain glue for %s: %w", t, err)
	}
	l.retainFnCount++
	fn := l.module.NewFunc(fmt.Sprintf("lyra_retain_%d_%s", l.retainFnCount, mangleTypeKey(key)),
		lltypes.Void, ir.NewParam("payload", lltypes.NewPointer(lltypes.I8)))
	// Cache before building the body: a recursive type's glue reaches itself (via a
	// `shared` field's retain), and it must find this entry rather than regenerate.
	l.retainFns[key] = fn

	entry := fn.NewBlock("entry")
	typed := entry.NewBitCast(fn.Params[0], lltypes.NewPointer(payloadTy))
	end, err := l.emitRetainValue(entry, entry.NewLoad(payloadTy, typed), t)
	if err != nil {
		return nil, err
	}
	end.NewRet(nil)
	return fn, nil
}

// emitRetainValue retains everything the first-class value v (of Lyra type t) owns,
// returning the block control ends in (a `data` value switches on its tag).
//
// The walk is owned_walk.go's — shared with emitDropValue, so a copy and its death cover
// the same fields by construction rather than by two switches agreeing. See that file for
// why having two of it was the bug rather than the style.
func (l *lowerer) emitRetainValue(block *ir.Block, v value.Value, t types.Type) (*ir.Block, error) {
	return l.emitOwnedValue(block, v, t, retainWalk)
}
