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
// `{ i64 rc, i64 weak, i64 len, i64 cap, T* elems }` — the refcount header, the element
// count, the buffer's capacity, and a pointer to the elements. Modelling it as a single
// box pointer (rather than a `{ data, len }` fat pointer) means it reuses the
// shared-value managed machinery unchanged: the value *is* a pointer, so managedBox /
// retain / release act on it directly, and the ownership pass frees it like any managed
// value (IsManaged covers `[]T`). Every `[]T` — even empty — is a real box, so
// retain/release stay uniform (no null special case).
//
// **The elements are in their own buffer, and that indirection is what makes `[]T`
// growable** (08/09). They used to sit inline in a `[0 x T]` tail: one allocation and
// one less load per access, and impossible to grow — a `[]T` value is the box pointer,
// so elements that moved would move the box and leave every other binding holding a
// dangling one. Aliasing is observable (`let b = a; a[0] = 9` reads through `b`), so
// that is a use-after-free rather than a choice about semantics. With the elements
// behind a pointer the box address never changes, so every alias sees a `push` — the
// same reference semantics `[]T` already had for element assignment. The cost is one
// load per element access, which is what every growable reference container pays.
//
// Covers construction from a literal, indexing (bounds-checked against the runtime
// len, negative-from-end), iteration, `.len()`, `push`, by-value flow through
// let/params/returns, and — via dynArrayDropFn — *managed* element types (`[]string`,
// `[][]T`): the box's drop glue loops over the runtime len releasing each element, then
// frees the buffer (the elements transfer their reference into the box at construction
// and at push, like a tuple/struct's fields). Deferred, loud error: `match` on a
// `shared` array.

// lowerDynArrayConstruction builds a `[]T` literal: allocate a ref-counted box
// sized to hold the elements, store the length and each element, and yield the box
// pointer. An empty literal still allocates a (len-0) box, keeping every dynamic
// array a uniform managed box.
func (l *lowerer) lowerDynArrayConstruction(block *ir.Block, e *ast.ArrayLiteralExpr, dynType types.DynamicArrayType) (value.Value, *ir.Block, error) {
	box, boxTy, elemLL, err := l.dynArrayBox(block, dynType.ElementType, len(e.Elements))
	if err != nil {
		return nil, nil, err
	}

	// Store each element into the flexible tail (field 2, index i).
	for i, elemExpr := range e.Elements {
		var v value.Value
		v, block, err = l.lowerExpr(block, elemExpr)
		if err != nil {
			return nil, nil, err
		}
		if diverged(v, block) {
			return nil, block, nil
		}
		v, err = l.coerceAggregateElem(block, v, elemLL, elemExpr)
		if err != nil {
			return nil, nil, err
		}
		elemPtr := dynArrayElemPtr(block, boxTy, box, i64c(int64(i)))
		block.NewStore(v, elemPtr)
	}
	return box, block, nil
}

// dynArrayBox allocates a `[]T` box able to hold n elements and stores the length,
// returning the box pointer, its LLVM struct type and the element's LLVM type.
//
// Shared by the literal and the repeat form rather than copied into each: the box
// layout, the header size and the stride rounding are the parts a second copy would get
// subtly wrong, and they are also the parts that change when the representation does.
// What the two callers do differ about — one value per slot, or one value in every slot
// — is the loop, which is theirs.
func (l *lowerer) dynArrayBox(block *ir.Block, elemLyra types.Type, n int) (value.Value, *lltypes.StructType, lltypes.Type, error) {
	if elemLyra == nil {
		return nil, nil, nil, fmt.Errorf("llvm: dynamic array has no element type")
	}
	elemLL, err := l.lowerType(elemLyra)
	if err != nil {
		return nil, nil, nil, err
	}
	elemSize, elemAlign, ok := SizeAndAlign(l.resolveForLayout(elemLyra))
	if !ok {
		return nil, nil, nil, fmt.Errorf("llvm: cannot size dynamic array element type %s", elemLyra)
	}
	stride := alignUp(elemSize, elemAlign)

	boxTy := DynArrayBoxType(elemLL)
	box := l.dynArrayAlloc(block, boxTy, elemLL, i64c(int64(n)), i64c(int64(n)), int64(stride))
	return box, boxTy, elemLL, nil
}

