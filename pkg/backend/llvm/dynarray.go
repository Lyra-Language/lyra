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

// A dynamic array `[]T` is a heap-boxed, ref-counted value: a `ptr` to
// `{ i64 rc, i64 len, [0 x T] }` — the refcount, the element count, then the
// elements. Modelling it as a single box pointer (rather than a `{ data, len }`
// fat pointer) means it reuses the shared-value managed machinery unchanged: the
// value *is* a pointer, so managedBox / retain / release act on it directly, and
// the ownership pass frees it like any managed value (IsManaged covers `[]T`). The
// `[0 x T]` tail is a flexible array member GEP'd past its declared length; the real
// storage is sized at allocation from the element count. Every `[]T` — even empty —
// is a real box, so retain/release stay uniform (no null special case).
//
// This first slice covers construction from a literal, indexing (bounds-checked
// against the runtime len, negative-from-end), and by-value flow through
// let/params/returns. Deferred, loud errors: a *managed* element type (the box's
// drop glue would have to loop over len to release each element — errored at
// construction), iteration (`for x in xs`), `match` on `[]T`, `.len()`, and growth
// (no grow operation exists in the language yet).

// i32c / i64c are small constant helpers for the many GEP indices below.
func i32c(n int64) *constant.Int { return constant.NewInt(lltypes.I32, n) }
func i64c(n int64) *constant.Int { return constant.NewInt(lltypes.I64, n) }

// lowerDynArrayConstruction builds a `[]T` literal: allocate a ref-counted box
// sized to hold the elements, store the length and each element, and yield the box
// pointer. An empty literal still allocates a (len-0) box, keeping every dynamic
// array a uniform managed box.
func (l *lowerer) lowerDynArrayConstruction(block *ir.Block, e *ast.ArrayLiteralExpr, dynType types.DynamicArrayType) (value.Value, *ir.Block, error) {
	elemLyra := dynType.ElementType
	if elemLyra == nil {
		return nil, nil, fmt.Errorf("llvm: dynamic array literal has no element type")
	}
	if ownership.IsManaged(elemLyra) {
		return nil, nil, fmt.Errorf("llvm: dynamic array of managed element type %s not implemented yet (element drop glue deferred)", elemLyra)
	}
	elemLL, err := l.lowerType(elemLyra)
	if err != nil {
		return nil, nil, err
	}
	elemSize, elemAlign, ok := SizeAndAlign(l.resolveForLayout(elemLyra))
	if !ok {
		return nil, nil, fmt.Errorf("llvm: cannot size dynamic array element type %s", elemLyra)
	}
	stride := alignUp(elemSize, elemAlign)
	n := len(e.Elements)

	l.ensureRCRuntime()
	boxTy := DynArrayBoxType(elemLL)
	boxSize := i64c(int64(dynArrayHeaderSize + n*stride))
	boxI8 := block.NewCall(l.rcAlloc, boxSize) // i8*, rc = 1
	box := block.NewBitCast(boxI8, lltypes.NewPointer(boxTy))

	// Store the length (field 1).
	block.NewStore(i64c(int64(n)), block.NewGetElementPtr(boxTy, box, i32c(0), i32c(1)))

	// Store each element into the flexible tail (field 2, index i).
	for i, elemExpr := range e.Elements {
		var v value.Value
		v, block, err = l.lowerExpr(block, elemExpr)
		if err != nil {
			return nil, nil, err
		}
		v, err = l.coerceAggregateElem(block, v, elemLL, elemExpr)
		if err != nil {
			return nil, nil, err
		}
		elemPtr := block.NewGetElementPtr(boxTy, box, i32c(0), i32c(2), i64c(int64(i)))
		block.NewStore(v, elemPtr)
	}
	return box, block, nil
}

// lowerDynArrayIndex lowers `xs[i]` on a dynamic array: load the runtime length
// from the box, bounds-check the (possibly negative, counting-from-the-end) index
// against it, then GEP+load the element. Unlike a fixed-size array there is no
// compile-time size, so the bounds check is always emitted (the value-range pass
// doesn't track dynamic lengths).
func (l *lowerer) lowerDynArrayIndex(block *ir.Block, e *ast.IndexExpr, dynType types.DynamicArrayType) (value.Value, *ir.Block, error) {
	elemLL, err := l.lowerType(dynType.ElementType)
	if err != nil {
		return nil, nil, err
	}
	boxTy := DynArrayBoxType(elemLL)

	box, block, err := l.lowerExpr(block, e.Object)
	if err != nil {
		return nil, nil, err
	}
	length := block.NewLoad(lltypes.I64, block.NewGetElementPtr(boxTy, box, i32c(0), i32c(1)))

	idx, block, err := l.lowerExpr(block, e.Index)
	if err != nil {
		return nil, nil, err
	}
	// Widen the index to i64 by its own signedness (a negative signed index stays
	// negative through the sign-extend), then apply the same negative-from-end +
	// unsigned-`>=`-bound trap as a fixed-size array, but against the runtime length.
	signed, _ := l.getIntSignedness(e.Index)
	idx64 := coerceIntWidth(block, idx, signed, lltypes.I64)
	neg := block.NewICmp(enum.IPredSLT, idx64, i64c(0))
	adjusted := block.NewSelect(neg, block.NewAdd(idx64, length), idx64)
	oob := block.NewICmp(enum.IPredUGE, adjusted, length)
	block = l.emitTrapIf(block, oob, l.panicIndexOOBFunc())

	elemPtr := block.NewGetElementPtr(boxTy, box, i32c(0), i32c(2), adjusted)
	return block.NewLoad(elemLL, elemPtr), block, nil
}
