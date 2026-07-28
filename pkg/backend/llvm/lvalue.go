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

// lowerLValueAssignment lowers an interior-mutation statement whose target is an
// array index — `xs[i] = v`. It computes the element's address (bounds-checked,
// with a negative index counting from the end, exactly like a read `xs[i]`) and
// stores the value into it, mutating the array in place. The typechecker
// (checkLValueAssignment) has already verified the root binding is mutable (a `var`,
// a `let mut`, or a `mut`/`own` parameter) and that the value's type matches the
// element type.
//
// Deferred, loud errors: a member target (`p.x = v` — struct-field assignment), a
// nested path (`grid[i].y = v`), and a *managed* element type (`[]string`) — the
// last needs to release the overwritten value and take ownership of the new one.
func (l *lowerer) lowerLValueAssignment(block *ir.Block, stmt *ast.LValueAssignmentStmt) (*ir.Block, error) {
	idxExpr, ok := stmt.Target.(*ast.IndexExpr)
	if !ok {
		return nil, fmt.Errorf("llvm: interior assignment to a %T target is not implemented yet (only array index `xs[i] = v`)", stmt.Target)
	}
	objType, ok := l.res.TypeTable.Get(idxExpr.Object)
	if !ok {
		return nil, fmt.Errorf("llvm: no type recorded for index-assignment object")
	}
	elemLyra := arrayElementLyraType(objType)
	if elemLyra == nil {
		return nil, fmt.Errorf("llvm: index assignment into %s is not implemented", objType)
	}
	if ownership.IsManaged(elemLyra) {
		return nil, fmt.Errorf("llvm: assigning to a managed array element (%s) is not implemented yet (needs release-old + retain-new)", elemLyra)
	}

	elemPtr, elemLL, block, err := l.arrayElemLValue(block, idxExpr, objType)
	if err != nil {
		return nil, err
	}
	v, block, err := l.lowerExpr(block, stmt.Value)
	if err != nil {
		return nil, err
	}
	// The typechecker already narrowed the value to the element type; coerce
	// defensively so a residual int-width mismatch fixes rather than emitting bad IR.
	v, err = l.coerceAggregateElem(block, v, elemLL, stmt.Value)
	if err != nil {
		return nil, err
	}
	block.NewStore(v, elemPtr)
	return block, nil
}

// arrayElementLyraType returns the element type of an array (fixed-size or dynamic),
// or nil for a non-array type.
func arrayElementLyraType(t types.Type) types.Type {
	switch a := t.(type) {
	case types.StaticArrayType:
		return a.ElementType
	case types.DynamicArrayType:
		return a.ElementType
	}
	return nil
}

// arrayElemLValue returns an addressable pointer to the element `obj[i]` for a
// write, plus the element's LLVM type. It handles a fixed-size array (`[N]T`, stack
// through its alloca or `shared` through its box payload) and a dynamic array
// (`[]T`, through the box payload), bounds-checking the index against the size /
// runtime length. This is the write-side counterpart to lowerIndexExpr's read.
func (l *lowerer) arrayElemLValue(block *ir.Block, e *ast.IndexExpr, objType types.Type) (value.Value, lltypes.Type, *ir.Block, error) {
	switch it := objType.(type) {
	case types.StaticArrayType:
		var arrPtr value.Value
		var arrayTy *lltypes.ArrayType
		var err error
		if types.AllocationOf(objType) == types.Shared {
			arrPtr, arrayTy, block, err = l.sharedArrayPayloadPtr(block, e.Object)
		} else {
			arrPtr, arrayTy, block, err = l.arrayLValue(block, e.Object)
		}
		if err != nil {
			return nil, nil, nil, err
		}
		size := constant.NewInt(lltypes.I64, int64(it.Size))
		adjusted, block, err := l.boundsCheckedIndex(block, e, size)
		if err != nil {
			return nil, nil, nil, err
		}
		elemPtr := block.NewGetElementPtr(arrayTy, arrPtr, constant.NewInt(lltypes.I64, 0), adjusted)
		return elemPtr, arrayTy.ElemType, block, nil
	case types.DynamicArrayType:
		elem, err := l.lowerType(it.ElementType)
		if err != nil {
			return nil, nil, nil, err
		}
		boxTy := DynArrayBoxType(elem)
		box, block, err := l.lowerExpr(block, e.Object)
		if err != nil {
			return nil, nil, nil, err
		}
		length := block.NewLoad(lltypes.I64, block.NewGetElementPtr(boxTy, box, i32c(0), i32c(1)))
		adjusted, block, err := l.boundsCheckedIndex(block, e, length)
		if err != nil {
			return nil, nil, nil, err
		}
		elemPtr := block.NewGetElementPtr(boxTy, box, i32c(0), i32c(2), adjusted)
		return elemPtr, elem, block, nil
	}
	return nil, nil, nil, fmt.Errorf("llvm: index assignment into %s is not implemented", objType)
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
