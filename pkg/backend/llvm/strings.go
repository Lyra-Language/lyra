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

// (Refcount bump/drop on a string's box are handled by the type-dispatching
// lowerManagedRetain / lowerManagedRelease in shared.go, which recover the box via
// stringBox above. A literal's box is pinned, so the runtime no-ops on it.)

// lowerStringIndex lowers `s[i]` on a string → the i-th **rune** (code point).
// Because a string is UTF-8, runes aren't randomly addressable: this walks from the
// front, decoding one rune per step, until it has skipped `i` of them, then yields
// that rune. It is therefore O(i) — for a full traversal, prefer `for c in s`. The
// index is a rune index, not a byte offset; running off the end before reaching `i`
// (which includes any negative index, since the rune counter only ever grows) traps
// out-of-bounds, the same trap an array index uses. There is no from-the-end
// (negative) form for a string — that would require a full rune count first.
func (l *lowerer) lowerStringIndex(block *ir.Block, e *ast.IndexExpr) (value.Value, *ir.Block, error) {
	str, block, err := l.lowerExpr(block, e.Object)
	if err != nil {
		return nil, nil, err
	}
	if !isStringLLVMType(str.Type()) {
		return nil, nil, fmt.Errorf("llvm: string index object did not lower to a string (%s)", str.Type())
	}
	data := block.NewExtractValue(str, 0)
	length := block.NewExtractValue(str, 1) // byte length
	idx, block, err := l.lowerExpr(block, e.Index)
	if err != nil {
		return nil, nil, err
	}
	signed, _ := l.getIntSignedness(e.Index)
	target := coerceIntWidth(block, idx, signed, lltypes.I64) // the wanted rune index
	decode := l.utf8DecodeFunc()

	fn := block.Parent
	entry := fn.Blocks[0]
	biSlot := entry.NewAlloca(lltypes.I64)  // byte index
	riSlot := entry.NewAlloca(lltypes.I64)  // rune index
	cpSlot := entry.NewAlloca(lltypes.I32)  // decode out-param
	block.NewStore(constant.NewInt(lltypes.I64, 0), biSlot)
	block.NewStore(constant.NewInt(lltypes.I64, 0), riSlot)

	condBlock := fn.NewBlock("")
	bodyBlock := fn.NewBlock("")
	advBlock := fn.NewBlock("")
	foundBlock := fn.NewBlock("")
	trapBlock := fn.NewBlock("")
	block.NewBr(condBlock)

	// Ran out of bytes before reaching rune `i` → out of bounds.
	bi := condBlock.NewLoad(lltypes.I64, biSlot)
	condBlock.NewCondBr(condBlock.NewICmp(enum.IPredULT, bi, length), bodyBlock, trapBlock)

	trapBlock.NewCall(l.panicIndexOOBFunc())
	trapBlock.NewUnreachable()

	// Decode the rune at the current byte index; if its rune index is the target,
	// we're done — otherwise advance.
	biB := bodyBlock.NewLoad(lltypes.I64, biSlot)
	ri := bodyBlock.NewLoad(lltypes.I64, riSlot)
	n := bodyBlock.NewCall(decode, data, biB, cpSlot)
	bodyBlock.NewCondBr(bodyBlock.NewICmp(enum.IPredEQ, ri, target), foundBlock, advBlock)

	advBlock.NewStore(advBlock.NewAdd(ri, constant.NewInt(lltypes.I64, 1)), riSlot)
	advBlock.NewStore(advBlock.NewAdd(biB, n), biSlot)
	advBlock.NewBr(condBlock)

	return foundBlock.NewLoad(lltypes.I32, cpSlot), foundBlock, nil
}

