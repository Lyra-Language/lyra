package llvm

import (
	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/constant"
	lltypes "github.com/llir/llvm/ir/types"
	"github.com/llir/llvm/ir/value"
)

// A function returning a large aggregate returns it **through memory**: it takes a pointer
// to the caller's slot first, marked `sret`, writes its result there and returns void —
// what C compilers do with a large struct.
//
// **Returned as a value, a large aggregate is the one shape LLVM cannot afford.** A fixed
// array is a first-class `[N x T]`, and `ret [N x T]` is lowered by code generation into N
// values: a function returning an `[8192]u32` took clang 262 seconds at `-O2`, a
// `[2048]u32` 8 (10/11, a game painting its tile art). Measured on that function, returning
// through an `sret` slot with a `memcpy` is 0.18 seconds; neither keeping the aggregate out
// of the function's locals nor copying it to a temporary first made any difference while
// the `ret` remained, so this is the convention itself, not a workaround around it.
//
// **Only the convention changes, never a value.** A call still produces the aggregate, as a
// load of the caller's slot ([callLyra]) — a load from memory only the call wrote is cheap —
// and a return still takes one, written to the slot ([storeResult]): copied with `memcpy`
// from where it was loaded when it was loaded, so the array a function built is never
// loaded whole. Every function declared through declareFunctionAs follows it — plain
// functions, generic specializations, trait methods — and each call of one goes through
// [callLyra]. A closure keeps returning by value: the thunk that makes a named function a
// value ([closureThunk]) adapts it, so the closure calling convention is untouched.

// sretMinBytes is the size from which an aggregate is returned through memory — a threshold,
// so llSizeOf's sum of field sizes (without padding) is near enough; a copy's size is
// [sizeOf]'s. Strings,
// small tuples and structs, and short arrays stay values: they are what the backend's
// aggregate handling is shaped for, and a struct of a few fields returns in registers.
const sretMinBytes = 256

// returnsInMemory reports whether a function returning t returns it through an `sret` slot:
// a large aggregate, on the host. A console keeps returning by value — see [isLarge].
func (l *lowerer) returnsInMemory(t lltypes.Type) bool {
	return l.res.Target.Host && isLarge(t)
}

// isLarge reports whether t is an aggregate of [sretMinBytes] or more.
func isLarge(t lltypes.Type) bool {
	switch t.(type) {
	case *lltypes.ArrayType, *lltypes.StructType:
		return llSizeOf(t) >= sretMinBytes
	}
	return false
}

// sretParam is the hidden first parameter of a function returning ty through memory.
func sretParam(ty lltypes.Type) *ir.Param {
	p := ir.NewParam("sret", lltypes.NewPointer(ty))
	p.Attrs = append(p.Attrs, ir.SRet{Typ: ty})
	return p
}

// resultType is what a call of fn produces: its return type, or for a function returning
// through memory the aggregate it writes.
func (l *lowerer) resultType(fn *ir.Func) lltypes.Type {
	if ty, ok := l.sretResults[fn]; ok {
		return ty
	}
	return fn.Sig.RetType
}

// lyraParams is fn's parameters as its Lyra declaration has them — without the hidden
// `sret` slot of a function returning through memory.
func (l *lowerer) lyraParams(fn *ir.Func) []*ir.Param {
	if _, ok := l.sretResults[fn]; ok {
		return fn.Params[1:]
	}
	return fn.Params
}

// callLyra calls a Lyra function and produces its result: for one returning through memory,
// a slot in the caller's entry block is passed first and loaded after.
func (l *lowerer) callLyra(block *ir.Block, fn *ir.Func, args []value.Value) value.Value {
	ty, ok := l.sretResults[fn]
	if !ok {
		return callWithDeclaredAttrs(block, fn, args)
	}
	slot := block.Parent.Blocks[0].NewAlloca(ty)
	callWithDeclaredAttrs(block, fn, append([]value.Value{slot}, args...))
	return block.NewLoad(ty, slot)
}

