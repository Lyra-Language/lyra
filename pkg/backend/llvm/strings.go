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
// match pattern's text) as a fat-pointer value { i8* data, i64 len }. The bytes
// are interned in a private, immutable global shaped as a **pinned ref-counted
// box** `{ i64 PinnedRC, [N x i8] }` (rc first, exactly like a heap box), with
// `data` pointing at the first payload byte (`box + rcHeaderSize`) — the same
// layout a `lyra_rc_alloc` box has. This costs no allocation (the box lives in
// static constant data), and the PinnedRC sentinel makes retain/release safe
// no-ops on a literal — so the ownership model can retain/release *any* string
// value uniformly without first asking whether it's a literal or heap-allocated
// (see ALLOCATION.md / STRING_LAYOUT.md). Returns the value in `block` (the two
// insertvalues don't branch).
func (l *lowerer) lowerStringConstant(block *ir.Block, content string) value.Value {
	bytes := []byte(content)
	arrTy := lltypes.NewArray(uint64(len(bytes)), lltypes.I8)
	boxTy := lltypes.NewStruct(lltypes.I64, arrTy) // { rc, payload } — a pinned static box
	pinned := constant.NewInt(lltypes.I64, -1)     // PinnedRC bit pattern: retain/release no-op
	g := l.module.NewGlobalDef(fmt.Sprintf(".str.%d", l.strLitCount),
		constant.NewStruct(boxTy, pinned, constant.NewCharArray(bytes)))
	g.Immutable = true
	g.Linkage = enum.LinkagePrivate
	l.strLitCount++

	zero := constant.NewInt(lltypes.I32, 0)
	one := constant.NewInt(lltypes.I32, 1)
	// i8* to the first payload byte: &box.payload[0] == box + rcHeaderSize.
	dataPtr := constant.NewGetElementPtr(boxTy, g, zero, one, zero)
	strTy := StringLLVMType()
	withPtr := block.NewInsertValue(constant.NewUndef(strTy), dataPtr, 0)
	return block.NewInsertValue(withPtr, constant.NewInt(lltypes.I64, int64(len(bytes))), 1)
}

// stringBox recovers the ref-counted box pointer from a string fat pointer. The
// data pointer always points rcHeaderSize bytes past the box header (the i64
// refcount) — for a heap string (lyra_rc_alloc) and a literal (a pinned static
// box) alike — so box = data - rcHeaderSize. This uniformity is what lets retain
// and release operate on any string value without distinguishing the two.
func (l *lowerer) stringBox(block *ir.Block, str value.Value) value.Value {
	data := block.NewExtractValue(str, 0)
	return block.NewGetElementPtr(lltypes.I8, data, constant.NewInt(lltypes.I64, -rcHeaderSize))
}

// lowerStringRetain / lowerStringRelease emit a refcount bump / drop on a string
// value's box. Both are safe on any string: a literal's box is pinned (PinnedRC),
// so the runtime no-ops; only a heap string's count actually moves, and release
// frees it at zero. release passes a null drop_fn — a string's payload is plain
// bytes with no nested owned resources.
func (l *lowerer) lowerStringRetain(block *ir.Block, str value.Value) {
	l.ensureRCRuntime()
	block.NewCall(l.rcRetain, l.stringBox(block, str))
}

func (l *lowerer) lowerStringRelease(block *ir.Block, str value.Value) {
	l.ensureRCRuntime()
	null := constant.NewNull(lltypes.NewPointer(lltypes.I8))
	block.NewCall(l.rcRelease, l.stringBox(block, str), null)
}

// stringSentinel returns an empty string value — a pinned static box, so
// releasing it is a no-op. The ownership lowering writes it into a binding's slot
// after a last-use drop/transfer, so the scope-exit frame release of that slot
// harmlessly releases the sentinel instead of double-freeing (or reading) the
// already-retired box.
func (l *lowerer) stringSentinel(block *ir.Block) value.Value {
	return l.lowerStringConstant(block, "")
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

// memcpyFunc lazily declares libc's `i8* @memcpy(i8*, i8*, i64)` (clang links
// libc), caching it so every concatenation shares one declaration.
func (l *lowerer) memcpyFunc() *ir.Func {
	if l.memcpy == nil {
		i8ptr := lltypes.NewPointer(lltypes.I8)
		l.memcpy = l.module.NewFunc("memcpy", i8ptr,
			ir.NewParam("", i8ptr), ir.NewParam("", i8ptr), ir.NewParam("", lltypes.I64))
	}
	return l.memcpy
}

// lowerStringConcat lowers `a ++ b`. A concatenated string is the first value
// this backend puts on the heap: it can't point into a constant global the way a
// literal does (its bytes don't exist until run time), so it allocates a
// ref-counted box (rcAllocPayload → lyra_rc_alloc), memcpy's both operands'
// bytes in, and returns a fat pointer { data, la+lb } into the box's payload.
//
// The two operands are ordinary fat pointers regardless of where *their* bytes
// live (literal global, another heap box, a parameter), so this composes: a
// chain `a ++ b ++ c` just concatenates left-to-right, each step allocating a
// fresh box. memcpy over a zero length is a valid no-op, so an empty operand
// needs no special case.
//
// Ownership: the box is never freed in this slice — a heap string leaks (no
// double-free or use-after-free, just unreclaimed memory). Release-on-scope-exit
// via lyra_rc_release is the deferred ownership story (ALLOCATION.md); the box
// header is already in place for it.
func (l *lowerer) lowerStringConcat(block *ir.Block, e *ast.StringConcatExpr) (value.Value, *ir.Block, error) {
	left, block, err := l.lowerExpr(block, e.Left)
	if err != nil {
		return nil, nil, err
	}
	right, block, err := l.lowerExpr(block, e.Right)
	if err != nil {
		return nil, nil, err
	}

	dataA := block.NewExtractValue(left, 0)
	lenA := block.NewExtractValue(left, 1)
	dataB := block.NewExtractValue(right, 0)
	lenB := block.NewExtractValue(right, 1)
	total := block.NewAdd(lenA, lenB)

	_, dst := l.rcAllocPayload(block, total)
	memcpy := l.memcpyFunc()
	block.NewCall(memcpy, dst, dataA, lenA)          // dst[0 .. lenA)  = a
	tail := block.NewGetElementPtr(lltypes.I8, dst, lenA)
	block.NewCall(memcpy, tail, dataB, lenB)         // dst[lenA .. total) = b

	strTy := StringLLVMType()
	withPtr := block.NewInsertValue(constant.NewUndef(strTy), dst, 0)
	return block.NewInsertValue(withPtr, total, 1), block, nil
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
