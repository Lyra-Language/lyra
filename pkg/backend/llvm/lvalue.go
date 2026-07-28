package llvm

import (
	"fmt"

	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/constant"
	"github.com/llir/llvm/ir/enum"
	lltypes "github.com/llir/llvm/ir/types"
	"github.com/llir/llvm/ir/value"

	"github.com/Lyra-Language/lyra/pkg/analyzer/ownership"
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// lowerLValueAssignment lowers an interior-mutation statement — `xs[i] = v`,
// `p.x = v`, or any path built from those (`grid[i].y = v`, `p.arr[i] = v`,
// `line.start.x = v`, `m[i][j] = v`). It computes the target location's address via
// lvalueAddress (which walks the path) and stores the value there, mutating in
// place. The typechecker (checkLValueAssignment) has already verified the root
// binding is mutable (a `var`, a `let mut`, or a `mut`/`own` parameter), that the
// value's type matches the target, and that no `readonly` field is written.
//
// Deferred, loud errors: a `shared` struct/array in the path, and a *managed* target
// type (`[]string`, a `string` field) — the latter needs to release the overwritten
// value and take ownership of the new one.
func (l *lowerer) lowerLValueAssignment(block *ir.Block, stmt *ast.LValueAssignmentStmt) (*ir.Block, error) {
	ptr, targetType, block, err := l.lvalueAddress(block, stmt.Target)
	if err != nil {
		return nil, err
	}
	if targetType == nil {
		return nil, fmt.Errorf("llvm: no type recorded for assignment target")
	}
	targetLL, err := l.lowerType(targetType)
	if err != nil {
		return nil, err
	}
	v, block, err := l.lowerExpr(block, stmt.Value)
	if err != nil {
		return nil, err
	}
	// The typechecker already narrowed the value to the target type; coerce
	// defensively so a residual int-width mismatch fixes rather than emitting bad IR.
	v, err = l.coerceAggregateElem(block, v, targetLL, stmt.Value)
	if err != nil {
		return nil, err
	}
	// A managed target owns whatever it currently holds; release that reference
	// before the new (+1) value overwrites it, so the slot's refcount stays balanced
	// (mirrors managed `var` reassignment). The new value is computed *before* this
	// release, so `xs[i] = xs[i] ++ y` — which reads the old element — is safe. The
	// ownership pass gave the RHS its +1 (its LValueAssignmentStmt case).
	if ownership.IsManaged(targetType) {
		old := block.NewLoad(targetLL, ptr)
		if err := l.lowerManagedRelease(block, old, targetType); err != nil {
			return nil, err
		}
	}
	block.NewStore(v, ptr)
	return block, nil
}

// lvalueAddress returns the address (and Lyra type) of an assignable location,
// recursing over the path: an **identifier** root resolves to its alloca; a
// **`.field`** hop geps into the object's stack-struct storage; an **`[i]`** hop
// geps to the array element (bounds-checked, negative-from-end). Because it recurses
// on the object, arbitrary mixes nest — `grid[i].y`, `p.arr[i]`, `m[i][j]`,
// `line.start.x`. A fixed-size array is addressed through its storage; a `shared` or
// dynamic array — and a `shared` struct — through its box (loaded from the object's
// location).
func (l *lowerer) lvalueAddress(block *ir.Block, e ast.Expression) (value.Value, types.Type, *ir.Block, error) {
	switch t := e.(type) {
	case *ast.IdentifierExpr:
		slot, ok := l.locals[t.Name]
		if !ok {
			return nil, nil, nil, fmt.Errorf("llvm: assignment to unbound identifier %q", t.Name)
		}
		if _, ok := slot.(*ir.InstAlloca); !ok {
			return nil, nil, nil, fmt.Errorf("llvm: identifier %q is not addressable", t.Name)
		}
		lyraType, _ := l.res.TypeTable.Get(t)
		return slot, lyraType, block, nil

	case *ast.MemberExpr:
		return l.memberFieldAddress(block, t)

	case *ast.IndexExpr:
		return l.indexElemAddress(block, t)
	}
	return nil, nil, nil, fmt.Errorf("llvm: unsupported assignment target path element %T", e)
}

// memberFieldAddress computes the address of `obj.field` by taking the object's
// address and gep-ing to the named field of its (stack) struct type.
func (l *lowerer) memberFieldAddress(block *ir.Block, e *ast.MemberExpr) (value.Value, types.Type, *ir.Block, error) {
	if e.Optional {
		return nil, nil, nil, fmt.Errorf("llvm: optional member assignment (?.) not implemented")
	}
	objType, ok := l.res.TypeTable.Get(e.Object)
	if !ok {
		return nil, nil, nil, fmt.Errorf("llvm: no type recorded for member-assignment object")
	}
	fields, ok := l.namedStructFields(objType)
	if !ok {
		return nil, nil, nil, fmt.Errorf("llvm: field assignment on non-struct type %s is not implemented", objType)
	}
	idx := -1
	var fieldType types.Type
	for i, f := range fields {
		if f.Name == e.Property.Name {
			idx, fieldType = i, f.Type
			break
		}
	}
	if idx < 0 {
		return nil, nil, nil, fmt.Errorf("llvm: struct has no field %q", e.Property.Name)
	}

	// A `shared` struct is a pointer to its box `{ i64 rc, payload }`, so the field
	// is addressed through the box (loaded from the object's slot): box → payload
	// (field 1) → field idx — the write counterpart to lowerMemberExpr's read.
	if types.AllocationOf(objType) == types.Shared {
		box, block, err := l.lvalueBoxPtr(block, e.Object, objType)
		if err != nil {
			return nil, nil, nil, err
		}
		payloadTy, err := l.lowerType(types.WithAllocation(objType, types.Stack))
		if err != nil {
			return nil, nil, nil, err
		}
		fieldPtr := block.NewGetElementPtr(SharedBoxType(payloadTy), box,
			constant.NewInt(lltypes.I32, 0), constant.NewInt(lltypes.I32, 1), constant.NewInt(lltypes.I32, int64(idx)))
		return fieldPtr, fieldType, block, nil
	}

	// A stack struct: the object's address *is* the struct storage; gep into it.
	objPtr, _, block, err := l.lvalueAddress(block, e.Object)
	if err != nil {
		return nil, nil, nil, err
	}
	structTy, err := l.lowerType(objType)
	if err != nil {
		return nil, nil, nil, err
	}
	fieldPtr := block.NewGetElementPtr(structTy, objPtr,
		constant.NewInt(lltypes.I32, 0), constant.NewInt(lltypes.I32, int64(idx)))
	return fieldPtr, fieldType, block, nil
}

// indexElemAddress computes the address of `obj[i]`. A fixed-size array is addressed
// through the object's own storage (its alloca / field slot); a `shared` or dynamic
// array through its box, loaded from that storage. The index is bounds-checked
// against the compile-time size or the runtime length.
func (l *lowerer) indexElemAddress(block *ir.Block, e *ast.IndexExpr) (value.Value, types.Type, *ir.Block, error) {
	objType, ok := l.res.TypeTable.Get(e.Object)
	if !ok {
		return nil, nil, nil, fmt.Errorf("llvm: no type recorded for index-assignment object")
	}
	switch at := objType.(type) {
	case types.StaticArrayType:
		elemLL, err := l.lowerType(at.ElementType)
		if err != nil {
			return nil, nil, nil, err
		}
		arrayTy := lltypes.NewArray(uint64(at.Size), elemLL)
		size := constant.NewInt(lltypes.I64, int64(at.Size))
		if types.AllocationOf(objType) == types.Shared {
			box, block, err := l.lvalueBoxPtr(block, e.Object, objType)
			if err != nil {
				return nil, nil, nil, err
			}
			adjusted, block, err := l.boundsCheckedIndex(block, e, size)
			if err != nil {
				return nil, nil, nil, err
			}
			// box → SharedBox { i64 rc, [N x T] } field 1 → element.
			elemPtr := block.NewGetElementPtr(SharedBoxType(arrayTy), box,
				constant.NewInt(lltypes.I32, 0), constant.NewInt(lltypes.I32, 1), adjusted)
			return elemPtr, at.ElementType, block, nil
		}
		objPtr, _, block, err := l.lvalueAddress(block, e.Object) // pointer to [N x T]
		if err != nil {
			return nil, nil, nil, err
		}
		adjusted, block, err := l.boundsCheckedIndex(block, e, size)
		if err != nil {
			return nil, nil, nil, err
		}
		elemPtr := block.NewGetElementPtr(arrayTy, objPtr, constant.NewInt(lltypes.I64, 0), adjusted)
		return elemPtr, at.ElementType, block, nil

	case types.DynamicArrayType:
		elemLL, err := l.lowerType(at.ElementType)
		if err != nil {
			return nil, nil, nil, err
		}
		boxTy := DynArrayBoxType(elemLL)
		box, block, err := l.lvalueBoxPtr(block, e.Object, objType)
		if err != nil {
			return nil, nil, nil, err
		}
		length := block.NewLoad(lltypes.I64, block.NewGetElementPtr(boxTy, box, i32c(0), i32c(1)))
		adjusted, block, err := l.boundsCheckedIndex(block, e, length)
		if err != nil {
			return nil, nil, nil, err
		}
		elemPtr := block.NewGetElementPtr(boxTy, box, i32c(0), i32c(2), adjusted)
		return elemPtr, at.ElementType, block, nil
	}
	return nil, nil, nil, fmt.Errorf("llvm: index assignment into %s is not implemented", objType)
}

// lvalueBoxPtr loads the box pointer a `shared`/dynamic-array location holds: the
// object's *address* (from lvalueAddress) points at the box-pointer slot, so this
// loads it. Reading the box pointer this way (rather than via lowerExpr) keeps the
// object out of the ownership retain/release hooks — the assignment only mutates the
// box in place, it doesn't take a reference.
func (l *lowerer) lvalueBoxPtr(block *ir.Block, objExpr ast.Expression, objType types.Type) (value.Value, *ir.Block, error) {
	addr, _, block, err := l.lvalueAddress(block, objExpr)
	if err != nil {
		return nil, nil, err
	}
	boxPtrTy, err := l.lowerType(objType) // ptr to the box
	if err != nil {
		return nil, nil, err
	}
	return block.NewLoad(boxPtrTy, addr), block, nil
}

// boundsCheckedIndex lowers the index of `e` and returns the from-the-end-adjusted,
// bounds-trapped i64 offset (a negative index counts from the end; out-of-range
// traps via lyra_panic_index_out_of_bounds). Unlike the read path it does not consult
// the value-range analysis's IndexInBounds — an assignment target isn't marked — so
// the check is always emitted.
func (l *lowerer) boundsCheckedIndex(block *ir.Block, e *ast.IndexExpr, size value.Value) (value.Value, *ir.Block, error) {
	idx, block, err := l.lowerExpr(block, e.Index)
	if err != nil {
		return nil, nil, err
	}
	signed, _ := l.getIntSignedness(e.Index)
	idx64 := coerceIntWidth(block, idx, signed, lltypes.I64)
	neg := block.NewICmp(enum.IPredSLT, idx64, constant.NewInt(lltypes.I64, 0))
	adjusted := block.NewSelect(neg, block.NewAdd(idx64, size), idx64)
	oob := block.NewICmp(enum.IPredUGE, adjusted, size)
	block = l.emitTrapIf(block, oob, l.panicIndexOOBFunc())
	return adjusted, block, nil
}
