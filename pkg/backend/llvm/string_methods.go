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

// The string methods `s.len()` and `s.slice(start, end)` (08/06).
//
// **Both are rune-indexed, and that was decided before either existed.** `s[i]`
// already yields the i-th code point (lowerStringIndex) and `for c in s` already
// walks code points, so a byte-based `len` would make `for i in 0..<s.len() { s[i] }`
// wrong the first time a non-ASCII byte arrives — silently, and only for some
// inputs, which is the worst shape a bug can have. The fat pointer's `len` field
// remains a *byte* count (STRING_LAYOUT.md): that is the representation, and this is
// the language.
//
// The cost is that `len()` is **O(n)** where the array one is O(1). It is the honest
// price of agreeing with the index, and `for c in s` is still the way to traverse a
// string without paying a walk per element — which is what the prelude's `parse_i64`
// does and what the trim family does.
//
// Both walk with `lyra_utf8_decode` (strings.go), the same decoder the index and the
// for-in loop use. Byte-length bounds every loop here by construction — no encoded
// rune is shorter than one byte, so a rune counter can never outrun the bytes — which
// is the same rule that bounds an array comprehension over a string (array_comp.go).

// lowerStringLen lowers `s.len()` → the number of runes in s, as an i64.
//
// A plain decode-and-count walk. It cannot trap: an empty string yields 0, and the
// loop is bounded by the byte length.
func (l *lowerer) lowerStringLen(block *ir.Block, call *ast.FunctionCallExpr, member *ast.MemberExpr) (value.Value, *ir.Block, error) {
	if len(call.Arguments) != 0 {
		return nil, nil, fmt.Errorf("llvm: len() expects 0 arguments, got %d", len(call.Arguments))
	}
	str, block, err := l.lowerExpr(block, member.Object)
	if err != nil {
		return nil, nil, err
	}
	if !isStringLLVMType(str.Type()) {
		return nil, nil, fmt.Errorf("llvm: string len() receiver did not lower to a string (%s)", str.Type())
	}
	data := block.NewExtractValue(str, 0)
	byteLen := block.NewExtractValue(str, 1)
	decode := l.utf8DecodeFunc()

	fn := block.Parent
	entry := fn.Blocks[0]
	biSlot := entry.NewAlloca(lltypes.I64) // byte cursor
	nSlot := entry.NewAlloca(lltypes.I64)  // runes seen
	cpSlot := entry.NewAlloca(lltypes.I32) // decode out-param, discarded
	block.NewStore(constant.NewInt(lltypes.I64, 0), biSlot)
	block.NewStore(constant.NewInt(lltypes.I64, 0), nSlot)

	condBlock := fn.NewBlock("")
	bodyBlock := fn.NewBlock("")
	doneBlock := fn.NewBlock("")
	block.NewBr(condBlock)

	bi := condBlock.NewLoad(lltypes.I64, biSlot)
	condBlock.NewCondBr(condBlock.NewICmp(enum.IPredULT, bi, byteLen), bodyBlock, doneBlock)

	biB := bodyBlock.NewLoad(lltypes.I64, biSlot)
	n := bodyBlock.NewCall(decode, data, biB, cpSlot)
	bodyBlock.NewStore(bodyBlock.NewAdd(biB, n), biSlot)
	count := bodyBlock.NewLoad(lltypes.I64, nSlot)
	bodyBlock.NewStore(bodyBlock.NewAdd(count, constant.NewInt(lltypes.I64, 1)), nSlot)
	bodyBlock.NewBr(condBlock)

	return doneBlock.NewLoad(lltypes.I64, nSlot), doneBlock, nil
}

