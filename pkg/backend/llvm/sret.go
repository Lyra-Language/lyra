package llvm

import (
	"github.com/Lyra-Language/lyra/pkg/ast"
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

// **A large aggregate passed by value is the same cost**, and gets the same answer: a
// parameter of one is a pointer to a copy the caller makes for the call ([passByCopy]) —
// `(xs: [8192]u32)` took six seconds to build. The callee binds the pointer as the
// parameter's slot, so nothing in the body changes; the caller copies with `memcpy` from
// where the argument was loaded ([callLyra]), so a by-value parameter still cannot see the
// caller's later writes, nor write back to it. A closure's parameters stay values, the thunk
// adapting, as for a return.

// sretMinBytes is the size from which an aggregate is returned through memory — a threshold,
// so llSizeOf's sum of field sizes (without padding) is near enough; a copy's size is
// [sizeOf]'s. Strings,
// small tuples and structs, and short arrays stay values: they are what the backend's
// aggregate handling is shaped for, and a struct of a few fields returns in registers.
const sretMinBytes = 256

// returnsInMemory reports whether a function returning t returns it through an `sret` slot: a
// large aggregate, on every target ([inMemory]). (The 68000 was left out for a day: its
// copies had come back empty, which looked like the M68k LLVM's `sret` and was a `memcpy` of
// the wrong length — see [copyBytes].)
func (l *lowerer) returnsInMemory(t lltypes.Type) bool {
	return l.inMemory(t)
}

// inMemory reports whether an aggregate of type t is returned through an `sret` slot and
// passed as a pointer to a copy: a large one on every target, and **every one on the
// 68000**. There the M68k LLVM's own way with an aggregate it cannot return in registers —
// demoting the return to a hidden pointer — puts that pointer in `a0` at the call and reads
// it from `d0` in a `fastcc` callee, every argument after it one register off: a
// `(u8, u8, u8)` from a function given a 31-byte struct came back wrong, but only once the
// function had two callers and was no longer inlined (10/14, `std.chips.music`'s FM writes).
// An explicit slot is an ordinary argument, which both sides agree on.
func (l *lowerer) inMemory(t lltypes.Type) bool {
	if l.arch == M68k {
		switch t.(type) {
		case *lltypes.ArrayType, *lltypes.StructType:
			return true
		}
	}
	return isLarge(t)
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

// passByCopy makes irParam, a by-value parameter of a large aggregate, a pointer to the
// caller's copy, and records it so the body binds it and each call copies for it.
func (l *lowerer) passByCopy(param ast.Parameter, irParam *ir.Param) {
	if paramIsByRef(param) || !l.inMemory(irParam.Typ) {
		return
	}
	irParam.Typ = lltypes.NewPointer(irParam.Typ)
	l.byCopyParams[irParam] = true
}

// valueParamType is p's type as a caller has the argument: the aggregate itself for a
// parameter passed by copy.
func (l *lowerer) valueParamType(p *ir.Param) lltypes.Type {
	if l.byCopyParams[p] {
		return p.Typ.(*lltypes.PointerType).ElemType
	}
	return p.Typ
}

// copyForParam is the argument a for parameter p: for a parameter passed by copy, a copy of
// it in a slot of the caller's entry block, and the slot; anything else, a itself — an
// argument already copied included. **Taken as each argument is made**, where a load it was
// made by is still the last thing written to its memory, so the copy is a `memcpy`
// ([storeAggregate]); made at the call, after the later arguments' code had run, it was a
// store of the whole value, and `at(table, seven())` built in 2.3 seconds rather than 0.4.
func (l *lowerer) copyForParam(block *ir.Block, a value.Value, p *ir.Param) value.Value {
	if !l.byCopyParams[p] || a.Type().Equal(p.Typ) {
		return a
	}
	slot := block.Parent.Blocks[0].NewAlloca(a.Type())
	l.storeAggregate(block, a, slot)
	return slot
}

// callLyra calls a Lyra function and produces its result. Each argument for a parameter passed
// by copy is copied into a slot of the caller's entry block and the slot passed; for a function
// returning through memory, a slot is passed first and loaded after.
func (l *lowerer) callLyra(block *ir.Block, fn *ir.Func, args []value.Value) value.Value {
	params := l.lyraParams(fn)
	for i, a := range args {
		if i < len(params) {
			args[i] = l.copyForParam(block, a, params[i])
		}
	}
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
	l.copyBytes(block, dest, ld.Src, sizeOf(val.Type()))
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
	l.zeroBytes(block, slot, sizeOf(ty))
}

// copyBytes copies size bytes from src to dest with `llvm.memcpy`, and zeroBytes zeroes them
// with `llvm.memset`. **LLVM's own, never libc's declared here**: the intrinsics take an
// `i64` length on every target and are lowered with the target's own `size_t`, where a
// declared `memcpy(i8*, i8*, i64)` passes eight bytes to a 68000 runtime reading four — the
// high half of the length, 0, so nothing was copied, and a large struct returned through
// memory came back as whatever its slot held (10/11).
func (l *lowerer) copyBytes(block *ir.Block, dest, src, size value.Value) {
	i8ptr := lltypes.NewPointer(lltypes.I8)
	fn := l.coroIntrinsic("llvm.memcpy.p0i8.p0i8.i64", lltypes.Void, i8ptr, i8ptr, lltypes.I64, lltypes.I1)
	block.NewCall(fn, block.NewBitCast(dest, i8ptr), block.NewBitCast(src, i8ptr), size, constant.False)
}

func (l *lowerer) zeroBytes(block *ir.Block, dest, size value.Value) {
	i8ptr := lltypes.NewPointer(lltypes.I8)
	fn := l.coroIntrinsic("llvm.memset.p0i8.i64", lltypes.Void, i8ptr, lltypes.I8, lltypes.I64, lltypes.I1)
	block.NewCall(fn, block.NewBitCast(dest, i8ptr), constant.NewInt(lltypes.I8, 0), size, constant.False)
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
