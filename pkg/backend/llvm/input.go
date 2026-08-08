package llvm

import (
	"fmt"

	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/constant"
	"github.com/llir/llvm/ir/enum"
	lltypes "github.com/llir/llvm/ir/types"
	"github.com/llir/llvm/ir/value"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// Console input — the `read_line()` builtin, `() -> Maybe<string>`.
//
// This is the only way a Lyra program reads from the outside world, and the whole
// feature is here: the runtime shim that does the reading, and the lowering that
// wraps its result in the canonical `Maybe`.

// ShimReadLine reads one line from stdin and returns the canonical
// `Maybe<string>` union directly — `Some` of the line, or `None` at EOF.
const ShimReadLine = "lyra_read_line"

// readLineInitialCap is the payload capacity the shim starts with, doubling as
// needed. 128 bytes covers essentially every interactive line in one allocation
// without being a size worth worrying about for a program that reads none.
const readLineInitialCap = 128

// ensureReadLineRuntime emits `lyra_read_line` into the module the first time it
// is needed, caching the handle on the lowerer (idempotent), in the same
// emit-into-the-module style as the refcount shims — so `lyrac build`'s single
// `clang out.ll` stays self-contained with no runtime object to link.
//
// **It reads with `getchar`, deliberately, rather than `getline` or `fgets`.**
// Those take a `FILE*` and so need the `stdin` global, whose *symbol* differs by
// platform — `__stdinp` on macOS, `stdin` on glibc — which would put a host
// conditional into emitted IR that is otherwise target-independent. `getchar` is
// plain libc with no such global, so one lowering serves both, and the byte-at-a-
// time cost is irrelevant against the read syscall and the human typing.
//
// The line terminator is consumed but not returned, and a trailing `\r` is
// dropped with it, so a CRLF stream does not leave an invisible byte on the end of
// every line — which would make a parse of the result fail for no reason the user
// can see.
func (l *lowerer) ensureReadLineRuntime(dt types.DataType, someC types.DataTypeConstructor, someTag int, noneC types.DataTypeConstructor, noneTag int) (*ir.Func, error) {
	if l.readLine != nil {
		return l.readLine, nil
	}
	l.ensureRCRuntime() // for lyra_rc_alloc / free, and l.malloc's declaration

	i8ptr := lltypes.NewPointer(lltypes.I8)
	zero := constant.NewInt(lltypes.I64, 0)
	one := constant.NewInt(lltypes.I64, 1)

	getchar := l.module.NewFunc("getchar", lltypes.I32)
	realloc := l.module.NewFunc("realloc", i8ptr, ir.NewParam("", i8ptr), ir.NewParam("", lltypes.I64))

	unionTy, err := l.lowerType(dt)
	if err != nil {
		return nil, err
	}
	fn := l.module.NewFunc(ShimReadLine, unionTy)

	entry := fn.NewBlock("entry")
	loop := fn.NewBlock("loop")
	notEOF := fn.NewBlock("not_eof")
	notNewline := fn.NewBlock("not_newline")
	grow := fn.NewBlock("grow")
	appendCh := fn.NewBlock("append")
	atEOF := fn.NewBlock("at_eof")
	retNull := fn.NewBlock("ret_null")
	finish := fn.NewBlock("finish")
	stripCR := fn.NewBlock("strip_cr")
	done := fn.NewBlock("done")

	// The buffer is a ref-counted box from the start (header + capacity), so the
	// string handed back is an ordinary owned heap string that release/drop already
	// understand — no second copy out of a scratch buffer, and no special case in
	// the string machinery for "a string that came from input".
	capSlot := entry.NewAlloca(lltypes.I64)
	boxSlot := entry.NewAlloca(i8ptr)
	lenSlot := entry.NewAlloca(lltypes.I64)
	initCap := constant.NewInt(lltypes.I64, readLineInitialCap)
	entry.NewStore(initCap, capSlot)
	entry.NewStore(zero, lenSlot)
	entry.NewStore(entry.NewCall(l.rcAlloc,
		entry.NewAdd(initCap, constant.NewInt(lltypes.I64, rcHeaderSize))), boxSlot)
	entry.NewBr(loop)

	// c = getchar(); EOF (-1) and '\n' both end the line; everything else is a byte.
	ch := loop.NewCall(getchar)
	loop.NewCondBr(loop.NewICmp(enum.IPredEQ, ch, constant.NewInt(lltypes.I32, -1)), atEOF, notEOF)
	notEOF.NewCondBr(notEOF.NewICmp(enum.IPredEQ, ch, constant.NewInt(lltypes.I32, '\n')), finish, notNewline)

	// Grow when the payload is full: realloc the whole box (header included) and
	// keep going. realloc is valid on lyra_rc_alloc's memory — it is plain malloc —
	// and preserves the refcount header along with the bytes already read.
	curLen := notNewline.NewLoad(lltypes.I64, lenSlot)
	curCap := notNewline.NewLoad(lltypes.I64, capSlot)
	notNewline.NewCondBr(notNewline.NewICmp(enum.IPredULT, curLen, curCap), appendCh, grow)

	newCap := grow.NewMul(curCap, constant.NewInt(lltypes.I64, 2))
	grow.NewStore(newCap, capSlot)
	grow.NewStore(grow.NewCall(realloc, grow.NewLoad(i8ptr, boxSlot),
		grow.NewAdd(newCap, constant.NewInt(lltypes.I64, rcHeaderSize))), boxSlot)
	grow.NewBr(appendCh)

	payloadAt := func(b *ir.Block, off value.Value) value.Value {
		box := b.NewLoad(i8ptr, boxSlot)
		payload := b.NewGetElementPtr(lltypes.I8, box, constant.NewInt(lltypes.I64, rcHeaderSize))
		return b.NewGetElementPtr(lltypes.I8, payload, off)
	}
	appendLen := appendCh.NewLoad(lltypes.I64, lenSlot)
	appendCh.NewStore(appendCh.NewTrunc(ch, lltypes.I8), payloadAt(appendCh, appendLen))
	appendCh.NewStore(appendCh.NewAdd(appendLen, one), lenSlot)
	appendCh.NewBr(loop)

	// EOF: a line's worth of bytes already read is a final line without a
	// terminator, and is returned as such. EOF with nothing read is the end of the
	// stream — free the box and answer null, which the caller turns into `None`.
	eofLen := atEOF.NewLoad(lltypes.I64, lenSlot)
	atEOF.NewCondBr(atEOF.NewICmp(enum.IPredEQ, eofLen, zero), retNull, finish)
	retNull.NewCall(l.free, retNull.NewLoad(i8ptr, boxSlot))
	noneVal, err := l.buildDataValue(retNull, dt, noneTag, noneC, nil)
	if err != nil {
		return nil, err
	}
	retNull.NewRet(noneVal)

	// Drop a trailing '\r' so a CRLF line is not one invisible byte longer than it
	// looks. Checked on the byte before the end, and only when there is one.
	finLen := finish.NewLoad(lltypes.I64, lenSlot)
	hasBytes := finish.NewICmp(enum.IPredUGT, finLen, zero)
	finish.NewCondBr(hasBytes, stripCR, done)
	lastIdx := stripCR.NewSub(finLen, one)
	lastCh := stripCR.NewLoad(lltypes.I8, payloadAt(stripCR, lastIdx))
	isCR := stripCR.NewICmp(enum.IPredEQ, lastCh, constant.NewInt(lltypes.I8, '\r'))
	stripCR.NewStore(stripCR.NewSelect(isCR, lastIdx, finLen), lenSlot)
	stripCR.NewBr(done)

	doneLen := done.NewLoad(lltypes.I64, lenSlot)
	doneBox := done.NewLoad(i8ptr, boxSlot)
	payload := done.NewGetElementPtr(lltypes.I8, doneBox, constant.NewInt(lltypes.I64, rcHeaderSize))
	strTy := StringLLVMType()
	withPtr := done.NewInsertValue(constant.NewUndef(strTy), payload, 0)
	str := done.NewInsertValue(withPtr, doneLen, 1)
	someVal, err := l.buildDataValue(done, dt, someTag, someC, []value.Value{str})
	if err != nil {
		return nil, err
	}
	done.NewRet(someVal)

	l.readLine = fn
	return fn, nil
}

// lowerReadLineCall lowers `read_line()` to a single call returning the canonical
// `Maybe<string>`.
//
// **The call site emits no branches, and that is load-bearing rather than tidy.**
// The null test and the two union constructions all live inside the shim, so this
// returns the same block it was given. An earlier version branched here and
// returned a *merge* block, which was silently wrong in a way worth recording:
// flushStmtTemps released a temporary at the end of the statement only when the temp
// was produced in the statement's start block, and otherwise *in its own production
// block* — the latter because a temp produced inside a branch is undefined on the
// path not taken. A merge block is neither case, so the owned `Maybe<string>` was
// released in the merge block, i.e. before the `match` that consumed it ever ran its
// switch: a use-after-free that printed a line of blanks instead of the input.
//
// That flush asks **dominance** now (08/07, llvm.go), so a merge block would no
// longer break it — the same proxy went on to bite `slice`, whose continuation block
// is unconditional. The call stays a single instruction anyway, because an owned
// builtin result behaving exactly like an ordinary function call is the cheaper thing
// to reason about than one that is merely handled correctly.
//
// The `Maybe` is read back from the typechecker's recorded type rather than
// constructed here, so the backend's union agrees with the front end's type,
// including when the canonical type is not spelled "Maybe".
func (l *lowerer) lowerReadLineCall(block *ir.Block, e *ast.FunctionCallExpr) (value.Value, *ir.Block, error) {
	if len(e.Arguments) != 0 {
		return nil, nil, fmt.Errorf("llvm: read_line expects 0 arguments, got %d", len(e.Arguments))
	}
	recorded, ok := l.recordedType(e)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: read_line call has no recorded type")
	}
	dt, ok := recorded.(types.DataType)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: read_line must return a Maybe, got %s", recorded)
	}
	someC, someTag, hasSome := findConstructor(dt, "Some")
	noneC, noneTag, hasNone := findConstructor(dt, "None")
	if !hasSome || !hasNone {
		return nil, nil, fmt.Errorf("llvm: read_line's return type %q is not a canonical Maybe", dt.Name)
	}
	fn, err := l.ensureReadLineRuntime(dt, someC, someTag, noneC, noneTag)
	if err != nil {
		return nil, nil, err
	}
	return block.NewCall(fn), block, nil
}