// dynArrayAlloc allocates a `[]T` box and its element buffer, storing the length, the
// capacity and the buffer pointer. It is the one place that knows a dynamic array is two
// allocations rather than one, so the three construction sites — a literal, a
// comprehension, and a match's rest-binding tail — cannot disagree about it.
//
// The box is a *fixed* size now; only the buffer scales with the count. `malloc(0)` for
// an empty array is deliberate rather than a null: it returns a pointer `free` accepts,
// so the drop glue needs no null case, and `realloc` grows it like any other. Every
// `[]T` is still a real box, so retain/release stay uniform.
func (l *lowerer) dynArrayAlloc(block *ir.Block, boxTy *lltypes.StructType, elemLL lltypes.Type, length, capacity value.Value, stride int64) value.Value {
	l.ensureRCRuntime()
	boxI8 := block.NewCall(l.rcAlloc, i64c(int64(dynArrayBoxSize))) // i8*, rc = 1
	box := block.NewBitCast(boxI8, lltypes.NewPointer(boxTy))
	block.NewStore(length, dynArrayLenPtr(block, boxTy, box))
	block.NewStore(capacity, dynArrayCapPtr(block, boxTy, box))
	buf := block.NewCall(l.malloc, block.NewMul(capacity, i64c(stride)))
	block.NewStore(block.NewBitCast(buf, lltypes.NewPointer(elemLL)), dynArrayElemsPtr(block, boxTy, box))
	return box
}

// lowerDynArrayRepeatRuntime builds `[v; n]` where **n is only known at run time** — the
// buffer a window resize or a terminal width sizes, which had no spelling at all before
// 08/14 (`push` in a loop was the workaround, and allocated once per element).
//
// It is the constant path below with two differences, and only two: the length reaches
// `dynArrayAlloc` as a value rather than a literal, and the loop bound is that same value.
// Everything else — evaluate the value once, retain per slot beyond the first, store into
// every slot — is the promise the constant form already makes, so the two are deliberately
// the same shape rather than the same function: the constant one still unrolls below
// `repeatUnrollLimit`, which this cannot do and should not pretend to.
//
// **A negative count traps.** The constant path refuses one at compile time; this is the
// same rule at the only moment the value exists. Zero is fine and yields an empty array,
// exactly as `[]` does.
func (l *lowerer) lowerDynArrayRepeatRuntime(block *ir.Block, e *ast.ArrayRepeatExpr, dynType types.DynamicArrayType) (value.Value, *ir.Block, error) {
	v, block, err := l.lowerExpr(block, e.Value)
	if err != nil {
		return nil, nil, err
	}
	if diverged(v, block) {
		return nil, block, nil
	}
	count, block, err := l.lowerExpr(block, e.Count)
	if err != nil {
		return nil, nil, err
	}
	if diverged(count, block) {
		return nil, block, nil
	}
	signed, _ := l.getIntSignedness(e.Count)
	n := coerceIntWidth(block, count, signed, lltypes.I64)
	block = l.emitTrapIf(block, block.NewICmp(enum.IPredSLT, n, i64c(0)), l.panicNegativeLengthFunc())

	elemLL, err := l.lowerType(dynType.ElementType)
	if err != nil {
		return nil, nil, err
	}
	elemSize, elemAlign, sized := SizeAndAlign(l.resolveForLayout(dynType.ElementType))
	if !sized {
		return nil, nil, fmt.Errorf("llvm: cannot size dynamic array element type %s", dynType.ElementType)
	}
	boxTy := DynArrayBoxType(elemLL)
	box := l.dynArrayAlloc(block, boxTy, elemLL, n, n, int64(alignUp(elemSize, elemAlign)))

	v, err = l.coerceAggregateElem(block, v, elemLL, e.Value)
	if err != nil {
		return nil, nil, err
	}
	if l.needsDrop(dynType.ElementType) {
		block, err = l.emitCountedLoop(block, i64c(1), n, func(body *ir.Block, _ value.Value) (*ir.Block, error) {
			return l.emitRetainValue(body, v, dynType.ElementType)
		})
		if err != nil {
			return nil, nil, err
		}
	}
	block, err = l.emitCountedLoop(block, i64c(0), n, func(body *ir.Block, i value.Value) (*ir.Block, error) {
		body.NewStore(v, dynArrayElemPtr(body, boxTy, box, i))
		return body, nil
	})
	if err != nil {
		return nil, nil, err
	}
	return box, block, nil
}

