package llvm

import (
	"math/big"

	"github.com/llir/llvm/ir/constant"
	lltypes "github.com/llir/llvm/ir/types"
)

// The signed minimum / maximum of an integer type are `-2^(w-1)` and `2^(w-1)-1`.
// The obvious `1 << (w-1)` idiom is wrong for a 128-bit type: the untyped `1`
// takes Go's `int` (64-bit), so `1 << 127` shifts past the width and yields 0.
// (It only works at w == 64 by two's-complement wraparound luck.) These helpers
// build the bound with big.Int, so they are correct at every width — the same
// representation constant.NewIntFromString produces (a *big.Int in the constant).

// intMinConst returns the two's-complement minimum of a signed integer type,
// `-2^(w-1)`, as an LLVM constant of that type.
func intMinConst(intTy *lltypes.IntType) *constant.Int {
	x := new(big.Int).Lsh(big.NewInt(1), uint(intTy.BitSize-1)) // 2^(w-1)
	x.Neg(x)                                                    // -2^(w-1)
	return &constant.Int{Typ: intTy, X: x}
}

// intMaxConst returns the signed maximum of an integer type, `2^(w-1)-1`, as an
// LLVM constant of that type.
func intMaxConst(intTy *lltypes.IntType) *constant.Int {
	x := new(big.Int).Lsh(big.NewInt(1), uint(intTy.BitSize-1)) // 2^(w-1)
	x.Sub(x, big.NewInt(1))                                     // 2^(w-1) - 1
	return &constant.Int{Typ: intTy, X: x}
}

// i32c and i64c are the LLVM integer constants this package spells most often — a GEP
// index, a length, a byte count, a tag.
//
// They lived in dynarray.go, where they were first needed, and most of the package went on
// writing `constant.NewInt(lltypes.I64, n)` in full: 120 inline I64 constants and 82 I32
// ones against 47 and 21 uses of these, with single files mixing both spellings. The sweep
// that fixed that is why they sit here instead, beside the other integer-constant helpers —
// and why these two definitions are written against `constant.NewInt` directly, which is
// now the only place in the package that names it for an I32 or I64.
func i32c(n int64) *constant.Int { return constant.NewInt(lltypes.I32, n) }
func i64c(n int64) *constant.Int { return constant.NewInt(lltypes.I64, n) }