// lowerStringSlice lowers `s.slice(start, end)` → the half-open rune range
// `[start, end)` as a fresh string.
//
// **It allocates, and a borrowed slice is deliberately not on offer.** A substring of
// UTF-8 is a contiguous byte range, so `{data + off, n}` would be a correct fat
// pointer costing nothing — that is how Go slices a string. It does not work here
// because every Lyra string is a ref-counted box whose header sits at the box's
// *start* (ALLOCATION.md): a pointer into the middle cannot find the header to
// retain or release through, so a borrowed slice would either leak its parent or free
// bytes it does not own. Copying into a fresh box keeps the one uniform rule that a
// string value is a box, at the cost of an allocation `noalloc` correctly refuses.
//
// **Bounds are checked, in rune terms, and a violation traps** — its own
// `lyra_panic_string_slice_out_of_range`, not the array one, which is what both this
// and the string index used to reach and which sends the reader looking for an array.
// `start > end` traps too rather than yielding "" quietly:
// an inverted range is a caller bug, and the empty string it would otherwise produce
// is indistinguishable from a correct empty slice. `start == end` is fine and yields
// "" — the empty range is the one every loop terminates on.
//
// **Either bound may be negative** (08/08), counting from the end as an array index
// does — `s.slice(1, -1)` drops the last rune. A negative bound is resolved by the
// backward byte walk in strRuneOffsetFunc, so it costs O(|bound|) with no decoding,
// and the ordering test is applied to the resolved *offsets* rather than to the written
// bounds — otherwise `1 > -1` would reject a perfectly ordinary interval. Rune index to
// byte offset is monotonic, so the two comparisons ask the same question.
//
// It resolves each bound with its own call, where it used to record both offsets in one
// pass and so cost O(end) rather than O(start) + O(end). That single walk is what a
// negative bound cannot be expressed in: it advances a rune counter forward, and there
// is no value of that counter which means "third from the end". Sharing one definition
// of where a rune begins is also what keeps this and `s[i]` from drifting (rule 8) —
// they carried the same forward walk twice. The doubled decode is bounded by a function
// that already allocates and copies, which is why it is the right side of that trade.
func (l *lowerer) lowerStringSlice(block *ir.Block, call *ast.FunctionCallExpr, member *ast.MemberExpr) (value.Value, *ir.Block, error) {
	if len(call.Arguments) != 2 {
		return nil, nil, fmt.Errorf("llvm: slice() expects 2 arguments, got %d", len(call.Arguments))
	}
	str, block, err := l.lowerExpr(block, member.Object)
	if err != nil {
		return nil, nil, err
	}
	if !isStringLLVMType(str.Type()) {
		return nil, nil, fmt.Errorf("llvm: string slice() receiver did not lower to a string (%s)", str.Type())
	}
	data := block.NewExtractValue(str, 0)
	byteLen := block.NewExtractValue(str, 1)

	startV, block, err := l.lowerExpr(block, call.Arguments[0])
	if err != nil {
		return nil, nil, err
	}
	startSigned, _ := l.getIntSignedness(call.Arguments[0])
	start := coerceIntWidth(block, startV, startSigned, lltypes.I64)

	endV, block, err := l.lowerExpr(block, call.Arguments[1])
	if err != nil {
		return nil, nil, err
	}
	endSigned, _ := l.getIntSignedness(call.Arguments[1])
	end := coerceIntWidth(block, endV, endSigned, lltypes.I64)

	fn := block.Parent
	zero := constant.NewInt(lltypes.I64, 0)
	yes := constant.NewInt(lltypes.I1, 1)

	trapBlock := fn.NewBlock("")
	trapBlock.NewCall(l.panicStringSliceOOBFunc())
	trapBlock.NewUnreachable()

	// Each bound is resolved to a byte offset independently, allowEnd set because the
	// exclusive end may name the position one past the last rune (`s.slice(n, n)` is
	// the empty string, not a trap). -1 is "no such rune".
	offsets := l.strRuneOffsetFunc()
	startOff := block.NewCall(offsets, data, byteLen, start, yes)
	endOff := block.NewCall(offsets, data, byteLen, end, yes)

	// One test for all three ways this can be wrong: either bound out of range, or an
	// inverted range. **Ordering is compared on the resolved offsets, not on the
	// written bounds**, which is what admits a mixed range like `s.slice(1, -1)` —
	// numerically 1 > -1, and in the string a perfectly ordinary interval. The mapping
	// from rune index to byte offset is monotonic, so comparing offsets asks exactly
	// what comparing rune indices would.
	//
	// `start > end` stays a trap rather than an empty string: an inverted range is a
	// caller bug, and the "" it would otherwise yield is indistinguishable from a
	// correct empty slice. `start == end` is fine and yields "".
	badStart := block.NewICmp(enum.IPredSLT, startOff, zero)
	badEnd := block.NewICmp(enum.IPredSLT, endOff, zero)
	inverted := block.NewICmp(enum.IPredSGT, startOff, endOff)
	bad := block.NewOr(block.NewOr(badStart, badEnd), inverted)

	buildBlock := fn.NewBlock("")
	block.NewCondBr(bad, trapBlock, buildBlock)

	nOut := buildBlock.NewSub(endOff, startOff)

	// A fresh box, exactly as `++` builds one (lowerStringConcat), so the result is an
	// ordinary owned heap string the ownership pass already knows how to release. A
	// memcpy of length 0 is a valid no-op, so the empty slice needs no special case.
	_, dst := l.rcAllocPayload(buildBlock, nOut)
	buildBlock.NewCall(l.memcpyFunc(), dst, buildBlock.NewGetElementPtr(lltypes.I8, data, startOff), nOut)

	strTy := StringLLVMType()
	withPtr := buildBlock.NewInsertValue(constant.NewUndef(strTy), dst, 0)
	return buildBlock.NewInsertValue(withPtr, nOut, 1), buildBlock, nil
}