// lowerDynArrayRepeat builds `[v; n]` in a **dynamic**-array context — the heap
// counterpart of lowerArrayRepeatExpr's fixed-size path, and it makes the same promise:
// the value is evaluated once, and each of the n slots is an owner, so a managed element
// takes n-1 extra retains.
func (l *lowerer) lowerDynArrayRepeat(block *ir.Block, e *ast.ArrayRepeatExpr, dynType types.DynamicArrayType, n int) (value.Value, *ir.Block, error) {
	v, block, err := l.lowerExpr(block, e.Value)
	if err != nil {
		return nil, nil, err
	}
	if diverged(v, block) {
		return nil, block, nil
	}
	box, boxTy, elemLL, err := l.dynArrayBox(block, dynType.ElementType, n)
	if err != nil {
		return nil, nil, err
	}
	v, err = l.coerceAggregateElem(block, v, elemLL, e.Value)
	if err != nil {
		return nil, nil, err
	}
	// **Unrolled while that is cheaper than a loop, and looped once it is not.** Emitting
	// one store per slot is better code for `[0; 3]` — no counter, no branch, and the
	// optimizer sees straight through it — and it is a compile-time bomb at any size a
	// frame buffer reaches: the IR grows *linearly in n*, so `[0; 200000]` produced a
	// 43 MB `.ll` file and clang had not finished with it after five minutes. Nothing
	// diagnosed that; the build simply never returned.
	//
	// The threshold is deliberately low. Above it the per-slot cost is one add, one
	// compare and one store either way, so the unrolled form buys almost nothing while
	// each element still costs its own line of IR.
	if n <= repeatUnrollLimit {
		for i := 1; i < n; i++ {
			block, err = l.emitRetainValue(block, v, dynType.ElementType)
			if err != nil {
				return nil, nil, err
			}
		}
		for i := 0; i < n; i++ {
			block.NewStore(v, dynArrayElemPtr(block, boxTy, box, i64c(int64(i))))
		}
		return box, block, nil
	}

	// A managed element is retained once per slot beyond the first, exactly as the
	// unrolled path does — the loop runs over [1, n) so the value's original reference
	// becomes slot 0's.
	if l.needsDrop(dynType.ElementType) {
		block, err = l.emitCountedLoop(block, i64c(1), i64c(int64(n)), func(body *ir.Block, _ value.Value) (*ir.Block, error) {
			return l.emitRetainValue(body, v, dynType.ElementType)
		})
		if err != nil {
			return nil, nil, err
		}
	}
	block, err = l.emitCountedLoop(block, i64c(0), i64c(int64(n)), func(body *ir.Block, i value.Value) (*ir.Block, error) {
		body.NewStore(v, dynArrayElemPtr(body, boxTy, box, i))
		return body, nil
	})
	if err != nil {
		return nil, nil, err
	}
	return box, block, nil
}

// repeatUnrollLimit is the largest `[v; n]` still emitted as n separate stores.
//
// Small enough that the IR a repeat literal contributes is bounded by a constant a
// reader would accept, and large enough that every repeat literal anyone writes by hand
// stays straight-line. `[0; 64]` is 64 stores; `[0; 65]` is a four-block loop.
const repeatUnrollLimit = 64

