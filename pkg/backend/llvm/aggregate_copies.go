package llvm

import (
	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/constant"
	lltypes "github.com/llir/llvm/ir/types"
	"github.com/llir/llvm/ir/value"
)

// copyLargeAggregates moves large aggregates through memory rather than as whole values,
// once the module is built, rather than at each of the many sites that move one — as
// [storeAggregate] does where a site asks it. Three rewrites, in order:
//
//   - **a field read out of a whole loaded value** (`extractvalue (load %Studio, p), 3`)
//     becomes a load of that field (`load (gep p, 0, 3)`), made where the whole load was, so
//     it reads memory when the load did ([readFieldsAlone]);
//   - **a store of a value put together field by field** (an `insertvalue` chain, as a
//     struct literal or a record update builds one) becomes a store of each field in place,
//     where the whole store was — the fields are values, so nothing is read at another time
//     ([storeFieldsInPlace]);
//   - **a copy made as a whole load and a whole store** becomes one `memcpy`
//     ([copyWithMemcpy]), or, where memory may change between them or the value is stored
//     twice, a `memcpy` to a snapshot where it was loaded and one from it at each store
//     ([snapshotStoredLoads]).
//
// **A whole value is what LLVM's optimizer is superlinear in.** It splits each whole load
// and store into one access per field (SROA's `.fca.` names), and a struct of structs —
// Vega's `Studio` — becomes hundreds of instructions an access, in one block, which the SLP
// vectorizer and the instruction scheduler are quadratic over: one part of Vega's checks
// grew from 2 MB of IR to 65 MB under -O2 and took 82 s to compile (10/30).
//
// Nothing volatile is touched: an interrupt handler's globals are, and keep their accesses
// as written.
func copyLargeAggregates(m *ir.Module) {
	for _, f := range m.Funcs {
		if len(f.Blocks) == 0 {
			continue
		}
		readFieldsAlone(f)
		storeFieldsInPlace(m, f)
		copyWithMemcpy(m, f)
		snapshotStoredLoads(m, f)
		dropDeadValues(f)
		unnumber(f)
	}
}

// unnumber clears the numbers of f's unnamed values, so they are numbered afresh when the
// module is printed: a function numbered before the rewrites keeps its numbers, and the
// instructions they added, all 0, printed as `%0` beside a parameter of that name.
func unnumber(f *ir.Func) {
	type numbered interface {
		IsUnnamed() bool
		SetID(int64)
	}
	clear := func(v any) {
		if n, ok := v.(numbered); ok && n.IsUnnamed() {
			n.SetID(0)
		}
	}
	for _, p := range f.Params {
		clear(p)
	}
	for _, block := range f.Blocks {
		clear(block)
		for _, inst := range block.Insts {
			clear(inst)
		}
		clear(block.Term)
	}
}

// readFieldsAlone replaces each field taken out of a large loaded value with a load of that
// field alone, inserted right after the whole load; the whole load goes when nothing else
// uses it.
func readFieldsAlone(f *ir.Func) {
	users := usersOf(f)
	replaced := map[value.Value]value.Value{}
	gone := map[ir.Instruction]bool{}
	for _, block := range f.Blocks {
		var out []ir.Instruction
		for _, inst := range block.Insts {
			out = append(out, inst)
			ld, ok := inst.(*ir.InstLoad)
			if !ok || ld.Volatile || ld.Atomic || !isLarge(ld.ElemType) {
				continue
			}
			left := 0
			for _, u := range users[ld] {
				ev, ok := u.(*ir.InstExtractValue)
				if !ok || ev.X != ld {
					left++
					continue
				}
				at, ty := fieldAddress(ld.Src, ld.ElemType, ev.Indices)
				narrow := ir.NewLoad(ty, at)
				out = append(out, at, narrow)
				replaced[ev] = narrow
				gone[ev] = true
			}
			if left == 0 {
				gone[ld] = true
			}
		}
		block.Insts = out
	}
	if len(replaced) == 0 {
		return
	}
	for _, block := range f.Blocks {
		kept := block.Insts[:0]
		for _, inst := range block.Insts {
			if !gone[inst] {
				kept = append(kept, inst)
			}
		}
		block.Insts = kept
	}
	replaceOperands(f, replaced)
}

