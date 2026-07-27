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
// Covers construction from a literal, indexing (bounds-checked against the runtime
// len, negative-from-end), by-value flow through let/params/returns, and — via
// dynArrayDropFn — *managed* element types (`[]string`, `[][]T`): the box's drop
// glue loops over the runtime len and releases each element (the elements transfer
// their reference into the box at construction, like a tuple/struct's fields).
// Deferred, loud errors: iteration (`for x in xs`), `match` on `[]T`, `.len()`, and
// growth (no grow operation exists in the language yet).

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

// lowerArrayLen lowers `xs.len()`: a fixed-size array's length is its compile-time
// size (a constant), a dynamic array's is the runtime `len` field of its box. The
// return type is i64 (builtins.go). Reading the length borrows the array (no
// reference consumed), so there is no ownership action on the receiver.
func (l *lowerer) lowerArrayLen(block *ir.Block, call *ast.FunctionCallExpr, member *ast.MemberExpr) (value.Value, *ir.Block, error) {
	if len(call.Arguments) != 0 {
		return nil, nil, fmt.Errorf("llvm: len() expects 0 arguments, got %d", len(call.Arguments))
	}
	recvT, ok := l.res.TypeTable.Get(member.Object)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: no type recorded for len() receiver")
	}
	switch it := recvT.(type) {
	case types.StaticArrayType:
		// Constant length, but the receiver may still have an effect (e.g.
		// `makeArray().len()`), so lower it for effect and discard the value.
		_, block, err := l.lowerExpr(block, member.Object)
		if err != nil {
			return nil, nil, err
		}
		return i64c(int64(it.Size)), block, nil
	case types.DynamicArrayType:
		elem, err := l.lowerType(it.ElementType)
		if err != nil {
			return nil, nil, err
		}
		boxTy := DynArrayBoxType(elem)
		box, block, err := l.lowerExpr(block, member.Object)
		if err != nil {
			return nil, nil, err
		}
		length := block.NewLoad(lltypes.I64, block.NewGetElementPtr(boxTy, box, i32c(0), i32c(1)))
		return length, block, nil
	}
	return nil, nil, fmt.Errorf("llvm: len() on non-array receiver %s not implemented", recvT)
}

// dynArrayDropFn returns the drop_fn to pass when releasing a `[]T` box: null when
// T owns nothing managed (the box just frees), else a generated function that loops
// over the runtime length and releases each element. It is the dynamic-length
// counterpart to the unrolled emitDropArray a fixed-size `shared [N]T` uses.
//
// The function receives the box *payload* — `box + rcHeaderSize`, i.e. the
// `{ i64 len, [0 x T] }` past the refcount — as an i8*, per lyra_rc_release's drop_fn
// contract; it loads len (payload field 0) and drops each element of the flexible
// tail (field 1). Generated once per element type and cached in l.dropFns (before
// the body, so a `[]` whose element type reaches itself terminates).
func (l *lowerer) dynArrayDropFn(dyn types.DynamicArrayType) (value.Value, error) {
	elemLyra := dyn.ElementType
	if !l.needsDrop(elemLyra) {
		return nullDropFn(), nil
	}
	key := dyn.String()
	if fn, ok := l.dropFns[key]; ok {
		return constant.NewBitCast(fn, lltypes.NewPointer(lltypes.I8)), nil
	}
	elemLL, err := l.lowerType(elemLyra)
	if err != nil {
		return nil, err
	}
	payloadTy := lltypes.NewStruct(lltypes.I64, lltypes.NewArray(0, elemLL)) // { len, [0 x T] }
	fn := l.module.NewFunc(l.dropFnName(key), lltypes.Void, ir.NewParam("payload", lltypes.NewPointer(lltypes.I8)))
	l.dropFns[key] = fn // cache before building the body

	entry := fn.NewBlock("entry")
	p := entry.NewBitCast(fn.Params[0], lltypes.NewPointer(payloadTy))
	length := entry.NewLoad(lltypes.I64, entry.NewGetElementPtr(payloadTy, p, i32c(0), i32c(0)))
	iSlot := entry.NewAlloca(lltypes.I64)
	entry.NewStore(i64c(0), iSlot)

	cond := fn.NewBlock("loopcond")
	body := fn.NewBlock("loopbody")
	exit := fn.NewBlock("exit")
	entry.NewBr(cond)

	cond.NewCondBr(cond.NewICmp(enum.IPredSLT, cond.NewLoad(lltypes.I64, iSlot), length), body, exit)

	i := body.NewLoad(lltypes.I64, iSlot)
	elem := body.NewLoad(elemLL, body.NewGetElementPtr(payloadTy, p, i32c(0), i32c(1), i))
	end, err := l.emitDropValue(body, elem, elemLyra) // may branch (a `data` element)
	if err != nil {
		return nil, err
	}
	end.NewStore(end.NewAdd(i, i64c(1)), iSlot)
	end.NewBr(cond)

	exit.NewRet(nil)
	return constant.NewBitCast(fn, lltypes.NewPointer(lltypes.I8)), nil
}