// emitCountedLoop emits `for i := lo; i < hi; i++ { body(i) }` as real IR and returns the
// block execution continues in. The body may itself branch — it is handed a block and
// returns the one it ended in — which is what lets a managed element's retain glue run
// inside the loop.
//
// Shaped like the per-element loop in the array drop glue above, which is the other place
// this file walks n slots at run time. Kept as a helper rather than a third copy: the
// pattern is four blocks and an alloca'd counter, and getting the back edge wrong produces
// IR that verifies and does not terminate.
func (l *lowerer) emitCountedLoop(block *ir.Block, lo, hi value.Value, body func(*ir.Block, value.Value) (*ir.Block, error)) (*ir.Block, error) {
	fn := block.Parent
	iSlot := block.NewAlloca(lltypes.I64)
	block.NewStore(lo, iSlot)

	cond := fn.NewBlock("")
	loopBody := fn.NewBlock("")
	exit := fn.NewBlock("")
	block.NewBr(cond)
	cond.NewCondBr(cond.NewICmp(enum.IPredSLT, cond.NewLoad(lltypes.I64, iSlot), hi), loopBody, exit)

	i := loopBody.NewLoad(lltypes.I64, iSlot)
	end, err := body(loopBody, i)
	if err != nil {
		return nil, err
	}
	end.NewStore(end.NewAdd(i, i64c(1)), iSlot)
	end.NewBr(cond)
	return exit, nil
}

// lowerDynArrayIndex lowers `xs[i]` on a dynamic array: load the runtime length
// from the box, bounds-check the index against it ([0, len); a negative traps), then
// GEP+load the element. Unlike a fixed-size array there is no
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
	length := block.NewLoad(lltypes.I64, dynArrayLenPtr(block, boxTy, box))

	idx, block, err := l.lowerExpr(block, e.Index)
	if err != nil {
		return nil, nil, err
	}
	// Widen the index to i64 by its own signedness, then the fixed-size array's
	// single unsigned bound compare against the runtime length — [0, len), with a
	// negative index caught by the same compare via its sign-extension. (The
	// negative-counts-from-the-end reading was removed 08/12; `xs.from_end(k)` is
	// the explicit spelling.)
	signed, _ := l.getIntSignedness(e.Index)
	idx64 := coerceIntWidth(block, idx, signed, lltypes.I64)
	oob := block.NewICmp(enum.IPredUGE, idx64, length)
	block = l.emitTrapIf(block, oob, l.panicIndexOOBFunc())

	elemPtr := dynArrayElemPtr(block, boxTy, box, idx64)
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
	recvT, ok := l.recordedType(member.Object)
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
		length := block.NewLoad(lltypes.I64, dynArrayLenPtr(block, boxTy, box))
		return length, block, nil
	}
	return nil, nil, fmt.Errorf("llvm: len() on non-array receiver %s not implemented", recvT)
}