// utf8DecodeFunc lazily defines
// `i64 @lyra_utf8_decode(i8* data, i64 pos, i32* cpOut)`, which decodes the UTF-8
// sequence starting at data[pos], writes the code point to *cpOut, and returns the
// number of bytes it consumed (1–4). It is the inverse of lyra_rune_to_utf8 (print.go)
// and drives `for c in <string>` (each iteration decodes one rune and advances by the
// returned byte count). Like the encoder it is unvalidated — it reads the lead byte's
// length class and the continuation bytes without checking them (matching rune's
// unvalidated-code-point contract); well-formed UTF-8 (the only kind Lyra can build,
// from literals/concatenation) never straddles the byte length, so the continuation
// reads stay in bounds.
func (l *lowerer) utf8DecodeFunc() *ir.Func {
	if l.utf8Decode != nil {
		return l.utf8Decode
	}
	i8ptr := lltypes.NewPointer(lltypes.I8)
	data := ir.NewParam("data", i8ptr)
	pos := ir.NewParam("pos", lltypes.I64)
	cpOut := ir.NewParam("cpOut", lltypes.NewPointer(lltypes.I32))
	fn := l.module.NewFunc("lyra_utf8_decode", lltypes.I64, data, pos, cpOut)

	entry := fn.NewBlock("entry")
	one := fn.NewBlock("one")       // b0 < 0x80
	twoPlus := fn.NewBlock("twoP")  // else
	two := fn.NewBlock("two")       // (b0 & 0xE0) == 0xC0
	threePlus := fn.NewBlock("triP")
	three := fn.NewBlock("three") // (b0 & 0xF0) == 0xE0
	four := fn.NewBlock("four")   // else

	// byteAt loads data[pos+off] and zero-extends it to i32.
	byteAt := func(b *ir.Block, off int64) value.Value {
		idx := value.Value(pos)
		if off != 0 {
			idx = b.NewAdd(pos, constant.NewInt(lltypes.I64, off))
		}
		return b.NewZExt(b.NewLoad(lltypes.I8, b.NewGetElementPtr(lltypes.I8, data, idx)), lltypes.I32)
	}
	c := func(n int64) *constant.Int { return constant.NewInt(lltypes.I32, n) }
	ret := func(b *ir.Block, cp value.Value, n int64) {
		b.NewStore(cp, cpOut)
		b.NewRet(constant.NewInt(lltypes.I64, n))
	}

	b0 := byteAt(entry, 0)
	entry.NewCondBr(entry.NewICmp(enum.IPredULT, b0, c(0x80)), one, twoPlus)

	ret(one, b0, 1) // ASCII: the byte is the code point

	twoPlus.NewCondBr(twoPlus.NewICmp(enum.IPredEQ, twoPlus.NewAnd(b0, c(0xE0)), c(0xC0)), two, threePlus)
	{
		b1 := byteAt(two, 1)
		cp := two.NewOr(two.NewShl(two.NewAnd(b0, c(0x1F)), c(6)), two.NewAnd(b1, c(0x3F)))
		ret(two, cp, 2)
	}

	threePlus.NewCondBr(threePlus.NewICmp(enum.IPredEQ, threePlus.NewAnd(b0, c(0xF0)), c(0xE0)), three, four)
	{
		b1, b2 := byteAt(three, 1), byteAt(three, 2)
		cp := three.NewOr(three.NewOr(
			three.NewShl(three.NewAnd(b0, c(0x0F)), c(12)),
			three.NewShl(three.NewAnd(b1, c(0x3F)), c(6))),
			three.NewAnd(b2, c(0x3F)))
		ret(three, cp, 3)
	}

	{
		b1, b2, b3 := byteAt(four, 1), byteAt(four, 2), byteAt(four, 3)
		cp := four.NewOr(four.NewOr(four.NewOr(
			four.NewShl(four.NewAnd(b0, c(0x07)), c(18)),
			four.NewShl(four.NewAnd(b1, c(0x3F)), c(12))),
			four.NewShl(four.NewAnd(b2, c(0x3F)), c(6))),
			four.NewAnd(b3, c(0x3F)))
		ret(four, cp, 4)
	}

	l.utf8Decode = fn
	return fn
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

// lowerInterpolatedString lowers a `"… ${expr} …"` to a heap string. It's the
// N-segment generalization of lowerStringConcat where each segment is first
// *formatted* to bytes: a literal chunk is already a string, an interpolated
// expression is rendered per its type by formatForPrint (the same int/float/
// bool/rune/string → (data, length) machinery print uses). Each formatted
// segment is a (data, length) pair; the segments are then concatenated into one
// fresh ref-counted box, so the result is an owned heap string exactly like `++`
// (the ownership pass already treats it as an owned producer whose segments are
// borrowed).
//
// The formatted-bytes pointers are read-then-copied: formatForPrint hands back
// pointers into per-segment stack buffers (numeric/rune) or interned globals
// (bool/literal), which don't alias across segments — each numeric format is its
// own entry-block alloca — so holding every pair through the length sum before
// the single memcpy pass is safe.
func (l *lowerer) lowerInterpolatedString(block *ir.Block, e *ast.InterpolatedStringExpr) (value.Value, *ir.Block, error) {
	type segment struct {
		data, length value.Value
	}
	var segs []segment
	total := value.Value(constant.NewInt(lltypes.I64, 0))

	for _, seg := range e.Segments {
		val, blk, err := l.lowerExpr(block, seg)
		if err != nil {
			return nil, nil, err
		}
		block = blk // a segment expression (e.g. an `if`) may move the insertion block
		segType, ok := l.recordedType(seg)
		if !ok {
			return nil, nil, fmt.Errorf("llvm: no type recorded for interpolation segment")
		}
		data, length, err := l.formatForPrint(block, val, segType)
		if err != nil {
			return nil, nil, err
		}
		segs = append(segs, segment{data: data, length: length})
		total = block.NewAdd(total, length)
	}

	_, dst := l.rcAllocPayload(block, total)
	memcpy := l.memcpyFunc()
	offset := value.Value(constant.NewInt(lltypes.I64, 0))
	for _, s := range segs {
		target := block.NewGetElementPtr(lltypes.I8, dst, offset)
		block.NewCall(memcpy, target, s.data, s.length)
		offset = block.NewAdd(offset, s.length)
	}

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