// storeResult writes a returned aggregate to the caller's slot, as [storeAggregate] stores.
func (l *lowerer) storeResult(block *ir.Block, val value.Value, dest value.Value) {
	l.storeAggregate(block, val, dest)
}

// storeAggregate stores val at dest — a return's slot, a binding's — and, for a large
// aggregate loaded from memory (a local array, a parameter, a call's result slot), copies it
// from there with `memcpy` instead, leaving the load unused: a load and store of the whole
// aggregate is what LLVM's optimizer is superlinear in, and `memcpy` is what it is good at.
// `let a = fill()` and `var art = #[0; n]` went from 26 seconds at 28,672 words to a
// fraction of one. Anything smaller is an ordinary store.
//
// **The copy reads the memory now, where the store would have used the value loaded then**,
// so it is taken only when nothing between the load and here can have written that memory —
// the load is in this block and nothing after it stores or calls. A return that copied after
// its function's releases read a field of a box those releases had just freed, and Vega's
// own drop glue crashed (10/11). Anything else is stored as it was.
func (l *lowerer) storeAggregate(block *ir.Block, val value.Value, dest value.Value) {
	ld, ok := val.(*ir.InstLoad)
	if !ok || !ld.ElemType.Equal(val.Type()) || !isLarge(val.Type()) || !untouchedSince(block, ld) {
		block.NewStore(val, dest)
		return
	}
	if ld.Src == dest {
		return // `a = a`: already there, and memcpy must not overlap itself
	}
	i8ptr := lltypes.NewPointer(lltypes.I8)
	block.NewCall(l.memcpyFunc(),
		block.NewBitCast(dest, i8ptr),
		block.NewBitCast(ld.Src, i8ptr),
		sizeOf(val.Type()))
}

// untouchedSince reports whether ld is in block and nothing after it there can write memory:
// no store, no call. Its source then still holds what it loaded.
func untouchedSince(block *ir.Block, ld *ir.InstLoad) bool {
	at := -1
	for i, inst := range block.Insts {
		if inst == ld {
			at = i
			break
		}
	}
	if at < 0 {
		return false
	}
	for _, inst := range block.Insts[at+1:] {
		switch inst.(type) {
		case *ir.InstStore, *ir.InstCall:
			return false
		}
	}
	return true
}

// zeroFill zeroes the `ty` at slot: for a large aggregate with `memset`, since a store of a
// whole `zeroinitializer` is as costly to LLVM as a store of any whole aggregate value — it
// was 20 of the 20.5 seconds a function filling a `[28672]u32` still took to build — and
// otherwise with that store.
func (l *lowerer) zeroFill(block *ir.Block, slot value.Value, ty lltypes.Type) {
	if !isLarge(ty) {
		block.NewStore(constant.NewZeroInitializer(ty), slot)
		return
	}
	i8ptr := lltypes.NewPointer(lltypes.I8)
	memset, _ := l.declareLibc("memset", i8ptr, i8ptr, lltypes.I32, lltypes.I64)
	block.NewCall(memset, block.NewBitCast(slot, i8ptr), i32c(0), sizeOf(ty))
}

// sizeOf is ty's allocation size as the target lays it out — `ptrtoint (gep T, T* null, 1)`,
// a constant LLVM folds with the target's own data layout. Never a sum of field sizes: that
// misses the padding between them, and a `memcpy` of a struct holding a `Result` (a one-byte
// tag, then an 8-aligned payload) came up 7 bytes short and left the copy's last field
// half-written (10/11, found by Vega).
func sizeOf(ty lltypes.Type) constant.Constant {
	ptr := lltypes.NewPointer(ty)
	end := constant.NewGetElementPtr(ty, constant.NewNull(ptr), constant.NewInt(lltypes.I64, 1))
	return constant.NewPtrToInt(end, lltypes.I64)
}
