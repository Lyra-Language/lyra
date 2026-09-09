package llvm

import (
	"fmt"

	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/constant"
	lltypes "github.com/llir/llvm/ir/types"
	"github.com/llir/llvm/ir/value"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// A C union lowers to memory, not to an SSA aggregate, and that is forced rather than
// chosen: every member sits at offset 0, so reading one member of a value written as
// another is a *reinterpretation* of bytes, and LLVM has no way to say that about an
// SSA value. `alloca`, then bitcast the address to a pointer to the member's own type.
//
// This is the same shape `buildDataValue` already uses for a `data` type's payload blob
// — the difference is only that a `data` value carries a tag saying which arm is live
// and a union does not, which is the whole reason its member read is `unsafe`.
//
// The spills are the ordinary `alloca` in the entry block, so mem2reg promotes them
// wherever the union does not actually escape; where it does — the SDL case, a buffer C
// fills through a pointer — the memory is what was wanted anyway.

// lowerUnionInstanceExpr builds `Ev { kind: 7 }`.
//
// **The whole slot is zeroed first**, which C does not do and Lyra does. A C union's
// unwritten bytes are indeterminate, so a program that reads the wrong member gets
// whatever the stack happened to hold — behaviour that changes with the call that ran
// before it. Zeroing costs one store of a constant and makes a wrong read *wrong the
// same way every time*, which is the difference between a bug that reproduces and one
// that does not.
func (l *lowerer) lowerUnionInstanceExpr(block *ir.Block, e *ast.StructInstanceExpr, ut types.UnionType) (value.Value, *ir.Block, error) {
	unionTy, err := l.lowerType(ut)
	if err != nil {
		return nil, nil, err
	}
	if len(e.Fields) != 1 {
		return nil, nil, fmt.Errorf("llvm: union %q literal has %d members; the typechecker admits exactly one", ut.Name, len(e.Fields))
	}
	field := e.Fields[0]
	member, ok := ut.MemberByName(field.Name)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: union %q has no member %q", ut.Name, field.Name)
	}
	memberTy, err := l.lowerType(l.resolveForLayout(member.Type))
	if err != nil {
		return nil, nil, err
	}

	val, block, err := l.lowerExpr(block, field.Value)
	if err != nil {
		return nil, nil, err
	}

	slot := block.Parent.Blocks[0].NewAlloca(unionTy)
	block.NewStore(constant.NewZeroInitializer(unionTy), slot)
	memberPtr := block.NewBitCast(slot, lltypes.NewPointer(memberTy))
	block.NewStore(val, memberPtr)
	return block.NewLoad(unionTy, slot), block, nil
}

// lowerUnionMemberExpr reads `e.kind`.
//
// It takes the union's **address** where the object is an lvalue path and spills a copy
// where it is not. The distinction is not an optimization: a union reached through a
// pointer C has just written — `SDL_PollEvent(&mut ev)` then `ev.type` — must read the
// storage that was written, and a spilled copy of a stale load would read what was there
// before the call.
func (l *lowerer) lowerUnionMemberExpr(block *ir.Block, e *ast.MemberExpr, ut types.UnionType) (value.Value, *ir.Block, error) {
	member, ok := ut.MemberByName(e.Property.Name)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: union %q has no member %q", ut.Name, e.Property.Name)
	}
	memberTy, err := l.lowerType(l.resolveForLayout(member.Type))
	if err != nil {
		return nil, nil, err
	}
	addr, block, err := l.unionAddress(block, e.Object, ut)
	if err != nil {
		return nil, nil, err
	}
	memberPtr := block.NewBitCast(addr, lltypes.NewPointer(memberTy))
	return block.NewLoad(memberTy, memberPtr), block, nil
}

// unionAddress yields a pointer to obj's union storage — the real storage for an lvalue
// path, and a fresh spill of the value otherwise (a union returned by a call, say, which
// has no home of its own).
func (l *lowerer) unionAddress(block *ir.Block, obj ast.Expression, ut types.UnionType) (value.Value, *ir.Block, error) {
	if isLValuePath(obj) {
		loc, block, err := l.lvalueAddress(block, obj)
		if err != nil {
			return nil, nil, err
		}
		return loc.ptr, block, nil
	}
	unionTy, err := l.lowerType(ut)
	if err != nil {
		return nil, nil, err
	}
	val, block, err := l.lowerExpr(block, obj)
	if err != nil {
		return nil, nil, err
	}
	slot := block.Parent.Blocks[0].NewAlloca(unionTy)
	block.NewStore(val, slot)
	return slot, block, nil
}