// lowerStringCompareBytes lowers `s.compare_bytes(other)` → a negative, zero or positive
// i64, memcmp's convention.
//
// **Byte order is code-point order in UTF-8**, by design of the encoding: a lower code
// point always encodes to a byte sequence that compares lower. So one memcmp answers
// exactly what a rune-by-rune walk would, in O(n) rather than the O(n²) an
// index-by-index loop in the prelude would cost — `s[i]` is O(i).
//
// memcmp over min(la, lb) can read no further than the shorter buffer, so it is safe
// whatever the lengths; when the common prefix is equal, the *lengths* decide, which is
// the standard "prefix sorts first" rule ("ab" < "abc").
func (l *lowerer) lowerStringCompareBytes(block *ir.Block, call *ast.FunctionCallExpr, member *ast.MemberExpr) (value.Value, *ir.Block, error) {
	if len(call.Arguments) != 1 {
		return nil, nil, fmt.Errorf("llvm: compare_bytes expects 1 argument, got %d", len(call.Arguments))
	}
	a, block, err := l.lowerExpr(block, member.Object)
	if err != nil {
		return nil, nil, err
	}
	b, block, err := l.lowerExpr(block, call.Arguments[0])
	if err != nil {
		return nil, nil, err
	}
	if !isStringLLVMType(a.Type()) || !isStringLLVMType(b.Type()) {
		return nil, nil, fmt.Errorf("llvm: compare_bytes needs two strings (%s, %s)", a.Type(), b.Type())
	}
	pa := block.NewExtractValue(a, 0)
	la := block.NewExtractValue(a, 1)
	pb := block.NewExtractValue(b, 0)
	lb := block.NewExtractValue(b, 1)

	aShorter := block.NewICmp(enum.IPredULT, la, lb)
	n := block.NewSelect(aShorter, la, lb) // min(la, lb) — reads past neither buffer
	cmp := block.NewSExt(block.NewCall(l.memcmpFunc(), pa, pb, n), lltypes.I64)

	// A non-zero memcmp decides it. Otherwise the common prefix matched and the shorter
	// string sorts first, so the difference of lengths carries the sign — computed as a
	// subtraction rather than a branch, keeping the call site branchless for the reason
	// the rest of this file is.
	lenDiff := block.NewSub(la, lb)
	prefixEqual := block.NewICmp(enum.IPredEQ, cmp, constant.NewInt(lltypes.I64, 0))
	return block.NewSelect(prefixEqual, lenDiff, cmp), block, nil
}

// lowerStringByteLen lowers `s.byte_len()` → the fat pointer's own length field, as
// an i64. O(1), where `len()` is an O(n) rune walk.
//
// The one string method that reads the representation rather than the language. It
// exists so compare_bytes_at has an offset to be given; see builtins.go for why that
// crack is worth opening and why it is this narrow.
func (l *lowerer) lowerStringByteLen(block *ir.Block, call *ast.FunctionCallExpr, member *ast.MemberExpr) (value.Value, *ir.Block, error) {
	if len(call.Arguments) != 0 {
		return nil, nil, fmt.Errorf("llvm: byte_len() expects 0 arguments, got %d", len(call.Arguments))
	}
	str, block, err := l.lowerExpr(block, member.Object)
	if err != nil {
		return nil, nil, err
	}
	if !isStringLLVMType(str.Type()) {
		return nil, nil, fmt.Errorf("llvm: string byte_len() receiver did not lower to a string (%s)", str.Type())
	}
	return block.NewExtractValue(str, 1), block, nil
}