// storeFieldsInPlace replaces each store of a large value put together by `insertvalue`
// with a store of each field at its address in the destination, the earliest first, so a
// field set twice keeps the later; the value it was put into first is stored before them
// (nothing for `undef`, zeroes for `zeroinitializer`).
func storeFieldsInPlace(m *ir.Module, f *ir.Func) {
	for _, block := range f.Blocks {
		var out []ir.Instruction
		changed := false
		for _, inst := range block.Insts {
			st, ok := inst.(*ir.InstStore)
			if !ok || st.Volatile || st.Atomic || !isLarge(st.Src.Type()) {
				out = append(out, inst)
				continue
			}
			iv, ok := st.Src.(*ir.InstInsertValue)
			if !ok {
				out = append(out, inst)
				continue
			}
			var chain []*ir.InstInsertValue
			var base value.Value = iv
			for {
				in, ok := base.(*ir.InstInsertValue)
				if !ok {
					break
				}
				chain = append(chain, in)
				base = in.X
			}
			changed = true
			ty := st.Src.Type()
			switch base.(type) {
			case *constant.Undef:
			case *constant.ZeroInitializer:
				size := sizeOf(ty)
				i8ptr := lltypes.NewPointer(lltypes.I8)
				dst := ir.NewBitCast(st.Dst, i8ptr)
				memset := intrinsic(m, "llvm.memset.p0i8.i64", lltypes.Void, i8ptr, lltypes.I8, lltypes.I64, lltypes.I1)
				out = append(out, dst, ir.NewCall(memset, dst, constant.NewInt(lltypes.I8, 0), size, constant.False))
			default:
				out = append(out, ir.NewStore(base, st.Dst))
			}
			for i := len(chain) - 1; i >= 0; i-- {
				at, _ := fieldAddress(st.Dst, ty, chain[i].Indices)
				out = append(out, at, ir.NewStore(chain[i].Elem, at))
			}
		}
		if changed {
			block.Insts = out
		}
	}
}

// copyWithMemcpy turns `%v = load T, p` then `store T %v, q`, T large, into a `memcpy` from
// p to q. **The copy reads p at the store, where the load read it earlier**, so it is made
// only when nothing between them can write memory: the load is in the store's block, used
// by nothing else, and between them stand only instructions that write nothing.
func copyWithMemcpy(m *ir.Module, f *ir.Func) {
	users := usersOf(f)
	for _, block := range f.Blocks {
		var out []ir.Instruction
		changed := false
		for _, inst := range block.Insts {
			st, ok := inst.(*ir.InstStore)
			if !ok || st.Volatile || st.Atomic {
				out = append(out, inst)
				continue
			}
			ld, ok := st.Src.(*ir.InstLoad)
			if !ok || ld.Volatile || ld.Atomic || !isLarge(ld.ElemType) || len(users[ld]) != 1 {
				out = append(out, inst)
				continue
			}
			at := loadedSince(out, ld)
			if at < 0 {
				out = append(out, inst)
				continue
			}
			// The load goes; the copy is made here, from where it read.
			out = append(out[:at], out[at+1:]...)
			changed = true
			if ld.Src == st.Dst {
				continue // `a = a`: already there, and memcpy must not overlap itself
			}
			i8ptr := lltypes.NewPointer(lltypes.I8)
			memcpy := intrinsic(m, "llvm.memcpy.p0i8.p0i8.i64", lltypes.Void, i8ptr, i8ptr, lltypes.I64, lltypes.I1)
			dst := ir.NewBitCast(st.Dst, i8ptr)
			src := ir.NewBitCast(ld.Src, i8ptr)
			out = append(out, dst, src, ir.NewCall(memcpy, dst, src, sizeOf(ld.ElemType), constant.False))
		}
		if changed {
			block.Insts = out
		}
	}
}

// snapshotStoredLoads takes each large load whose every use is a store of it — the stores
// [copyWithMemcpy] could not make a copy, since a call stands between, or it is stored
// twice — and copies what it read to a slot of its own where it was loaded, and from that
// slot at each store. The bytes stored are the ones the load read, whatever ran between.
// A record update is the usual case: its source is loaded whole first and stored at the
// end, after the calls that retained its fields, to be released.
func snapshotStoredLoads(m *ir.Module, f *ir.Func) {
	users := usersOf(f)
	snapshot := map[*ir.InstLoad]*ir.InstAlloca{}
	var slots []ir.Instruction // in the loads' order, so the text is the same every build
	for _, block := range f.Blocks {
		for _, inst := range block.Insts {
			ld, ok := inst.(*ir.InstLoad)
			if !ok || ld.Volatile || ld.Atomic || !isLarge(ld.ElemType) || len(users[ld]) == 0 {
				continue
			}
			only := true
			for _, u := range users[ld] {
				st, ok := u.(*ir.InstStore)
				if !ok || st.Src != ld || st.Dst == ld || st.Volatile || st.Atomic {
					only = false
					break
				}
			}
			if only {
				slot := ir.NewAlloca(ld.ElemType)
				snapshot[ld] = slot
				slots = append(slots, slot)
			}
		}
	}
	if len(snapshot) == 0 {
		return
	}
	i8ptr := lltypes.NewPointer(lltypes.I8)
	memcpy := intrinsic(m, "llvm.memcpy.p0i8.p0i8.i64", lltypes.Void, i8ptr, i8ptr, lltypes.I64, lltypes.I1)
	copyInsts := func(dst, src value.Value, ty lltypes.Type) []ir.Instruction {
		d := ir.NewBitCast(dst, i8ptr)
		s := ir.NewBitCast(src, i8ptr)
		return []ir.Instruction{d, s, ir.NewCall(memcpy, d, s, sizeOf(ty), constant.False)}
	}
	for _, block := range f.Blocks {
		var out []ir.Instruction
		for _, inst := range block.Insts {
			switch in := inst.(type) {
			case *ir.InstLoad:
				if slot, ok := snapshot[in]; ok {
					out = append(out, copyInsts(slot, in.Src, in.ElemType)...)
					continue
				}
			case *ir.InstStore:
				if ld, ok := in.Src.(*ir.InstLoad); ok {
					if slot, ok := snapshot[ld]; ok {
						out = append(out, copyInsts(in.Dst, slot, ld.ElemType)...)
						continue
					}
				}
			}
			out = append(out, inst)
		}
		block.Insts = out
	}
	// The slots go first in the entry block, as every alloca the lowering makes for a
	// local does, so a loop does not grow the stack.
	entry := f.Blocks[0]
	entry.Insts = append(slots, entry.Insts...)
}

