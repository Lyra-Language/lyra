package llvm

import (
	"fmt"
	"unicode/utf8"

	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/constant"
	"github.com/llir/llvm/ir/enum"
	lltypes "github.com/llir/llvm/ir/types"
	"github.com/llir/llvm/ir/value"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
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
	boxConst, boxTy := pinnedBoxConstant(constant.NewCharArray(bytes))
	g := l.module.NewGlobalDef(fmt.Sprintf(".str.%d", l.strLitCount), boxConst)
	g.Immutable = true
	g.Linkage = enum.LinkagePrivate
	l.strLitCount++

	zero := constant.NewInt(lltypes.I32, 0)
	// i8* to the first payload byte: &box.payload[0] == box + rcHeaderSize.
	dataPtr := constant.NewGetElementPtr(boxTy, g, zero, constant.NewInt(lltypes.I32, boxPayloadField), zero)
	strTy := StringLLVMType()
	withPtr := block.NewInsertValue(constant.NewUndef(strTy), dataPtr, 0)
	withLen := block.NewInsertValue(withPtr, constant.NewInt(lltypes.I64, int64(len(bytes))), 1)
	// The rune count is a compile-time fact for a literal — the one construction
	// where it is literally free.
	return block.NewInsertValue(withLen, constant.NewInt(lltypes.I64, int64(utf8.RuneCountInString(content))), 2)
}