// lowerArrayFromEnd lowers `xs.from_end(k)` → the k-th element from the end, 1-based
// (`from_end(1)` is the last). The explicit spelling that replaced the negative index
// (08/12): the element is at `len - k`, and the single unsigned compare
// `len - k >= len` catches every bad k at once — `k < 1` wraps the subtraction to or
// past `len`, `k > len` wraps it negative and so to a huge unsigned — which is the
// same one-compare trick the index paths use for [0, len).
func (l *lowerer) lowerArrayFromEnd(block *ir.Block, call *ast.FunctionCallExpr, member *ast.MemberExpr, recvT types.Type) (value.Value, *ir.Block, error) {
	if len(call.Arguments) != 1 {
		return nil, nil, fmt.Errorf("llvm: from_end() expects 1 argument, got %d", len(call.Arguments))
	}
	lowerK := func(b *ir.Block) (value.Value, *ir.Block, error) {
		kV, b, err := l.lowerExpr(b, call.Arguments[0])
		if err != nil {
			return nil, nil, err
		}
		kSigned, _ := l.getIntSignedness(call.Arguments[0])
		return coerceIntWidth(b, kV, kSigned, lltypes.I64), b, nil
	}
	switch it := recvT.(type) {
	case types.StaticArrayType:
		var arrPtr value.Value
		var arrayTy *lltypes.ArrayType
		var err error
		if types.AllocationOf(recvT) == types.Shared {
			arrPtr, arrayTy, block, err = l.sharedArrayPayloadPtr(block, member.Object)
		} else {
			arrPtr, arrayTy, block, err = l.arrayLValue(block, member.Object)
		}
		if err != nil {
			return nil, nil, err
		}
		k, block, err := lowerK(block)
		if err != nil {
			return nil, nil, err
		}
		size := i64c(int64(it.Size))
		idx := block.NewSub(size, k)
		block = l.emitTrapIf(block, block.NewICmp(enum.IPredUGE, idx, size), l.panicIndexOOBFunc())
		elemPtr := block.NewGetElementPtr(arrayTy, arrPtr, i64c(0), idx)
		return block.NewLoad(arrayTy.ElemType, elemPtr), block, nil
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
		length := block.NewLoad(lltypes.I64, dynArrayLenPtr(block, boxTy, box))
		k, block, err := lowerK(block)
		if err != nil {
			return nil, nil, err
		}
		idx := block.NewSub(length, k)
		block = l.emitTrapIf(block, block.NewICmp(enum.IPredUGE, idx, length), l.panicIndexOOBFunc())
		elemPtr := dynArrayElemPtr(block, boxTy, box, idx)
		return block.NewLoad(elem, elemPtr), block, nil
	}
	return nil, nil, fmt.Errorf("llvm: from_end() on non-array receiver %s not implemented", recvT)
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
	// **Always a drop function now, even for `[]i64`.** The elements live in their own
	// malloc'd buffer, so releasing the box has to free that buffer whatever the element
	// type is; returning nullDropFn for a scalar element — which was right while the
	// elements were inline and freed with the box — would leak the buffer of every
	// dynamic array of scalars in the program. The element loop below is what is
	// conditional on needsDrop, not the function.
	key := dyn.String()
	if fn, ok := l.dropFns[key]; ok {
		return constant.NewBitCast(fn, lltypes.NewPointer(lltypes.I8)), nil
	}
	l.ensureRCRuntime() // the body calls free; the glue may be built before any allocation
	elemLL, err := l.lowerType(elemLyra)
	if err != nil {
		return nil, err
	}
	elemPtrTy := lltypes.NewPointer(elemLL)
	// The payload past the refcount header: { len, cap, T* }.
	payloadTy := lltypes.NewStruct(lltypes.I64, lltypes.I64, elemPtrTy)
	fn := l.module.NewFunc(l.dropFnName(key), lltypes.Void, ir.NewParam("payload", lltypes.NewPointer(lltypes.I8)))
	l.dropFns[key] = fn // cache before building the body

	entry := fn.NewBlock("entry")
	p := entry.NewBitCast(fn.Params[0], lltypes.NewPointer(payloadTy))
	elems := entry.NewLoad(elemPtrTy, entry.NewGetElementPtr(payloadTy, p, i32c(0), i32c(2)))
	exit := fn.NewBlock("exit")

	if !l.needsDrop(elemLyra) {
		// Nothing to release per element; the buffer still has to go.
		entry.NewBr(exit)
	} else {
		length := entry.NewLoad(lltypes.I64, entry.NewGetElementPtr(payloadTy, p, i32c(0), i32c(0)))
		iSlot := entry.NewAlloca(lltypes.I64)
		entry.NewStore(i64c(0), iSlot)

		cond := fn.NewBlock("loopcond")
		body := fn.NewBlock("loopbody")
		entry.NewBr(cond)

		cond.NewCondBr(cond.NewICmp(enum.IPredSLT, cond.NewLoad(lltypes.I64, iSlot), length), body, exit)

		i := body.NewLoad(lltypes.I64, iSlot)
		elem := body.NewLoad(elemLL, body.NewGetElementPtr(elemLL, elems, i))
		end, err := l.emitDropValue(body, elem, elemLyra) // may branch (a `data` element)
		if err != nil {
			return nil, err
		}
		end.NewStore(end.NewAdd(i, i64c(1)), iSlot)
		end.NewBr(cond)
	}

	// **After** the elements, never before: an element's own release may read through
	// the buffer it is stored in.
	exit.NewCall(l.free, exit.NewBitCast(elems, lltypes.NewPointer(lltypes.I8)))
	exit.NewRet(nil)
	return constant.NewBitCast(fn, lltypes.NewPointer(lltypes.I8)), nil
}

