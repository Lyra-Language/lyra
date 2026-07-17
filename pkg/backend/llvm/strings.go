package llvm

import (
	"fmt"

	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/constant"
	"github.com/llir/llvm/ir/enum"
	lltypes "github.com/llir/llvm/ir/types"
	"github.com/llir/llvm/ir/value"

	"github.com/Lyra-Language/lyra/pkg/ast"
)

// lowerStringConstant materializes a compile-time string (a literal's bytes, or a
// match pattern's text) as a fat-pointer value { i8* data, i64 len }: it interns
// the bytes in a private, immutable global `[N x i8]` and builds the struct from
// a pointer to that global's first byte plus the byte length. No allocation — the
// bytes live in the module's constant data. Returns the value in `block` (the two
// insertvalues don't branch).
func (l *lowerer) lowerStringConstant(block *ir.Block, content string) value.Value {
	bytes := []byte(content)
	arrTy := lltypes.NewArray(uint64(len(bytes)), lltypes.I8)
	g := l.module.NewGlobalDef(fmt.Sprintf(".str.%d", l.strLitCount), constant.NewCharArray(bytes))
	g.Immutable = true
	g.Linkage = enum.LinkagePrivate
	l.strLitCount++

	zero := constant.NewInt(lltypes.I32, 0)
	dataPtr := constant.NewGetElementPtr(arrTy, g, zero, zero) // i8* to the first byte
	strTy := StringLLVMType()
	withPtr := block.NewInsertValue(constant.NewUndef(strTy), dataPtr, 0)
	return block.NewInsertValue(withPtr, constant.NewInt(lltypes.I64, int64(len(bytes))), 1)
}

// memcmpFunc lazily declares libc's `i32 @memcmp(i8*, i8*, i64)` (clang links
// libc), caching it so string comparisons share one declaration.
func (l *lowerer) memcmpFunc() *ir.Func {
	if l.memcmp == nil {
		i8ptr := lltypes.NewPointer(lltypes.I8)
		l.memcmp = l.module.NewFunc("memcmp", lltypes.I32,
			ir.NewParam("", i8ptr), ir.NewParam("", i8ptr), ir.NewParam("", lltypes.I64))
	}
	return l.memcmp
}

// lowerStringEquality builds the i1 "are these two strings equal?" test,
// branchlessly: strings are equal iff their byte lengths match AND the first
// min(la, lb) bytes compare equal. memcmp over min(la, lb) never reads past
// either buffer (so it's memory-safe even when the lengths differ — the length
// check then rejects), and n = 0 is a valid no-op compare (two empty strings are
// equal). Returns an i1 in `block`.
func (l *lowerer) lowerStringEquality(block *ir.Block, a, b value.Value) value.Value {
	pa := block.NewExtractValue(a, 0)
	la := block.NewExtractValue(a, 1)
	pb := block.NewExtractValue(b, 0)
	lb := block.NewExtractValue(b, 1)

	lenEq := block.NewICmp(enum.IPredEQ, la, lb)
	aShorter := block.NewICmp(enum.IPredULT, la, lb)
	n := block.NewSelect(aShorter, la, lb) // min(la, lb)
	cmp := block.NewCall(l.memcmpFunc(), pa, pb, n)
	bytesEq := block.NewICmp(enum.IPredEQ, cmp, constant.NewInt(lltypes.I32, 0))
	return block.NewAnd(lenEq, bytesEq)
}

// lowerStringComparison lowers `==`/`!=` on two already-lowered string values.
// Strings support only equality (the typechecker requires numeric operands for
// ordering), so any other operator is an error.
func (l *lowerer) lowerStringComparison(block *ir.Block, op ast.BooleanBinaryOp, left, right value.Value) (value.Value, error) {
	eq := l.lowerStringEquality(block, left, right)
	switch op {
	case ast.BooleanBinaryOpEq:
		return eq, nil
	case ast.BooleanBinaryOpNEq:
		return block.NewXor(eq, constant.NewInt(lltypes.I1, 1)), nil
	default:
		return nil, fmt.Errorf("llvm: string comparison operator %v not implemented (strings support only == and !=)", op)
	}
}

// isStringLLVMType reports whether t is the fat-pointer string representation
// { i8*, i64 } (see StringLLVMType). Used to route comparisons and match tests to
// the string path. A user aggregate can't spell this shape in surface syntax, and
// a struct/tuple scrutinee is dispatched before the scalar path anyway, so there's
// no ambiguity in practice.
func isStringLLVMType(t lltypes.Type) bool {
	st, ok := t.(*lltypes.StructType)
	if !ok || len(st.Fields) != 2 {
		return false
	}
	ptr, ok := st.Fields[0].(*lltypes.PointerType)
	if !ok {
		return false
	}
	elem, ok := ptr.ElemType.(*lltypes.IntType)
	if !ok || elem.BitSize != 8 {
		return false
	}
	lenTy, ok := st.Fields[1].(*lltypes.IntType)
	return ok && lenTy.BitSize == 64
}
