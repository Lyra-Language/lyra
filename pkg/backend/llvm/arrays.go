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
// A `shared`-flavored array literal (`let xs: shared [3]T = [...]`) is heap-boxed
// like any other `shared` aggregate: the inline `[N x T]` is built first, then
// wrapped in a ref-counted box (the value becomes the box pointer). Only a
// StaticArrayType lowers here; a dynamic array `[]T` (no size) needs a heap-backed
// representation and is deferred with a loud error.
func (l *lowerer) lowerArrayLiteralExpr(block *ir.Block, e *ast.ArrayLiteralExpr) (value.Value, *ir.Block, error) {
	recorded, ok := l.recordedType(e)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: no type recorded for array literal")
	}
	if dynType, ok := recorded.(types.DynamicArrayType); ok {
		return l.lowerDynArrayConstruction(block, e, dynType)
	}
	arrType, ok := recorded.(types.StaticArrayType)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: array literal lowering not implemented for %s (only fixed-size arrays)", recorded)
	}
	// Lower the element aggregate against the by-value array type (strip any `shared`
	// flavor, or lowerType would hand back the box pointer type instead of `[N x T]`).
	stackArrType := types.WithAllocation(arrType, types.Stack).(types.StaticArrayType)
	llType, err := l.lowerType(stackArrType)
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
		// `[1, panic("…")]` — an element diverged, so the array is never built and any
		// element after it is unreachable. See diverged (trap.go).
		if diverged(elemVal, block) {
			return nil, block, nil
		}
		elemVal, err = l.coerceAggregateElem(block, elemVal, arrayTy.ElemType, elemExpr)
		if err != nil {
			return nil, nil, err
		}
		agg = block.NewInsertValue(agg, elemVal, uint64(i))
	}
	// A `shared` array is heap-allocated in a ref-counted box; the value is the box
	// pointer. Mirrors the struct/data construction path (lowerStructInstanceExpr).
	if types.AllocationOf(recorded) == types.Shared {
		boxed, err := l.lowerBoxShared(block, agg, stackArrType)
		return boxed, block, err
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
	objType, ok := l.recordedType(e.Object)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: no type recorded for index object")
	}
	if dynType, ok := objType.(types.DynamicArrayType); ok {
		return l.lowerDynArrayIndex(block, e, dynType)
	}
	if types.IsString(objType) {
		return l.lowerStringIndex(block, e)
	}
	arrType, ok := objType.(types.StaticArrayType)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: indexing into %s not implemented yet (only fixed-size arrays and strings)", objType)
	}

	isShared := types.AllocationOf(objType) == types.Shared

	// A constant literal index: the typechecker rejected an out-of-range one, so
	// read it directly with no bounds check. An inline array uses extractvalue (no
	// memory); a `shared` array is a box pointer, so gep+load through the payload. A
	// large-unsigned literal used as an index is nonsensical — fall to the runtime
	// path, which bounds-checks it.
	if lit, ok := e.Index.(*ast.IntegerLiteralExpr); ok && !lit.Unsigned {
		if !isShared {
			arr, block, err := l.lowerExpr(block, e.Object)
			if err != nil {
				return nil, nil, err
			}
			return block.NewExtractValue(arr, uint64(lit.Value)), block, nil
		}
		payloadPtr, arrayTy, block, err := l.sharedArrayPayloadPtr(block, e.Object)
		if err != nil {
			return nil, nil, err
		}
		elemPtr := block.NewGetElementPtr(arrayTy, payloadPtr,
			constant.NewInt(lltypes.I64, 0), constant.NewInt(lltypes.I64, lit.Value))
		return block.NewLoad(arrayTy.ElemType, elemPtr), block, nil
	}

	// Runtime (or negative-constant) index: get an addressable `[N x T]*` — an inline
	// array through its own alloca, a `shared` array through its box's payload.
	var arrPtr value.Value
	var arrayTy *lltypes.ArrayType
	var err error
	if isShared {
		arrPtr, arrayTy, block, err = l.sharedArrayPayloadPtr(block, e.Object)
	} else {
		arrPtr, arrayTy, block, err = l.arrayLValue(block, e.Object)
	}
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
	// A local's slot is addressed in place — including a by-reference `mut`
	// parameter's, whose slot is the caller's own array storage. Falling through to
	// the materializing path below would copy that array into a fresh alloca and
	// mutate the copy, which is precisely how a `mut [N]T` used to lose its writes.
	if id, ok := obj.(*ast.IdentifierExpr); ok {
		if slot, found := l.locals[id.Name]; found {
			if elem, err := slotElemType(slot); err == nil {
				if at, ok := elem.(*lltypes.ArrayType); ok {
					return slot, at, block, nil
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

// sharedArrayPayloadPtr lowers a `shared [N]T` object to its box pointer and geps
// to the inline `[N x T]` payload (box → field 1), returning an addressable pointer
// so an index can `getelementptr`+`load` through the box in place — the array
// analogue of unboxSharedData (which loads a whole aggregate out for a `match`).
// Reading through the box *borrows* it (no reference consumed), so the binding's
// own release still fires at its last use / scope exit.
func (l *lowerer) sharedArrayPayloadPtr(block *ir.Block, obj ast.Expression) (value.Value, *lltypes.ArrayType, *ir.Block, error) {
	boxVal, block, err := l.lowerExpr(block, obj)
	if err != nil {
		return nil, nil, nil, err
	}
	ptr, ok := boxVal.Type().(*lltypes.PointerType)
	if !ok {
		return nil, nil, nil, fmt.Errorf("llvm: shared array object is not a box pointer (%s)", boxVal.Type())
	}
	boxTy, ok := ptr.ElemType.(*lltypes.StructType)
	if !ok || len(boxTy.Fields) != boxPayloadField+1 {
		return nil, nil, nil, fmt.Errorf("llvm: shared array box is not { rc, payload } (%s)", boxVal.Type())
	}
	arrayTy, ok := boxTy.Fields[boxPayloadField].(*lltypes.ArrayType)
	if !ok {
		return nil, nil, nil, fmt.Errorf("llvm: shared array payload is not an array (%s)", boxTy.Fields[boxPayloadField])
	}
	payloadPtr := boxPayloadPtr(block, boxTy, boxVal)
	return payloadPtr, arrayTy, block, nil
}

// lowerArrayRepeatExpr lowers `[v; n]` — n copies of one value, as an `[n x T]`.
//
// **The value is evaluated once**, which is the only defensible reading and is what the
// count being a compile-time constant lets us promise: `[next(); 3]` would otherwise be
// three calls, and nothing in the syntax says so. Rust reaches the same place from the
// other side, requiring `Copy` or a const expression precisely so the question cannot
// arise.
//
// Evaluating once is what makes the retains necessary. Each slot is an **owner**, so a
// managed element — a string, a `shared` value, a struct holding one — needs n-1 retains
// beyond the reference the value already carries, or the array's drop glue releases n
// times what was retained once. The array literal `[s, s, s]` needs none of this: it
// lowers three separate uses and the ownership pass retains each.
//
// A count of 0 yields an empty array and drops the value on the floor — it is still
// evaluated, since a program is entitled to whatever the expression does.
func (l *lowerer) lowerArrayRepeatExpr(block *ir.Block, e *ast.ArrayRepeatExpr) (value.Value, *ir.Block, error) {
	recorded, ok := l.recordedType(e)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: no type recorded for array repeat literal")
	}
	// The count is folded here rather than read off the recorded type, because a
	// *dynamic* context records `[]T` and drops the size. A plain fold suffices: the
	// typechecker rewrote a `const` count to the literal it folded to, so by now every
	// count is a literal and this cannot disagree with the size the checker used.
	n64, ok := ast.FoldIntExpr(e.Count)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: array repeat count is not a compile-time integer")
	}
	if dynType, isDyn := recorded.(types.DynamicArrayType); isDyn {
		return l.lowerDynArrayRepeat(block, e, dynType, int(n64))
	}
	arrType, ok := recorded.(types.StaticArrayType)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: array repeat lowering not implemented for %s (only fixed-size arrays)", recorded)
	}
	stackArrType := types.WithAllocation(arrType, types.Stack).(types.StaticArrayType)
	llType, err := l.lowerType(stackArrType)
	if err != nil {
		return nil, nil, err
	}
	arrayTy, ok := llType.(*lltypes.ArrayType)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: array type %s did not lower to an LLVM array", arrType)
	}

	elemVal, block, err := l.lowerExpr(block, e.Value)
	if err != nil {
		return nil, nil, err
	}
	// `[panic("…"); 3]` — the value diverged, so the array is never built.
	if diverged(elemVal, block) {
		return nil, block, nil
	}
	elemVal, err = l.coerceAggregateElem(block, elemVal, arrayTy.ElemType, e.Value)
	if err != nil {
		return nil, nil, err
	}

	n := int(arrayTy.Len)
	// One retain per *additional* owner. Emitted before the aggregate is built so the
	// count is right whatever the element is; `emitRetainValue` is a no-op for a type
	// that owns nothing, which is every scalar and therefore the common case.
	for i := 1; i < n; i++ {
		block, err = l.emitRetainValue(block, elemVal, arrType.ElementType)
		if err != nil {
			return nil, nil, err
		}
	}

	var agg value.Value = constant.NewUndef(arrayTy)
	for i := 0; i < n; i++ {
		agg = block.NewInsertValue(agg, elemVal, uint64(i))
	}
	if types.AllocationOf(recorded) == types.Shared {
		boxed, err := l.lowerBoxShared(block, agg, stackArrType)
		return boxed, block, err
	}
	return agg, block, nil
}