// loadedSince is ld's index in insts, the block so far, if nothing after it there can write
// memory; -1 otherwise.
func loadedSince(insts []ir.Instruction, ld *ir.InstLoad) int {
	for i := len(insts) - 1; i >= 0; i-- {
		switch in := insts[i].(type) {
		case *ir.InstLoad:
			if in == ld {
				return i
			}
			if in.Volatile || in.Atomic {
				return -1
			}
		case *ir.InstAlloca, *ir.InstBitCast, *ir.InstGetElementPtr, *ir.InstPtrToInt,
			*ir.InstIntToPtr, *ir.InstExtractValue, *ir.InstInsertValue:
		default:
			return -1
		}
	}
	return -1
}

// dropDeadValues removes what the rewrites left unused — the values a chain put together,
// the whole loads fields were read from, the addresses no store took — until none is left.
// Only instructions that do nothing but make a value: a non-volatile load among them.
func dropDeadValues(f *ir.Func) {
	for {
		users := usersOf(f)
		removed := false
		for _, block := range f.Blocks {
			kept := block.Insts[:0]
			for _, inst := range block.Insts {
				if v, ok := inst.(value.Value); ok && len(users[v]) == 0 && onlyMakesAValue(inst) {
					removed = true
					continue
				}
				kept = append(kept, inst)
			}
			block.Insts = kept
		}
		if !removed {
			return
		}
	}
}

func onlyMakesAValue(inst ir.Instruction) bool {
	switch in := inst.(type) {
	case *ir.InstInsertValue, *ir.InstExtractValue, *ir.InstGetElementPtr, *ir.InstBitCast:
		return true
	case *ir.InstLoad:
		return !in.Volatile && !in.Atomic
	}
	return false
}

// fieldAddress is the address of the field `indices` names inside the `ty` at ptr, and the
// field's type: a struct's field by an i32, an array's element by an i64.
func fieldAddress(ptr value.Value, ty lltypes.Type, indices []uint64) (*ir.InstGetElementPtr, lltypes.Type) {
	idx := []value.Value{constant.NewInt(lltypes.I32, 0)}
	at := ty
	for _, i := range indices {
		switch t := at.(type) {
		case *lltypes.StructType:
			idx = append(idx, constant.NewInt(lltypes.I32, int64(i)))
			at = t.Fields[i]
		case *lltypes.ArrayType:
			idx = append(idx, constant.NewInt(lltypes.I64, int64(i)))
			at = t.ElemType
		}
	}
	return ir.NewGetElementPtr(ty, ptr, idx...), at
}

// usersOf is, for each value in f, the instructions and terminators using it — once for
// each time it is an operand.
func usersOf(f *ir.Func) map[value.Value][]any {
	users := map[value.Value][]any{}
	add := func(user any) {
		ops, ok := user.(interface{ Operands() []*value.Value })
		if !ok {
			return
		}
		for _, op := range ops.Operands() {
			users[*op] = append(users[*op], user)
			// An argument with attributes (`zeroext`) is the value wrapped: its use is the
			// value's.
			if arg, ok := (*op).(*ir.Arg); ok {
				users[arg.Value] = append(users[arg.Value], user)
			}
		}
	}
	for _, block := range f.Blocks {
		for _, inst := range block.Insts {
			add(inst)
		}
		add(block.Term)
	}
	return users
}

// replaceOperands makes every use in f of a key of replaced a use of its value.
func replaceOperands(f *ir.Func, replaced map[value.Value]value.Value) {
	replace := func(user any) {
		ops, ok := user.(interface{ Operands() []*value.Value })
		if !ok {
			return
		}
		for _, op := range ops.Operands() {
			if r, found := replaced[*op]; found {
				*op = r
			}
			if arg, ok := (*op).(*ir.Arg); ok {
				if r, found := replaced[arg.Value]; found {
					arg.Value = r
				}
			}
		}
	}
	for _, block := range f.Blocks {
		for _, inst := range block.Insts {
			replace(inst)
		}
		replace(block.Term)
	}
}

// intrinsic is the module's declaration of the intrinsic `name` — the one the lowering
// declared ([copyBytes], [zeroBytes]), or declared now.
func intrinsic(m *ir.Module, name string, ret lltypes.Type, params ...lltypes.Type) *ir.Func {
	for _, f := range m.Funcs {
		if f.Name() == name {
			return f
		}
	}
	ps := make([]*ir.Param, len(params))
	for i, t := range params {
		ps[i] = ir.NewParam("", t)
	}
	return m.NewFunc(name, ret, ps...)
}
