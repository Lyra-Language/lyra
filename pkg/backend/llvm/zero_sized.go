package llvm

import (
	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/constant"
	lltypes "github.com/llir/llvm/ir/types"
	"github.com/llir/llvm/ir/value"
)

// dropZeroSizedAccesses removes every load and store of a value that takes no space — a
// `struct Marker {}`, an element of a `[]Marker`, a `void` payload's `{}` — once the module
// is built, rather than at each of the many sites that move a value through memory.
//
// **Such an access means nothing, and one tool does not survive it.** clang drops a
// zero-sized store, but its AddressSanitizer checks one at `address - 1`, finds the
// allocation's red zone, and crashes writing the report ("nested bug in the same thread",
// 10/29: `[Marker {}, Marker {}]` under the backend's ASan tests). A load is replaced by
// the value it could only have produced, `zeroinitializer`, wherever it was used.
func dropZeroSizedAccesses(m *ir.Module) {
	for _, f := range m.Funcs {
		replaced := map[value.Value]value.Value{}
		for _, block := range f.Blocks {
			kept := block.Insts[:0]
			for _, inst := range block.Insts {
				switch in := inst.(type) {
				case *ir.InstStore:
					if takesNoSpace(in.Src.Type()) {
						continue
					}
				case *ir.InstLoad:
					if takesNoSpace(in.ElemType) {
						replaced[in] = constant.NewZeroInitializer(in.ElemType)
						continue
					}
				}
				kept = append(kept, inst)
			}
			block.Insts = kept
		}
		if len(replaced) == 0 {
			continue
		}
		replace := func(user any) {
			ops, ok := user.(interface{ Operands() []*value.Value })
			if !ok {
				return
			}
			for _, op := range ops.Operands() {
				if r, found := replaced[*op]; found {
					*op = r
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
}

// takesNoSpace reports whether a value of t occupies no bytes: an empty struct, a struct of
// such, an array of none or of such. A pointer always takes space, which is also what ends
// the walk on a recursive type.
func takesNoSpace(t lltypes.Type) bool {
	switch tt := t.(type) {
	case *lltypes.StructType:
		if tt.Opaque {
			return false
		}
		for _, f := range tt.Fields {
			if !takesNoSpace(f) {
				return false
			}
		}
		return true
	case *lltypes.ArrayType:
		return tt.Len == 0 || takesNoSpace(tt.ElemType)
	}
	return false
}