// lowerDynArrayPush lowers `xs.push(v)`: grow the buffer if it is full, store the
// element at index len, and bump len. Yields void.
//
// **The box never moves**, which is the whole reason the elements live behind a pointer
// (layout.go). A `[]T` value *is* the box pointer, so a growth that relocated the box
// would leave every other binding holding a dangling one — and aliasing is observable
// (`let b = a; a[0] = 9` reads through `b`), so that is a use-after-free rather than a
// choice about semantics. Growing the buffer instead means every alias sees the push,
// which is the reference semantics `[]T` already had for element assignment.
//
// **Amortized doubling**, from a floor of 4: `realloc` is called on a full buffer only,
// so n pushes cost O(n) copying rather than O(n²). The floor keeps a push-in-a-loop from
// reallocating on its first few iterations, which is where a from-empty array spends
// its worst relative cost. `realloc(p, n)` on the `malloc(0)` an empty array carries is
// well-defined and behaves as `malloc(n)`, so there is no empty special case.
//
// The element **transfers** into the box: a managed value pushed here is owned by the
// array from now on, exactly as an element written by a literal is, and the box's drop
// glue releases it over the runtime length. That is why nothing is retained at this site
// — the ownership pass sees a use whose value moves into a container.
func (l *lowerer) lowerDynArrayPush(block *ir.Block, call *ast.FunctionCallExpr, member *ast.MemberExpr, dynType types.DynamicArrayType) (value.Value, *ir.Block, error) {
	if len(call.Arguments) != 1 {
		return nil, nil, fmt.Errorf("llvm: push() expects 1 argument, got %d", len(call.Arguments))
	}
	elemLyra := dynType.ElementType
	elemLL, err := l.lowerType(elemLyra)
	if err != nil {
		return nil, nil, err
	}
	elemSize, elemAlign, ok := SizeAndAlign(l.resolveForLayout(elemLyra))
	if !ok {
		return nil, nil, fmt.Errorf("llvm: cannot size dynamic array element type %s", elemLyra)
	}
	stride := int64(alignUp(elemSize, elemAlign))

	box, block, err := l.lowerExpr(block, member.Object)
	if err != nil {
		return nil, nil, err
	}
	if diverged(box, block) {
		return nil, block, nil
	}
	v, block, err := l.lowerExpr(block, call.Arguments[0])
	if err != nil {
		return nil, nil, err
	}
	if diverged(v, block) {
		return nil, block, nil
	}
	v, err = l.coerceAggregateElem(block, v, elemLL, call.Arguments[0])
	if err != nil {
		return nil, nil, err
	}

	boxTy := DynArrayBoxType(elemLL)
	elemPtrTy := lltypes.NewPointer(elemLL)
	length := block.NewLoad(lltypes.I64, dynArrayLenPtr(block, boxTy, box))
	capacity := block.NewLoad(lltypes.I64, dynArrayCapPtr(block, boxTy, box))

	fn := block.Parent
	growBlock := fn.NewBlock("")
	storeBlock := fn.NewBlock("")
	block.NewCondBr(block.NewICmp(enum.IPredSLT, length, capacity), storeBlock, growBlock)

	// cap == 0 -> 4, else cap * 2. Computed with a select so the growth path stays one
	// basic block.
	doubled := growBlock.NewMul(capacity, i64c(2))
	tooSmall := growBlock.NewICmp(enum.IPredSLT, doubled, i64c(4))
	newCap := growBlock.NewSelect(tooSmall, i64c(4), doubled)
	oldBuf := growBlock.NewLoad(elemPtrTy, dynArrayElemsPtr(growBlock, boxTy, box))
	newBuf := growBlock.NewCall(l.reallocFunc(),
		growBlock.NewBitCast(oldBuf, lltypes.NewPointer(lltypes.I8)),
		growBlock.NewMul(newCap, i64c(stride)))
	growBlock.NewStore(growBlock.NewBitCast(newBuf, elemPtrTy), dynArrayElemsPtr(growBlock, boxTy, box))
	growBlock.NewStore(newCap, dynArrayCapPtr(growBlock, boxTy, box))
	growBlock.NewBr(storeBlock)

	// Re-read the buffer through dynArrayElemPtr rather than threading a phi: the growth
	// path stored the new pointer into the box, so one load answers for both paths.
	storeBlock.NewStore(v, dynArrayElemPtr(storeBlock, boxTy, box, length))
	storeBlock.NewStore(storeBlock.NewAdd(length, i64c(1)), dynArrayLenPtr(storeBlock, boxTy, box))
	return nil, storeBlock, nil
}

