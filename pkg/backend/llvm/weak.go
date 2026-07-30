package llvm

import (
	"fmt"

	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/constant"
	"github.com/llir/llvm/ir/enum"
	lltypes "github.com/llir/llvm/ir/types"
	"github.com/llir/llvm/ir/value"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// `weak T` — a non-owning reference to a `shared T`, and the answer to refcount
// cycle leaks (ALLOCATION.md: refcounting leaks cycles, and the intended fix is
// `shared`-only cycles plus `weak`, not a tracing collector).
//
// A weak reference is the box pointer, represented as an opaque `i8*`: it keeps
// the box's **memory** alive without keeping its **value** alive. That split is
// what the two-count header exists for — the payload's drop glue runs when the
// strong count hits 0, and the memory is freed when the weak count also hits 0
// (runtime.go). So a weak reference is never dangling: it always has a live
// header to read a strong count out of.
//
// It is created by `x.weak()` on a `shared` value and read *only* through
// `if let strong = w { … }`, which upgrades it. There is deliberately no direct
// dereference: the referent may be gone, and the only sound read is one that
// admits that. The upgrade takes a real strong reference, so the value cannot die
// while the then-branch holds it.

// lowerWeakDowngrade lowers `x.weak()`: take a weak reference to a `shared`
// value's box. The box pointer itself is the representation, so the only work is
// the count — the weak reference now keeps the memory alive.
func (l *lowerer) lowerWeakDowngrade(block *ir.Block, call *ast.FunctionCallExpr, member *ast.MemberExpr) (value.Value, *ir.Block, error) {
	if len(call.Arguments) != 0 {
		return nil, nil, fmt.Errorf("llvm: weak() expects 0 arguments, got %d", len(call.Arguments))
	}
	recvT, ok := l.recordedType(member.Object)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: no type recorded for weak() receiver")
	}
	if types.AllocationOf(recvT) != types.Shared {
		return nil, nil, fmt.Errorf("llvm: weak() needs a `shared` receiver, got %s", recvT)
	}
	recv, block, err := l.lowerExpr(block, member.Object)
	if err != nil {
		return nil, nil, err
	}
	if _, isPtr := recv.Type().(*lltypes.PointerType); !isPtr {
		return nil, nil, fmt.Errorf("llvm: weak() receiver did not lower to a box pointer (%s)", recv.Type())
	}
	l.ensureRCRuntime()
	box := block.NewBitCast(recv, lltypes.NewPointer(lltypes.I8))
	block.NewCall(l.rcWeakRetain, box)
	return box, block, nil
}

// lowerWeakUpgrade lowers `if let strong = w { … } else { … }` where `w` is a
// `weak T`: ask the runtime whether the referent is still alive and, if so, bind a
// **new owning reference** to it in the then-branch.
//
// The upgrade is the only way to read through a weak reference, which is what
// makes a dangling read unexpressible rather than merely discouraged. The binding
// is a genuine `shared T` — lyra_rc_upgrade incremented the strong count — so the
// value cannot die while the branch holds it, and the branch owns that reference:
// it is framed like any other managed binding and released at the branch's exit.
//
// The pattern must be a plain identifier. A destructuring pattern would have to
// upgrade *and* match, and the failure paths would be indistinguishable to a
// reader ("gone" vs "didn't match"); `if let s = w { if let Some(v) = s { … } }`
// says which is which.
func (l *lowerer) lowerWeakUpgrade(block *ir.Block, s *ast.IfDestructuringStmt, weakType types.WeakType) (*ir.Block, error) {
	d := &s.DestructuringStatement
	ident, ok := d.Pattern.(*ast.IdentifierPattern)
	if !ok {
		return nil, fmt.Errorf("llvm: upgrading a `weak` reference binds a plain name (`if let s = w`), not a %T pattern", d.Pattern)
	}
	if d.Value == nil {
		return nil, fmt.Errorf("llvm: `if let` on a weak reference has no value")
	}
	w, block, err := l.lowerExpr(block, d.Value)
	if err != nil {
		return nil, err
	}
	l.ensureRCRuntime()
	i8ptr := lltypes.NewPointer(lltypes.I8)
	upgraded := block.NewCall(l.rcUpgrade, block.NewBitCast(w, i8ptr))
	alive := block.NewICmp(enum.IPredNE, upgraded, constant.NewNull(i8ptr))

	fn := block.Parent
	thenBlock := fn.NewBlock("")
	merge := fn.NewBlock("")
	elseBlock := merge
	if s.Else != nil {
		elseBlock = fn.NewBlock("")
	}
	block.NewCondBr(alive, thenBlock, elseBlock)

	// The strong reference lives only in the then-branch, so it is bound *and*
	// framed there: the branch owns the +1 the upgrade minted and releases it on the
	// way out, exactly as a `let` of a `shared` value would.
	sharedTy := types.WithAllocation(weakType.Inner, types.Shared)
	if ident.Name != "_" {
		boxTy, err := l.lowerType(sharedTy)
		if err != nil {
			return nil, err
		}
		slot := fn.Blocks[0].NewAlloca(boxTy)
		thenBlock.NewStore(thenBlock.NewBitCast(upgraded, boxTy), slot)
		l.locals[ident.Name] = slot
		l.addManagedBinding(slot, sharedTy)
	}
	thenEnd, err := l.lowerForEffect(thenBlock, s.Then)
	if err != nil {
		return nil, err
	}
	if thenEnd.Term == nil {
		thenEnd.NewBr(merge)
	}

	if s.Else != nil {
		elseEnd, err := l.lowerForEffect(elseBlock, s.Else)
		if err != nil {
			return nil, err
		}
		if elseEnd.Term == nil {
			elseEnd.NewBr(merge)
		}
	}
	return merge, nil
}
