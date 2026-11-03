package llvm

import (
	"fmt"
	"strings"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/constant"
	"github.com/llir/llvm/ir/enum"
	lltypes "github.com/llir/llvm/ir/types"
	"github.com/llir/llvm/ir/value"
)

// **A `@banked` const on the Z80** (LANGUAGE.md) is a table in a ROM bank: more cartridge than
// the Z80's 64 KB can see at once, reached through the 16 KB window at 8000. Its global is in a
// section of its own, `.bank.<symbol>`, which the linker gives a bank (pkg/rom/z80.go).
//
// Every instruction that names one calls `__z80_bank(bank, table)` first and uses the pointer
// it answers. That pointer is the whole of the ordering: the optimizer may move a read of constant
// memory past a store to the mapper, but not ahead of the call that makes its address. And the
// call is opaque, so the table's contents are not folded into code, which would put them back in
// the fixed part of the ROM. The bank's number is the address of an external symbol,
// `__lyra_bank.<symbol>`, which only the linker can answer.

const (
	bankSectionPrefix = ".bank."
	bankSymbolPrefix  = "__lyra_bank."
)

// isBanked reports whether a const's initializer carries `@banked`.
func isBanked(value ast.Expression) bool {
	switch table := value.(type) {
	case *ast.ArrayLiteralExpr:
		return table.Banked
	case *ast.ArrayRepeatExpr:
		return table.Banked
	}
	return false
}

// operandUser is what every instruction and terminator in the module is.
type operandUser interface {
	Operands() []*value.Value
}

// selectBanks rewrites each use of a banked table in m to go through `__z80_bank`.
func selectBanks(m *ir.Module) error {
	banked := map[*ir.Global]bool{}
	for _, g := range m.Globals {
		if strings.HasPrefix(g.Section, bankSectionPrefix) {
			banked[g] = true
		}
	}
	if len(banked) == 0 {
		return nil
	}
	bytePointer := lltypes.NewPointer(lltypes.I8)
	sel := m.NewFunc("__z80_bank", bytePointer, ir.NewParam("bank", lltypes.I16), ir.NewParam("table", bytePointer))
	numbers := map[*ir.Global]*ir.Global{}
	number := func(g *ir.Global) constant.Constant {
		sym, ok := numbers[g]
		if !ok {
			sym = m.NewGlobal(bankSymbolPrefix+g.Name(), lltypes.I8)
			sym.Linkage = enum.LinkageExternal
			numbers[g] = sym
		}
		return constant.NewPtrToInt(sym, lltypes.I16)
	}

	for _, fn := range m.Funcs {
		for _, block := range fn.Blocks {
			var insts []ir.Instruction
			// The call for g, ahead of what is being rewritten, as a pointer of g's type.
			through := func(g *ir.Global) value.Value {
				call := ir.NewCall(sel, number(g), constant.NewBitCast(g, bytePointer))
				cast := ir.NewBitCast(call, g.Type())
				insts = append(insts, call, cast)
				return cast
			}
			rewrite := func(user operandUser) error {
				for _, operand := range user.Operands() {
					if operand == nil || *operand == nil {
						continue
					}
					if g, ok := (*operand).(*ir.Global); ok {
						if banked[g] {
							*operand = through(g)
						}
						continue
					}
					if c, isConstant := (*operand).(constant.Expression); isConstant {
						for g := range banked {
							if strings.Contains(c.Ident(), g.Ident()) {
								return fmt.Errorf("llvm: %s is used in a constant expression, where no bank can be selected", g.Name())
							}
						}
					}
				}
				return nil
			}
			for _, inst := range block.Insts {
				if user, ok := inst.(operandUser); ok {
					if err := rewrite(user); err != nil {
						return err
					}
				}
				insts = append(insts, inst)
			}
			if user, ok := block.Term.(operandUser); ok {
				if err := rewrite(user); err != nil {
					return err
				}
			}
			block.Insts = insts
		}
	}
	return nil
}