// reallocFunc lazily declares libc's `i8* @realloc(i8*, i64)`, cached on the lowerer
// beside malloc and free.
func (l *lowerer) reallocFunc() *ir.Func {
	i8ptr := lltypes.NewPointer(lltypes.I8)
	fn, _ := l.declareLibc("realloc", i8ptr, i8ptr, lltypes.I64)
	return fn
}

// lowerArraySlice lowers `xs.slice(start, end)` → a fresh `[]T` holding the half-open
// element range `[start, end)`.
//
// **It copies.** Sharing the parent's element buffer would need that buffer ref-counted
// apart from the box that owns it, and a `push` on the parent reallocates it — so the
// slice would dangle while the array it came from is perfectly alive. That is the same
// answer the string method gives for its own version of the problem, and the reason
// `noalloc` refuses both.
//
// Two things the copy has to get right, and the second is the one a memcpy alone misses:
//
//   - **The bounds are checked before anything is read**, with the string method's
//     one-test-for-every-way-this-is-wrong shape: a negative bound, either bound past the
//     length, or an inverted range. `end == len` is legal (it names the position one past
//     the last element, so `xs.slice(0, xs.len())` is a copy) and `start == end` yields an
//     empty array rather than a trap, matching `..<`.
//   - **Every copied element is retained**, because each slot in the new box is an owner:
//     a `[]string` slice holds the same pointers the parent does, and without the retains
//     the parent's drop would free strings the slice still points at. This is `[v; n]`'s
//     per-slot rule applied to n *different* values, which is why the loop is over the
//     destination rather than a single retain repeated.
//
// A `[N]T` slices too and yields a `[]T`, since `end - start` is a run-time value and no
// fixed size could be written down.
func (l *lowerer) lowerArraySlice(block *ir.Block, call *ast.FunctionCallExpr, member *ast.MemberExpr, recvT types.Type) (value.Value, *ir.Block, error) {
	if len(call.Arguments) != 2 {
		return nil, nil, fmt.Errorf("llvm: slice() expects 2 arguments, got %d", len(call.Arguments))
	}
	elemLyra, err := sliceElementType(recvT)
	if err != nil {
		return nil, nil, err
	}
	elemLL, err := l.lowerType(elemLyra)
	if err != nil {
		return nil, nil, err
	}
	elemSize, elemAlign, sized := SizeAndAlign(l.resolveForLayout(elemLyra))
	if !sized {
		return nil, nil, fmt.Errorf("llvm: cannot size array element type %s", elemLyra)
	}
	stride := int64(alignUp(elemSize, elemAlign))

	// `srcAt` yields the address of the source's i-th element, and `length` its count.
	// The two receiver kinds differ in exactly this and nothing else, so they are resolved
	// into one shape here rather than duplicating the copy below.
	var (
		length value.Value
		srcAt  func(b *ir.Block, i value.Value) value.Value
	)
	switch it := recvT.(type) {
	case types.StaticArrayType:
		var arrPtr value.Value
		var arrayTy *lltypes.ArrayType
		if types.AllocationOf(recvT) == types.Shared {
			arrPtr, arrayTy, block, err = l.sharedArrayPayloadPtr(block, member.Object)
		} else {
			arrPtr, arrayTy, block, err = l.arrayLValue(block, member.Object)
		}
		if err != nil {
			return nil, nil, err
		}
		length = i64c(int64(it.Size))
		srcAt = func(b *ir.Block, i value.Value) value.Value {
			return b.NewGetElementPtr(arrayTy, arrPtr, i64c(0), i)
		}
	case types.DynamicArrayType:
		boxTy := DynArrayBoxType(elemLL)
		var box value.Value
		box, block, err = l.lowerExpr(block, member.Object)
		if err != nil {
			return nil, nil, err
		}
		if diverged(box, block) {
			return nil, block, nil
		}
		length = block.NewLoad(lltypes.I64, dynArrayLenPtr(block, boxTy, box))
		srcAt = func(b *ir.Block, i value.Value) value.Value {
			return dynArrayElemPtr(b, boxTy, box, i)
		}
	default:
		return nil, nil, fmt.Errorf("llvm: slice() on non-array receiver %s not implemented", recvT)
	}

	start, block, err := l.lowerSliceBound(block, call.Arguments[0])
	if err != nil {
		return nil, nil, err
	}
	end, block, err := l.lowerSliceBound(block, call.Arguments[1])
	if err != nil {
		return nil, nil, err
	}

	// One test for every way this can be wrong, in the order the string method uses: a
	// negative bound, either bound past the length, or an inverted range. `end == length`
	// is legal — it names the position one past the last element — so the comparison is
	// strictly-greater rather than the unsigned trick the *index* paths use, which has no
	// room for an end position.
	zero := i64c(0)
	bad := block.NewOr(
		block.NewOr(
			block.NewICmp(enum.IPredSLT, start, zero),
			block.NewICmp(enum.IPredSLT, end, zero)),
		block.NewOr(
			block.NewICmp(enum.IPredSGT, end, length),
			block.NewICmp(enum.IPredSGT, start, end)))
	block = l.emitTrapIf(block, bad, l.panicArraySliceOOBFunc())

	n := block.NewSub(end, start)
	boxTy := DynArrayBoxType(elemLL)
	out := l.dynArrayAlloc(block, boxTy, elemLL, n, n, stride)

	needRetain := l.needsDrop(elemLyra)
	block, err = l.emitCountedLoop(block, zero, n, func(body *ir.Block, i value.Value) (*ir.Block, error) {
		v := body.NewLoad(elemLL, srcAt(body, body.NewAdd(start, i)))
		body.NewStore(v, dynArrayElemPtr(body, boxTy, out, i))
		if !needRetain {
			return body, nil
		}
		// Per element rather than once: these are n different values, each of which the
		// new box now owns a reference to.
		return l.emitRetainValue(body, v, elemLyra)
	})
	if err != nil {
		return nil, nil, err
	}
	return out, block, nil
}

// lowerSliceBound lowers one bound of a slice call to an i64, widening from whatever
// integer width it was written at. Signed, because the trap tests for a negative one and
// a bound zero-extended from a narrow type could never be negative.
func (l *lowerer) lowerSliceBound(block *ir.Block, arg ast.Expression) (value.Value, *ir.Block, error) {
	v, block, err := l.lowerExpr(block, arg)
	if err != nil {
		return nil, nil, err
	}
	if diverged(v, block) {
		return nil, block, nil
	}
	signed, _ := l.getIntSignedness(arg)
	return coerceIntWidth(block, v, signed, lltypes.I64), block, nil
}

// sliceElementType is the element type of the array `slice` was called on.
func sliceElementType(recvT types.Type) (types.Type, error) {
	switch it := recvT.(type) {
	case types.StaticArrayType:
		return it.ElementType, nil
	case types.DynamicArrayType:
		return it.ElementType, nil
	}
	return nil, fmt.Errorf("llvm: slice() on non-array receiver %s not implemented", recvT)
}