// utf8CountFunc lazily defines `i64 @lyra_utf8_count(i8* data, i64 byteLen)` — the
// number of runes in a byte range, counted as the non-continuation bytes
// (`(b & 0xC0) != 0x80`): every rune contributes exactly one lead byte, so one pass
// with no decoding answers it. This is the fallback for the two string producers
// whose bytes arrive from outside the fat-pointer world (read_line's libc buffer,
// interpolation's formatted segments); every other construction derives its count
// arithmetically (StringLLVMType has the ledger).
func (l *lowerer) utf8CountFunc() *ir.Func {
	if l.utf8Count != nil {
		return l.utf8Count
	}
	i8ptr := lltypes.NewPointer(lltypes.I8)
	data := ir.NewParam("data", i8ptr)
	byteLen := ir.NewParam("byteLen", lltypes.I64)
	fn := l.module.NewFunc("lyra_utf8_count", lltypes.I64, data, byteLen)
	l.utf8Count = fn

	zero := constant.NewInt(lltypes.I64, 0)
	one := constant.NewInt(lltypes.I64, 1)

	entry := fn.NewBlock("entry")
	cond := fn.NewBlock("cond")
	body := fn.NewBlock("body")
	done := fn.NewBlock("done")

	iSlot := entry.NewAlloca(lltypes.I64)
	nSlot := entry.NewAlloca(lltypes.I64)
	entry.NewStore(zero, iSlot)
	entry.NewStore(zero, nSlot)
	entry.NewBr(cond)

	i := cond.NewLoad(lltypes.I64, iSlot)
	cond.NewCondBr(cond.NewICmp(enum.IPredULT, i, byteLen), body, done)

	iB := body.NewLoad(lltypes.I64, iSlot)
	b := body.NewLoad(lltypes.I8, body.NewGetElementPtr(lltypes.I8, data, iB))
	isCont := body.NewICmp(enum.IPredEQ,
		body.NewAnd(b, constant.NewInt(lltypes.I8, -64)), // 0xC0
		constant.NewInt(lltypes.I8, -128))                // 0x80
	n := body.NewLoad(lltypes.I64, nSlot)
	body.NewStore(body.NewSelect(isCont, n, body.NewAdd(n, one)), nSlot)
	body.NewStore(body.NewAdd(iB, one), iSlot)
	body.NewBr(cond)

	done.NewRet(done.NewLoad(lltypes.I64, nSlot))
	return fn
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
//
// Because a string is UTF-8, runes aren't randomly addressable, so the position comes
// from strRuneOffsetFunc and this decodes the one rune there. `i` costs O(i) — for a
// full traversal, prefer `for c in s`.
//
// **A negative index traps** (08/12). It counted from the end from 08/08 until then,
// which the audit called the design's sharpest self-contradiction: in the language
// whose thesis is trap-over-silently-wrong, the most common off-by-one — an index
// underflowing past zero — got a valid read of the wrong element. The performance
// that justified it (a backward byte walk instead of two full decode walks) lives on
// in `s.from_end(k)`, the explicit spelling; strRuneOffsetFunc's negative-idx
// encoding is now that method's internal contract, so the guard here must reject a
// negative *before* the call or it would silently mean from-the-end again.
//
// Out of range traps — its own trap, not the array one, since a string index is a
// rune index and pointing the reader at an array is a wrong turn. The end *position*
// is not a valid index (allowEnd is false), unlike in slice.
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

	negIdx := block.NewICmp(enum.IPredSLT, target, constant.NewInt(lltypes.I64, 0))
	block = l.emitTrapIf(block, negIdx, l.panicStringIndexOOBFunc())

	off := block.NewCall(l.strRuneOffsetFunc(), data, length, target, constant.NewInt(lltypes.I1, 0))

	fn := block.Parent
	foundBlock := fn.NewBlock("")
	trapBlock := fn.NewBlock("")
	block.NewCondBr(block.NewICmp(enum.IPredSLT, off, constant.NewInt(lltypes.I64, 0)), trapBlock, foundBlock)

	trapBlock.NewCall(l.panicStringIndexOOBFunc())
	trapBlock.NewUnreachable()

	cpSlot := fn.Blocks[0].NewAlloca(lltypes.I32) // decode out-param
	foundBlock.NewCall(l.utf8DecodeFunc(), data, off, cpSlot)
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
	one := fn.NewBlock("one")      // b0 < 0x80
	twoPlus := fn.NewBlock("twoP") // else
	two := fn.NewBlock("two")      // (b0 & 0xE0) == 0xC0
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
	// The result's rune count is the operands' sum — concatenation cannot split or
	// merge a rune, so no bytes need looking at. This additivity is most of why the
	// count can afford to ride the value (StringLLVMType).
	count := block.NewAdd(block.NewExtractValue(left, 2), block.NewExtractValue(right, 2))

	_, dst := l.rcAllocPayload(block, total)
	memcpy := l.memcpyFunc()
	block.NewCall(memcpy, dst, dataA, lenA) // dst[0 .. lenA)  = a
	tail := block.NewGetElementPtr(lltypes.I8, dst, lenA)
	block.NewCall(memcpy, tail, dataB, lenB) // dst[lenA .. total) = b

	strTy := StringLLVMType()
	withPtr := block.NewInsertValue(constant.NewUndef(strTy), dst, 0)
	withLen := block.NewInsertValue(withPtr, total, 1)
	return block.NewInsertValue(withLen, count, 2), block, nil
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

	// Formatted segments arrive as raw (data, length) pairs — a number's snprintf
	// buffer carries no rune count — so the result's count is one linear pass over
	// the bytes just copied, cache-hot and dwarfed by the allocation beside it.
	count := block.NewCall(l.utf8CountFunc(), dst, total)

	strTy := StringLLVMType()
	withPtr := block.NewInsertValue(constant.NewUndef(strTy), dst, 0)
	withLen := block.NewInsertValue(withPtr, total, 1)
	return block.NewInsertValue(withLen, count, 2), block, nil
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
// { i8*, i64, i64 } (see StringLLVMType). Used to route comparisons and match tests
// to the string path. A user aggregate can't spell this shape in surface syntax, and
// a struct/tuple scrutinee is dispatched before the scalar path anyway, so there's
// no ambiguity in practice.
func isStringLLVMType(t lltypes.Type) bool {
	st, ok := t.(*lltypes.StructType)
	if !ok || len(st.Fields) != 3 {
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

// strRuneOffsetFunc lazily defines
// `i64 @lyra_str_rune_offset(i8* data, i64 byteLen, i64 idx, i1 allowEnd)`: the **byte**
// offset at which rune `idx` starts, or -1 when there is no such rune.
//
// It is the one definition of "where does rune k begin", and both `s[i]` and
// `s.slice(a, b)` go through it. Each used to carry its own forward walk — the same
// question answered twice, which is the drift hazard the compiler's rule 8 is about, and
// which is why a negative bound could not be added to one without being added to the
// other by hand.
//
// **A negative `idx` walks backward from the end** (-1 is the last rune) — which as
// of 08/12 is an *internal contract*, not a language feature: `s[-1]` is refused at
// the surface (negative indexing was removed — the most common off-by-one got a valid
// read of the wrong element), and the sole caller of the negative branch is
// `s.from_end(k)`, which passes -k. The branch itself is why from_end is cheap: it is
// a *byte* walk that skips continuation bytes (`10xxxxxx`) until it lands on a lead
// byte, well-defined precisely because UTF-8 is self-synchronizing — the property
// `starts_with` and `index` already lean on. So it costs O(k) runes with **no decoding
// at all**, where the positional spelling `s[s.len() - k]` is two full O(n) decode
// walks — the O(n) tax the accessor exists to remove. Every caller that hands this a
// *surface* index must therefore reject a negative before the call, or the value would
// silently mean from-the-end again.
//
// `allowEnd` admits `idx == runeCount`, whose offset is the byte length. `slice` needs
// it (an exclusive end, and `s.slice(n, n)` is the empty string) and indexing must not
// have it (`s[n]` is out of bounds). A negative `idx` never reaches that case, since
// stepping back at least one rune always lands strictly inside.
//
// Out of range returns -1 rather than trapping, so the *caller* raises the panic that
// fits it — the string-index trap and the string-slice trap say different things, and a
// shared helper that trapped would have to pick one.
func (l *lowerer) strRuneOffsetFunc() *ir.Func {
	if l.strRuneOffset != nil {
		return l.strRuneOffset
	}
	i8ptr := lltypes.NewPointer(lltypes.I8)
	data := ir.NewParam("data", i8ptr)
	byteLen := ir.NewParam("byteLen", lltypes.I64)
	idx := ir.NewParam("idx", lltypes.I64)
	allowEnd := ir.NewParam("allowEnd", lltypes.I1)
	fn := l.module.NewFunc("lyra_str_rune_offset", lltypes.I64, data, byteLen, idx, allowEnd)
	l.strRuneOffset = fn

	zero := constant.NewInt(lltypes.I64, 0)
	one := constant.NewInt(lltypes.I64, 1)

	entry := fn.NewBlock("entry")
	fwdInit := fn.NewBlock("fwd")
	fwdCond := fn.NewBlock("fwdCond")
	fwdMore := fn.NewBlock("fwdMore")
	fwdStep := fn.NewBlock("fwdStep")
	fwdFound := fn.NewBlock("fwdFound")
	fwdAtEnd := fn.NewBlock("fwdAtEnd")
	backInit := fn.NewBlock("back")
	backCond := fn.NewBlock("backCond")
	backMore := fn.NewBlock("backMore")
	backStep := fn.NewBlock("backStep")
	skipCond := fn.NewBlock("skipCond")
	skipTest := fn.NewBlock("skipTest")
	skipBody := fn.NewBlock("skipBody")
	backNext := fn.NewBlock("backNext")
	retOK := fn.NewBlock("ok")
	oor := fn.NewBlock("oor")

	// biSlot is the byte cursor throughout; nSlot is the rune counter on the way
	// forward and the number of runes still to step back on the way back.
	biSlot := entry.NewAlloca(lltypes.I64)
	nSlot := entry.NewAlloca(lltypes.I64)
	cpSlot := entry.NewAlloca(lltypes.I32) // decode out-param, discarded here
	entry.NewCondBr(entry.NewICmp(enum.IPredSLT, idx, zero), backInit, fwdInit)

	// Forward: decode rune by rune until the counter reaches idx.
	fwdInit.NewStore(zero, biSlot)
	fwdInit.NewStore(zero, nSlot)
	fwdInit.NewBr(fwdCond)

	fwdCond.NewCondBr(fwdCond.NewICmp(enum.IPredEQ, fwdCond.NewLoad(lltypes.I64, nSlot), idx), fwdFound, fwdMore)
	// Out of bytes before reaching idx: there is no such rune.
	fwdMore.NewCondBr(fwdMore.NewICmp(enum.IPredULT, fwdMore.NewLoad(lltypes.I64, biSlot), byteLen), fwdStep, oor)
	{
		bi := fwdStep.NewLoad(lltypes.I64, biSlot)
		n := fwdStep.NewCall(l.utf8DecodeFunc(), data, bi, cpSlot)
		fwdStep.NewStore(fwdStep.NewAdd(bi, n), biSlot)
		fwdStep.NewStore(fwdStep.NewAdd(fwdStep.NewLoad(lltypes.I64, nSlot), one), nSlot)
		fwdStep.NewBr(fwdCond)
	}
	// The cursor reached idx. Strictly inside the bytes it names a rune; exactly at the
	// end it names the end *position*, which only slice may ask for.
	fwdFound.NewCondBr(fwdFound.NewICmp(enum.IPredULT, fwdFound.NewLoad(lltypes.I64, biSlot), byteLen), retOK, fwdAtEnd)
	fwdAtEnd.NewCondBr(allowEnd, retOK, oor)

	// Backward: step back one rune at a time from the end, skipping continuation bytes.
	// No decoding — the lead-byte test is the whole of it.
	backInit.NewStore(backInit.NewSub(zero, idx), nSlot)
	backInit.NewStore(byteLen, biSlot)
	backInit.NewBr(backCond)

	backCond.NewCondBr(backCond.NewICmp(enum.IPredEQ, backCond.NewLoad(lltypes.I64, nSlot), zero), retOK, backMore)
	// Already at the front with runes left to step: idx reaches past the string.
	backMore.NewCondBr(backMore.NewICmp(enum.IPredEQ, backMore.NewLoad(lltypes.I64, biSlot), zero), oor, backStep)
	backStep.NewStore(backStep.NewSub(backStep.NewLoad(lltypes.I64, biSlot), one), biSlot)
	backStep.NewBr(skipCond)

	// `bi > 0 && isContinuation(data[bi])`, split across two blocks so the byte load
	// happens only when the cursor is in bounds.
	skipCond.NewCondBr(skipCond.NewICmp(enum.IPredUGT, skipCond.NewLoad(lltypes.I64, biSlot), zero), skipTest, backNext)
	{
		bi := skipTest.NewLoad(lltypes.I64, biSlot)
		b := skipTest.NewZExt(skipTest.NewLoad(lltypes.I8, skipTest.NewGetElementPtr(lltypes.I8, data, bi)), lltypes.I32)
		isCont := skipTest.NewICmp(enum.IPredEQ,
			skipTest.NewAnd(b, constant.NewInt(lltypes.I32, 0xC0)), constant.NewInt(lltypes.I32, 0x80))
		skipTest.NewCondBr(isCont, skipBody, backNext)
	}
	skipBody.NewStore(skipBody.NewSub(skipBody.NewLoad(lltypes.I64, biSlot), one), biSlot)
	skipBody.NewBr(skipCond)
	backNext.NewStore(backNext.NewSub(backNext.NewLoad(lltypes.I64, nSlot), one), nSlot)
	backNext.NewBr(backCond)

	retOK.NewRet(retOK.NewLoad(lltypes.I64, biSlot))
	oor.NewRet(constant.NewInt(lltypes.I64, -1))
	return fn
}

// lowerDecodeUTF8 lowers `bytes.decode_utf8()` — a `[]u8` or `[N]u8` read as UTF-8 text.
//
// The shape is `lowerStringConcat`'s: allocate a ref-counted box, memcpy the bytes into
// its payload, and hand back a fat pointer into it. What differs is where the bytes come
// from and how the rune count is got. Concatenation adds its operands' counts because
// joining cannot split or merge a rune; a byte buffer carries no count at all, so this is
// one of the three places that has to walk the bytes (`lyra_utf8_count`, beside
// `read_line` and interpolation's formatted segments).
//
// **The copy is not avoidable.** A string is a ref-counted box whose header sits at its
// start, so a fat pointer into an array's buffer could not be released — the same reason
// `slice` copies rather than borrowing its parent's bytes. It is also what makes the
// result independent of the array: a later `push` may move the buffer, and the string
// must not follow.
func (l *lowerer) lowerDecodeUTF8(block *ir.Block, call *ast.FunctionCallExpr, member *ast.MemberExpr, recvT types.Type) (value.Value, *ir.Block, error) {
	if len(call.Arguments) != 0 {
		return nil, nil, fmt.Errorf("llvm: decode_utf8() expects 0 arguments, got %d", len(call.Arguments))
	}
	src, byteLen, block, err := l.byteBufferOf(block, member.Object, recvT)
	if err != nil {
		return nil, nil, err
	}
	_, dst := l.rcAllocPayload(block, byteLen)
	// memcpy over a zero length is a valid no-op, so an empty buffer needs no special
	// case — it decodes to the empty string, which is the right answer.
	block.NewCall(l.memcpyFunc(), dst, src, byteLen)

	strTy := StringLLVMType()
	withPtr := block.NewInsertValue(constant.NewUndef(strTy), dst, 0)
	withLen := block.NewInsertValue(withPtr, byteLen, 1)
	return block.NewInsertValue(withLen, block.NewCall(l.utf8CountFunc(), dst, byteLen), 2), block, nil
}

// byteBufferOf answers an `i8*` to a byte array's elements and their count.
//
// The two array flavours differ in exactly the way their layouts do. A `[]u8` is a box
// holding a pointer to its elements, so the buffer is a load and the length a field read.
// A `[N]u8` *is* its elements, held by value, so it has no address until one is taken —
// `argumentAddress`, the same helper `&xs[0]` goes through, which spills a temporary to a
// slot when the receiver is not storage (`makeBytes().decode_utf8()`).
func (l *lowerer) byteBufferOf(block *ir.Block, obj ast.Expression, recvT types.Type) (value.Value, value.Value, *ir.Block, error) {
	i8ptr := lltypes.NewPointer(lltypes.I8)
	switch it := recvT.(type) {
	case types.DynamicArrayType:
		elem, err := l.lowerType(it.ElementType)
		if err != nil {
			return nil, nil, nil, err
		}
		boxTy := DynArrayBoxType(elem)
		box, block, err := l.lowerExpr(block, obj)
		if err != nil {
			return nil, nil, nil, err
		}
		length := block.NewLoad(lltypes.I64, dynArrayLenPtr(block, boxTy, box))
		elems := block.NewLoad(lltypes.NewPointer(elem), dynArrayElemsPtr(block, boxTy, box))
		return block.NewBitCast(elems, i8ptr), length, block, nil
	case types.StaticArrayType:
		addr, block, err := l.argumentAddress(block, obj)
		if err != nil {
			return nil, nil, nil, err
		}
		return block.NewBitCast(addr, i8ptr), i64c(int64(it.Size)), block, nil
	}
	return nil, nil, nil, fmt.Errorf("llvm: decode_utf8() on non-array receiver %s", recvT)
}

// lowerEncodeUTF8 lowers `s.encode_utf8()` — a string's bytes as a `[]u8`.
//
// `decode_utf8` in reverse, and the same two allocations a `[]u8` always costs: a box
// (fixed size, holding length, capacity and a buffer pointer) plus the buffer itself,
// which is then memcpy'd from the string's payload. The stride is 1 and the element type
// i8, so the byte length is the element count and no scaling appears.
//
// It copies for the reason every construction here does: the two representations are
// different shapes — a string is a fat pointer into a ref-counted box, a `[]u8` is a box
// pointing at a separately-malloc'd buffer — so there is no aliasing arrangement to
// choose between. It is also what keeps the result mutable without the string becoming
// so, which a language whose strings are immutable had better not offer.
func (l *lowerer) lowerEncodeUTF8(block *ir.Block, call *ast.FunctionCallExpr, member *ast.MemberExpr) (value.Value, *ir.Block, error) {
	if len(call.Arguments) != 0 {
		return nil, nil, fmt.Errorf("llvm: encode_utf8() expects 0 arguments, got %d", len(call.Arguments))
	}
	str, block, err := l.lowerExpr(block, member.Object)
	if err != nil {
		return nil, nil, err
	}
	data := block.NewExtractValue(str, 0)
	byteLen := block.NewExtractValue(str, 1)

	boxTy := DynArrayBoxType(lltypes.I8)
	box := l.dynArrayAlloc(block, boxTy, lltypes.I8, byteLen, byteLen, 1)
	buf := block.NewLoad(lltypes.NewPointer(lltypes.I8), dynArrayElemsPtr(block, boxTy, box))
	// malloc(0) for an empty string is a pointer `free` accepts, and memcpy over a zero
	// length is a no-op, so the empty case needs nothing of its own.
	block.NewCall(l.memcpyFunc(), buf, data, byteLen)
	return box, block, nil
}
