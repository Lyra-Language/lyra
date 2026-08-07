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
// One walk, not two: it records the byte offsets of `start` and `end` as it passes
// them, so the cost is O(end) rather than O(start) + O(end).
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
	entry := fn.Blocks[0]
	biSlot := entry.NewAlloca(lltypes.I64)       // byte cursor
	riSlot := entry.NewAlloca(lltypes.I64)       // rune cursor
	startOffSlot := entry.NewAlloca(lltypes.I64) // byte offset of rune `start`
	endOffSlot := entry.NewAlloca(lltypes.I64)   // byte offset of rune `end`
	seenStart := entry.NewAlloca(lltypes.I1)
	seenEnd := entry.NewAlloca(lltypes.I1)
	cpSlot := entry.NewAlloca(lltypes.I32) // decode out-param, discarded

	zero := constant.NewInt(lltypes.I64, 0)
	block.NewStore(zero, biSlot)
	block.NewStore(zero, riSlot)
	block.NewStore(zero, startOffSlot)
	block.NewStore(zero, endOffSlot)
	block.NewStore(constant.NewInt(lltypes.I1, 0), seenStart)
	block.NewStore(constant.NewInt(lltypes.I1, 0), seenEnd)

	trapBlock := fn.NewBlock("")
	trapBlock.NewCall(l.panicStringSliceOOBFunc())
	trapBlock.NewUnreachable()

	// `start > end` is a caller bug, not an empty slice. Checked before the walk, so
	// an inverted range traps even when both bounds are individually in range.
	//
	// Signed compare: the arguments are i64 and a negative bound must reach the trap
	// rather than wrap to a huge unsigned value and merely fail to be found.
	orderOK := fn.NewBlock("")
	block.NewCondBr(block.NewICmp(enum.IPredSLE, start, end), orderOK, trapBlock)
	negOK := fn.NewBlock("")
	orderOK.NewCondBr(orderOK.NewICmp(enum.IPredSGE, start, zero), negOK, trapBlock)

	condBlock := fn.NewBlock("")
	bodyBlock := fn.NewBlock("")
	doneBlock := fn.NewBlock("")
	negOK.NewBr(condBlock)

	// The walk stops as soon as the end offset is known, so slicing the head of a
	// long string does not traverse its tail.
	bi := condBlock.NewLoad(lltypes.I64, biSlot)
	more := condBlock.NewICmp(enum.IPredULT, bi, byteLen)
	notDone := condBlock.NewXor(condBlock.NewLoad(lltypes.I1, seenEnd), constant.NewInt(lltypes.I1, 1))
	condBlock.NewCondBr(condBlock.NewAnd(more, notDone), bodyBlock, doneBlock)

	// Record the byte offset the moment the rune cursor reaches each bound. Both are
	// tested before decoding, so `end == rune count` records the offset one past the
	// last rune (the byte length) on the iteration that fails the `more` test — which
	// is why the same check is repeated in doneBlock below.
	riB := bodyBlock.NewLoad(lltypes.I64, riSlot)
	biB := bodyBlock.NewLoad(lltypes.I64, biSlot)
	atStart := bodyBlock.NewICmp(enum.IPredEQ, riB, start)
	bodyBlock.NewStore(bodyBlock.NewSelect(atStart, biB, bodyBlock.NewLoad(lltypes.I64, startOffSlot)), startOffSlot)
	bodyBlock.NewStore(bodyBlock.NewOr(bodyBlock.NewLoad(lltypes.I1, seenStart), atStart), seenStart)
	atEnd := bodyBlock.NewICmp(enum.IPredEQ, riB, end)
	bodyBlock.NewStore(bodyBlock.NewSelect(atEnd, biB, bodyBlock.NewLoad(lltypes.I64, endOffSlot)), endOffSlot)
	bodyBlock.NewStore(bodyBlock.NewOr(bodyBlock.NewLoad(lltypes.I1, seenEnd), atEnd), seenEnd)

	nBytes := bodyBlock.NewCall(l.utf8DecodeFunc(), data, biB, cpSlot)
	bodyBlock.NewStore(bodyBlock.NewAdd(biB, nBytes), biSlot)
	bodyBlock.NewStore(bodyBlock.NewAdd(riB, constant.NewInt(lltypes.I64, 1)), riSlot)
	bodyBlock.NewBr(condBlock)

	// A bound equal to the rune count lands here rather than in the loop: the cursor
	// reaches it exactly as the bytes run out. Both bounds get the same treatment, so
	// `s.slice(n, n)` on a string of n runes is the empty string rather than a trap.
	riEnd := doneBlock.NewLoad(lltypes.I64, riSlot)
	biEnd := doneBlock.NewLoad(lltypes.I64, biSlot)
	atStartEnd := doneBlock.NewICmp(enum.IPredEQ, riEnd, start)
	doneBlock.NewStore(doneBlock.NewSelect(atStartEnd, biEnd, doneBlock.NewLoad(lltypes.I64, startOffSlot)), startOffSlot)
	sawStart := doneBlock.NewOr(doneBlock.NewLoad(lltypes.I1, seenStart), atStartEnd)
	atEndEnd := doneBlock.NewICmp(enum.IPredEQ, riEnd, end)
	doneBlock.NewStore(doneBlock.NewSelect(atEndEnd, biEnd, doneBlock.NewLoad(lltypes.I64, endOffSlot)), endOffSlot)
	sawEnd := doneBlock.NewOr(doneBlock.NewLoad(lltypes.I1, seenEnd), atEndEnd)

	// A bound the walk never reached is past the end of the string.
	buildBlock := fn.NewBlock("")
	doneBlock.NewCondBr(doneBlock.NewAnd(sawStart, sawEnd), buildBlock, trapBlock)

	startOff := buildBlock.NewLoad(lltypes.I64, startOffSlot)
	endOff := buildBlock.NewLoad(lltypes.I64, endOffSlot)
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
