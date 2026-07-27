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

// lowerArrayLiteralExpr lowers a fixed-size array literal (`[1, 2, 3]`) to an
// LLVM aggregate value `[N x T]`, the same insertvalue-over-undef shape as a
// tuple: a first-class value that round-trips through a `let` binding's
// alloca/store/load. Each element is coerced to the array's element type before
// the insert (normally the identity — the typechecker narrowed literal element
// widths against the annotation — but a residual int-width mismatch is fixed
// rather than panicking llir).
//
// Only a StaticArrayType lowers here; a dynamic array `[]T` (no size) needs a
// heap-backed representation and is deferred with a loud error.
func (l *lowerer) lowerArrayLiteralExpr(block *ir.Block, e *ast.ArrayLiteralExpr) (value.Value, *ir.Block, error) {
	recorded, ok := l.res.TypeTable.Get(e)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: no type recorded for array literal")
	}
	arrType, ok := recorded.(types.StaticArrayType)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: array literal lowering not implemented for %s (only fixed-size arrays)", recorded)
	}
	llType, err := l.lowerType(arrType)
	if err != nil {
		return nil, nil, err
	}
	arrayTy, ok := llType.(*lltypes.ArrayType)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: array type %s did not lower to an LLVM array", arrType)
	}

	var agg value.Value = constant.NewUndef(arrayTy)
	for i, elemExpr := range e.Elements {
		var elemVal value.Value
		elemVal, block, err = l.lowerExpr(block, elemExpr)
		if err != nil {
			return nil, nil, err
		}
		elemVal, err = l.coerceAggregateElem(block, elemVal, arrayTy.ElemType, elemExpr)
		if err != nil {
			return nil, nil, err
		}
		agg = block.NewInsertValue(agg, elemVal, uint64(i))
	}
	return agg, block, nil
}

// lowerIndexExpr lowers array indexing (`xs[i]`). A compile-time-constant index
// reads the element with `extractvalue` on the (first-class) array value — the
// typechecker already range-checked a constant index, so no runtime guard is
// needed. A runtime index goes through memory: the array is made addressable (a
// local/param reuses its own alloca; any other array value is materialized into a
// temp), the index is bounds-checked (Pit-of-Success: an out-of-range index traps
// rather than reading past the array), and the element is `getelementptr`+`load`ed.
//
// String indexing (`s[i]` → rune) and dynamic arrays are deferred with a loud
// error.
func (l *lowerer) lowerIndexExpr(block *ir.Block, e *ast.IndexExpr) (value.Value, *ir.Block, error) {
	if e.Optional {
		return nil, nil, fmt.Errorf("llvm: optional index (?[]) not implemented yet")
	}
	objType, ok := l.res.TypeTable.Get(e.Object)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: no type recorded for index object")
	}
	arrType, ok := objType.(types.StaticArrayType)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: indexing into %s not implemented yet (only fixed-size arrays)", objType)
	}

	// A constant literal index: the typechecker rejected an out-of-range one, so
	// read it directly off the array value with extractvalue (no bounds check, no
	// memory). A large-unsigned literal used as an index is nonsensical — fall to
	// the runtime path, which bounds-checks it.
	if lit, ok := e.Index.(*ast.IntegerLiteralExpr); ok && !lit.Unsigned {
		arr, block, err := l.lowerExpr(block, e.Object)
		if err != nil {
			return nil, nil, err
		}
		return block.NewExtractValue(arr, uint64(lit.Value)), block, nil
	}

	arrPtr, arrayTy, block, err := l.arrayLValue(block, e.Object)
	if err != nil {
		return nil, nil, err
	}
	idx, block, err := l.lowerExpr(block, e.Index)
	if err != nil {
		return nil, nil, err
	}
	// Widen the index to i64 by its own signedness — a signed index sign-extends so
	// a negative value stays negative; an unsigned index can't be negative, so the
	// wrap below is a no-op for it.
	signed, _ := l.getIntSignedness(e.Index)
	idx64 := coerceIntWidth(block, idx, signed, lltypes.I64)
	size := constant.NewInt(lltypes.I64, int64(arrType.Size))

	// The value-range analysis may have proved 0 <= i < size (res.RangeSafety): then
	// the index is non-negative *and* in range, so both the from-the-end adjustment
	// and the bounds trap are dead — emit the bare gep+load. Otherwise:
	// a negative index counts from the end (Python-style): `i < 0` becomes
	// `i + size`, so -1 is the last element and -size is the first. After this
	// adjustment a valid index is in [0, size); an out-of-range one (i < -size, or
	// i >= size) leaves `adjusted` negative or >= size, and the single *unsigned*
	// `>= size` compare catches both (a negative sign-extends to a large unsigned).
	var adjusted value.Value = idx64
	if !l.res.RangeSafety.IndexInBounds(e) {
		neg := block.NewICmp(enum.IPredSLT, idx64, constant.NewInt(lltypes.I64, 0))
		adjusted = block.NewSelect(neg, block.NewAdd(idx64, size), idx64)
		oob := block.NewICmp(enum.IPredUGE, adjusted, size)
		block = l.emitTrapIf(block, oob, l.panicIndexOOBFunc())
	}

	// getelementptr [N x T], [N x T]* arrPtr, i64 0, i64 adjusted  →  T*
	elemPtr := block.NewGetElementPtr(arrayTy, arrPtr, constant.NewInt(lltypes.I64, 0), adjusted)
	return block.NewLoad(arrayTy.ElemType, elemPtr), block, nil
}

// arrayLValue returns an addressable `[N x T]*` pointer for an array-typed object
// expression, so a runtime index can `getelementptr` into it. A local or
// parameter is already backed by an alloca of the array type, so it's used
// directly (no copy). Any other array value (a literal, a call result) is
// materialized into a fresh entry-block alloca and stored, since a first-class
// aggregate value can't be indexed by a runtime value.
func (l *lowerer) arrayLValue(block *ir.Block, obj ast.Expression) (value.Value, *lltypes.ArrayType, *ir.Block, error) {
	if id, ok := obj.(*ast.IdentifierExpr); ok {
		if slot, found := l.locals[id.Name]; found {
			if alloca, ok := slot.(*ir.InstAlloca); ok {
				if at, ok := alloca.ElemType.(*lltypes.ArrayType); ok {
					return alloca, at, block, nil
				}
			}
		}
	}
	arr, block, err := l.lowerExpr(block, obj)
	if err != nil {
		return nil, nil, nil, err
	}
	at, ok := arr.Type().(*lltypes.ArrayType)
	if !ok {
		return nil, nil, nil, fmt.Errorf("llvm: indexing a non-array value of type %s", arr.Type())
	}
	entry := block.Parent.Blocks[0]
	slot := entry.NewAlloca(at)
	block.NewStore(arr, slot)
	return slot, at, block, nil
}