// lowerStringCompareBytesAt lowers `s.compare_bytes_at(offset, other)` → a negative,
// zero or positive i64: memcmp of **exactly `other`'s bytes** against the bytes of s
// starting at byte `offset`.
//
// Comparing `other`'s length rather than the rest of `s` is what makes it a *prefix*
// test rather than an equality test — `"hello".compare_bytes_at(0, "he")` is 0, where
// the sibling `compare_bytes` would answer positive because `s` is longer. That is the
// only semantic difference between the two, and it is the reason this exists.
//
// **Branchless, and total.** Every out-of-range case is folded into selects rather
// than a trap or a branch: the offset is clamped before it reaches the GEP, the memcmp
// length is clamped to what s actually has, and a shortfall (or an offset that was out
// of range at all) forces a negative result — short sorts first, the rule compare_bytes
// already follows. Branchless is not a micro-optimization here: a call site that ends
// in a merge block is not a case flushStmtTemps handles, which is the bug that made two
// `slice` results in one interpolation clobber each other (08/07), and it is why
// read_line and `<=>` are branchless too. A trap would also be the wrong answer for a
// predicate the prelude guards anyway.
func (l *lowerer) lowerStringCompareBytesAt(block *ir.Block, call *ast.FunctionCallExpr, member *ast.MemberExpr) (value.Value, *ir.Block, error) {
	if len(call.Arguments) != 2 {
		return nil, nil, fmt.Errorf("llvm: compare_bytes_at expects 2 arguments, got %d", len(call.Arguments))
	}
	self, block, err := l.lowerExpr(block, member.Object)
	if err != nil {
		return nil, nil, err
	}
	offV, block, err := l.lowerExpr(block, call.Arguments[0])
	if err != nil {
		return nil, nil, err
	}
	offSigned, _ := l.getIntSignedness(call.Arguments[0])
	offset := coerceIntWidth(block, offV, offSigned, lltypes.I64)

	other, block, err := l.lowerExpr(block, call.Arguments[1])
	if err != nil {
		return nil, nil, err
	}
	if !isStringLLVMType(self.Type()) || !isStringLLVMType(other.Type()) {
		return nil, nil, fmt.Errorf("llvm: compare_bytes_at needs two strings (%s, %s)", self.Type(), other.Type())
	}

	data := block.NewExtractValue(self, 0)
	byteLen := block.NewExtractValue(self, 1)
	otherData := block.NewExtractValue(other, 0)
	otherLen := block.NewExtractValue(other, 1)

	zero := constant.NewInt(lltypes.I64, 0)
	// 0 <= offset <= byteLen. An offset *equal* to the length is in range and names the
	// empty tail, which is what `"".ends_with("")` and `s.ends_with(s)` need.
	inRange := block.NewAnd(
		block.NewICmp(enum.IPredSGE, offset, zero),
		block.NewICmp(enum.IPredSLE, offset, byteLen),
	)
	// Clamped *before* the GEP, so an out-of-range offset never forms a wild pointer —
	// the result is corrected below rather than by not computing it.
	base := block.NewSelect(inRange, offset, zero)
	avail := block.NewSub(byteLen, base) // >= 0 by the clamp

	shorter := block.NewICmp(enum.IPredULT, avail, otherLen)
	n := block.NewSelect(shorter, avail, otherLen) // read past neither buffer
	p := block.NewGetElementPtr(lltypes.I8, data, base)
	cmp := block.NewSExt(block.NewCall(l.memcmpFunc(), p, otherData, n), lltypes.I64)

	// The bytes that were there matched. `n - otherLen` is 0 when all of `other` was
	// compared and negative when `self` ran short; an offset that was out of range is
	// negative for the same reason — there was no such range to match.
	tail := block.NewSelect(inRange, block.NewSub(n, otherLen), constant.NewInt(lltypes.I64, -1))
	prefixEqual := block.NewICmp(enum.IPredEQ, cmp, zero)
	return block.NewSelect(prefixEqual, tail, cmp), block, nil
}
