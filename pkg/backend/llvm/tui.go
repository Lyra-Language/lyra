package llvm

import (
	"fmt"
	"runtime"

	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/constant"
	"github.com/llir/llvm/ir/enum"
	lltypes "github.com/llir/llvm/ir/types"
	"github.com/llir/llvm/ir/value"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// The terminal — `set_raw_mode(on)`, `read_key()` and `terminal_size()`, the three
// builtins an interactive TUI needs and the only three it needs.
//
// Same division of labour as `read_line`/`parse_i64` and `random_seed`/`Rng`: each of
// these is a libc call this language cannot otherwise reach, and everything built on
// them — key decoding, colours, boxes, frame diffing, a status bar — is ordinary Lyra
// in `std.tui`. The **display** half needed nothing at all: `\e` reaches stdout as byte
// 27, so ANSI positioning and colour were already `print` calls.
//
// Why input needed the compiler and output did not: `read_line` is line-buffered, so a
// keypress is not observable until Enter, and the fix is `tcsetattr` — a syscall, not a
// string.
//
// # Platform
//
// This file is the **first place the backend is not platform-neutral**, and only in one
// spot: `TIOCGWINSZ` is 0x5413 on Linux and 0x40087468 on macOS (its ioctl numbers encode
// direction and payload size, Linux's do not). It is chosen from `runtime.GOOS` at
// emission time, which is honest rather than convenient — `lyrac` hands its IR to the
// host's clang with no target flag, so the compiling host *is* the target, and that holds
// inside the Debian container `asan.sh` runs too, where both halves are Linux.
//
// The other two dodge the question rather than answering it, and that is worth keeping:
// `struct termios` has a genuinely different layout on the two platforms (4- vs 8-byte
// flags, an extra `c_line`, NCCS 20 vs 32), which would put *field offsets* in this file
// — the kind of constant that stays plausible while being wrong. Going through
// `cfmakeraw` means the struct is never indexed, only carried, so an over-sized opaque
// buffer covers both. `struct winsize` is four `unsigned short` on both, so only its
// ioctl number was ever in question.

// termiosBufBytes over-sizes `struct termios` for either target — macOS needs 72 bytes
// and glibc 60. Nothing here reads a field, so the exact figure only has to be an upper
// bound, and a round one leaves room for a platform that grows the struct.
const termiosBufBytes = 128

// TCSANOW is 0 on both targets: apply the change immediately, without draining.
// Draining (TCSADRAIN) would be wrong here — entering raw mode should not block on
// output the program has already queued.
const tcsaNow = 0

// tiocgwinsz is the ioctl selector for the window size. See this file's Platform note
// for why this one constant is chosen from the compiling host.
func tiocgwinsz() int64 {
	if runtime.GOOS == "darwin" {
		return 0x40087468
	}
	return 0x5413
}

// Shim names for the three terminal builtins.
const (
	ShimSetRawMode   = "lyra_set_raw_mode"
	ShimReadKey      = "lyra_read_key"
	ShimTerminalSize = "lyra_terminal_size"
	ShimWaitForKey   = "lyra_wait_for_key_ms"
)

// ensureSetRawModeRuntime emits `lyra_set_raw_mode(i1)`, idempotent per module.
//
// Raw mode is not one flag but a set of them — echo off, canonical mode off, signal
// generation off, CR/LF translation off, a one-byte minimum read with no timeout — and
// `cfmakeraw` is the libc call that sets exactly that combination. Using it rather than
// masking the flags here is what keeps `struct termios`'s layout out of this file
// entirely (see the Platform note).
//
// **The original is saved on the first enable and restored on disable**, in module
// globals rather than handed back to the caller. A terminal left in raw mode after the
// program exits is unusable — no echo, no line editing, Ctrl-C dead — so restoring must
// not depend on the program having kept a token safe. `set_raw_mode(false)` therefore
// puts back what was actually there rather than a constructed "sane" mode, which is the
// same reason the clock zeroes its timespec: a defined answer beats a plausible one.
//
// Not registered with `atexit`: a panic path that skips it would leave the terminal
// wedged, and that is a real gap — but `atexit` handlers do not run on a signal either,
// so the honest fix is the prelude wrapping this in a function that restores on the way
// out, which is Lyra's to write and not the compiler's.
func (l *lowerer) ensureSetRawModeRuntime() *ir.Func {
	if l.setRawMode != nil {
		return l.setRawMode
	}
	i8ptr := lltypes.NewPointer(lltypes.I8)
	bufTy := lltypes.NewArray(termiosBufBytes, lltypes.I8)

	tcgetattr := l.module.NewFunc("tcgetattr", lltypes.I32,
		ir.NewParam("", lltypes.I32), ir.NewParam("", i8ptr))
	tcsetattr := l.module.NewFunc("tcsetattr", lltypes.I32,
		ir.NewParam("", lltypes.I32), ir.NewParam("", lltypes.I32), ir.NewParam("", i8ptr))
	cfmakeraw := l.module.NewFunc("cfmakeraw", lltypes.Void, ir.NewParam("", i8ptr))

	saved := l.module.NewGlobalDef("lyra_termios_saved", constant.NewZeroInitializer(bufTy))
	savedOK := l.module.NewGlobalDef("lyra_termios_saved_ok", constant.NewInt(lltypes.I32, 0))

	on := ir.NewParam("on", lltypes.I1)
	fn := l.module.NewFunc(ShimSetRawMode, lltypes.Void, on)

	entry := fn.NewBlock("entry")
	enable := fn.NewBlock("enable")
	firstSave := fn.NewBlock("first_save")
	applyRaw := fn.NewBlock("apply_raw")
	disable := fn.NewBlock("disable")
	restore := fn.NewBlock("restore")
	ret := fn.NewBlock("ret")

	// The scratch termios lives in the entry block: an alloca anywhere else is a fresh
	// allocation each time control reaches it, which is a stack leak in a loop even
	// where it happens to be correct.
	scratch := entry.NewAlloca(bufTy)
	scratchPtr := entry.NewBitCast(scratch, i8ptr)
	savedPtr := entry.NewBitCast(saved, i8ptr)
	stdin := constant.NewInt(lltypes.I32, 0)
	now := constant.NewInt(lltypes.I32, tcsaNow)
	entry.NewCondBr(on, enable, disable)

	// Save once. A second enable must not overwrite the saved copy with the raw mode
	// this function itself installed — that is how a restore comes to restore nothing.
	enable.NewCondBr(
		enable.NewICmp(enum.IPredEQ, enable.NewLoad(lltypes.I32, savedOK),
			constant.NewInt(lltypes.I32, 0)),
		firstSave, applyRaw)
	firstSave.NewCall(tcgetattr, stdin, savedPtr)
	firstSave.NewStore(constant.NewInt(lltypes.I32, 1), savedOK)
	firstSave.NewBr(applyRaw)

	// Read the *current* attributes rather than copying the saved ones, so no memcpy is
	// needed and the two paths cannot disagree about which struct is being modified.
	applyRaw.NewCall(tcgetattr, stdin, scratchPtr)
	applyRaw.NewCall(cfmakeraw, scratchPtr)
	applyRaw.NewCall(tcsetattr, stdin, now, scratchPtr)
	applyRaw.NewBr(ret)

	// Restoring without ever having enabled would write a zeroed termios over a working
	// terminal, which is far worse than doing nothing.
	disable.NewCondBr(
		disable.NewICmp(enum.IPredNE, disable.NewLoad(lltypes.I32, savedOK),
			constant.NewInt(lltypes.I32, 0)),
		restore, ret)
	restore.NewCall(tcsetattr, stdin, now, savedPtr)
	restore.NewBr(ret)

	ret.NewRet(nil)

	l.setRawMode = fn
	return fn
}

// lowerSetRawModeCall lowers `set_raw_mode(on)`.
//
// The argument is a `bool`, which is an i1 here, and the result is void — nothing owns
// anything, so there is no temp machinery to involve.
func (l *lowerer) lowerSetRawModeCall(block *ir.Block, e *ast.FunctionCallExpr) (value.Value, *ir.Block, error) {
	if len(e.Arguments) != 1 {
		return nil, nil, fmt.Errorf("llvm: set_raw_mode expects 1 argument, got %d", len(e.Arguments))
	}
	on, block, err := l.lowerExpr(block, e.Arguments[0])
	if err != nil {
		return nil, nil, err
	}
	block.NewCall(l.ensureSetRawModeRuntime(), on)
	return nil, block, nil
}

// ensureReadKeyRuntime emits `lyra_read_key()`, returning the canonical `Maybe<rune>`.
//
// One **code point**, not one byte, because that is what `rune` means everywhere else in
// this language — `s[i]` and `for c in s` both walk code points. So a multi-byte
// character typed at the terminal arrives as one key rather than as two or three
// mojibake keys, and the decode reuses `lyra_utf8_decode`, the same routine string
// iteration uses.
//
// **An escape sequence is deliberately *not* decoded here.** An arrow key sends ESC,
// `[`, `A` as three separate reads, and this returns them as three keys. Assembling them
// into "up arrow" needs a timeout to tell a real ESC press from the start of a sequence,
// a table of the sequences, and a policy for unrecognized ones — none of which is
// primitive, all of which is expressible in Lyra. That is the `parse_i64` line: the
// syscall is the builtin, the interpretation is the prelude's.
//
// `None` means the read returned nothing — EOF on stdin, or an interrupted read. In raw
// mode with VMIN=1 (which `cfmakeraw` sets) a read blocks until a byte is available, so
// `None` is not "no key yet"; it is "no more keys".
func (l *lowerer) ensureReadKeyRuntime(dt types.DataType, someC types.DataTypeConstructor, someTag int, noneC types.DataTypeConstructor, noneTag int) (*ir.Func, error) {
	if l.readKey != nil {
		return l.readKey, nil
	}
	i8ptr := lltypes.NewPointer(lltypes.I8)
	read := l.module.NewFunc("read", lltypes.I64,
		ir.NewParam("", lltypes.I32), ir.NewParam("", i8ptr), ir.NewParam("", lltypes.I64))

	fn := l.module.NewFunc(ShimReadKey, constant.NewUndef(lltypes.I8).Type())
	// The real return type is the Maybe union's; it is only known once the data type is
	// lowered, so the signature is fixed up after.
	unionTy, err := l.lowerType(dt)
	if err != nil {
		return nil, err
	}
	fn.Sig.RetType = unionTy

	entry := fn.NewBlock("entry")
	gotFirst := fn.NewBlock("got_first")
	contLoop := fn.NewBlock("cont_loop")
	contRead := fn.NewBlock("cont_read")
	decode := fn.NewBlock("decode")
	retNone := fn.NewBlock("ret_none")

	// A 4-byte buffer holds the longest UTF-8 sequence there is.
	buf := entry.NewAlloca(lltypes.NewArray(4, lltypes.I8))
	bufPtr := entry.NewBitCast(buf, i8ptr)
	cpSlot := entry.NewAlloca(lltypes.I32)
	idxSlot := entry.NewAlloca(lltypes.I64)
	zero64 := constant.NewInt(lltypes.I64, 0)
	one64 := constant.NewInt(lltypes.I64, 1)
	stdin := constant.NewInt(lltypes.I32, 0)

	n0 := entry.NewCall(read, stdin, bufPtr, one64)
	entry.NewCondBr(entry.NewICmp(enum.IPredSLE, n0, zero64), retNone, gotFirst)

	// How many continuation bytes the leading byte promises. Computed with selects
	// rather than branches — it is a pure function of one byte, and a chain of four
	// blocks would say otherwise. A byte matching none of the leading forms yields 0,
	// so an invalid sequence decodes as whatever lyra_utf8_decode makes of that single
	// byte instead of blocking on continuation bytes that will never arrive.
	b0 := gotFirst.NewLoad(lltypes.I8, bufPtr)
	b0w := gotFirst.NewZExt(b0, lltypes.I64)
	is2 := gotFirst.NewICmp(enum.IPredEQ,
		gotFirst.NewAnd(b0w, constant.NewInt(lltypes.I64, 0xE0)), constant.NewInt(lltypes.I64, 0xC0))
	is3 := gotFirst.NewICmp(enum.IPredEQ,
		gotFirst.NewAnd(b0w, constant.NewInt(lltypes.I64, 0xF0)), constant.NewInt(lltypes.I64, 0xE0))
	is4 := gotFirst.NewICmp(enum.IPredEQ,
		gotFirst.NewAnd(b0w, constant.NewInt(lltypes.I64, 0xF8)), constant.NewInt(lltypes.I64, 0xF0))
	extra := gotFirst.NewSelect(is2, one64,
		gotFirst.NewSelect(is3, constant.NewInt(lltypes.I64, 2),
			gotFirst.NewSelect(is4, constant.NewInt(lltypes.I64, 3), zero64)))
	gotFirst.NewStore(one64, idxSlot)
	gotFirst.NewBr(contLoop)

	// Read the continuation bytes one at a time. A short read mid-sequence is a
	// truncated character, which is `None` rather than a half-decoded rune.
	idx := contLoop.NewLoad(lltypes.I64, idxSlot)
	contLoop.NewCondBr(
		contLoop.NewICmp(enum.IPredULE, idx, extra), contRead, decode)
	slot := contRead.NewGetElementPtr(lltypes.I8, bufPtr, contRead.NewLoad(lltypes.I64, idxSlot))
	nk := contRead.NewCall(read, stdin, slot, one64)
	contRead.NewStore(contRead.NewAdd(contRead.NewLoad(lltypes.I64, idxSlot), one64), idxSlot)
	contRead.NewCondBr(contRead.NewICmp(enum.IPredSLE, nk, zero64), retNone, contLoop)

	decode.NewCall(l.utf8DecodeFunc(), bufPtr, zero64, cpSlot)
	someVal, err := l.buildDataValue(decode, dt, someTag, someC,
		[]value.Value{decode.NewLoad(lltypes.I32, cpSlot)})
	if err != nil {
		return nil, err
	}
	decode.NewRet(someVal)

	noneVal, err := l.buildDataValue(retNone, dt, noneTag, noneC, nil)
	if err != nil {
		return nil, err
	}
	retNone.NewRet(noneVal)

	l.readKey = fn
	return fn, nil
}

// lowerReadKeyCall lowers `read_key()` to a single call returning `Maybe<rune>`.
//
// The `Maybe` is read back from the typechecker's recorded type for the reason
// `read_line`'s is: the canonical type's *spelling* is free (the `@builtin(Maybe)` marker
// confers the identity), so constructing one named "Maybe" here would build whatever type
// the program happens to have declared under that name.
//
// A `rune` is a scalar, so unlike `read_line` the payload owns nothing and this result
// needs no release — `Maybe<rune>` is an inline union all the way down.
func (l *lowerer) lowerReadKeyCall(block *ir.Block, e *ast.FunctionCallExpr) (value.Value, *ir.Block, error) {
	if len(e.Arguments) != 0 {
		return nil, nil, fmt.Errorf("llvm: read_key expects 0 arguments, got %d", len(e.Arguments))
	}
	recorded, ok := l.recordedType(e)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: read_key call has no recorded type")
	}
	dt, ok := recorded.(types.DataType)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: read_key must return a Maybe, got %s", recorded)
	}
	someC, someTag, hasSome := findConstructor(dt, "Some")
	noneC, noneTag, hasNone := findConstructor(dt, "None")
	if !hasSome || !hasNone {
		return nil, nil, fmt.Errorf("llvm: read_key's return type %q is not a canonical Maybe", dt.Name)
	}
	fn, err := l.ensureReadKeyRuntime(dt, someC, someTag, noneC, noneTag)
	if err != nil {
		return nil, nil, err
	}
	return block.NewCall(fn), block, nil
}

// terminalSizeFallback is what a failed or absent `TIOCGWINSZ` answers: the VT100's
// dimensions, and the near-universal default for exactly this case.
//
// Answering rather than failing is clock.go's rule — a failed syscall must leave a
// *defined* value, not an unspecified one — and here it also keeps the common
// non-interactive case working: with output piped to a file there is no window at all,
// and a viewer that renders 80x24 into the pipe is more useful than one that renders
// nothing or divides by zero.
const (
	terminalSizeFallbackCols = 80
	terminalSizeFallbackRows = 24
)

// ensureTerminalSizeRuntime emits `lyra_terminal_size()`, returning `{i64, i64}`.
//
// **The pair is (columns, rows) — width first**, which is the reverse of the
// `struct winsize` it comes from (`ws_row` precedes `ws_col`). The tuple is this
// language's API rather than C's, "size" is width-then-height everywhere it is a pair,
// and a caller writing `let (width, height) = terminal_size()` is the shape this exists
// to serve. A swap here is silent and looks like a transposed render, so the order is
// stated in the builtin's diagnostic-facing docs too.
//
// The struct is queried on **stdout**, not stdin: output is what is being sized, and
// stdin may be a pipe while the display is still a terminal.
func (l *lowerer) ensureTerminalSizeRuntime(tt types.TupleType) (*ir.Func, error) {
	if l.terminalSize != nil {
		return l.terminalSize, nil
	}
	i8ptr := lltypes.NewPointer(lltypes.I8)
	ioctl := l.module.NewFunc("ioctl", lltypes.I32,
		ir.NewParam("", lltypes.I32), ir.NewParam("", lltypes.I64))
	ioctl.Sig.Variadic = true

	retTy, err := l.lowerType(tt)
	if err != nil {
		return nil, err
	}
	structTy, ok := retTy.(*lltypes.StructType)
	if !ok {
		return nil, fmt.Errorf("llvm: terminal_size return type %s did not lower to a struct", tt)
	}

	fn := l.module.NewFunc(ShimTerminalSize, structTy)
	b := fn.NewBlock("entry")

	// `struct winsize` is four `unsigned short` on both targets, so this needs no
	// platform knowledge — only the ioctl selector does.
	wsTy := lltypes.NewArray(4, lltypes.I16)
	ws := b.NewAlloca(wsTy)
	i32zero := constant.NewInt(lltypes.I32, 0)
	rowPtr := b.NewGetElementPtr(wsTy, ws, i32zero, constant.NewInt(lltypes.I32, 0))
	colPtr := b.NewGetElementPtr(wsTy, ws, i32zero, constant.NewInt(lltypes.I32, 1))
	// Zeroed before the call, so a failure is distinguishable and lands on the fallback
	// below instead of reading uninitialized stack — clock.go's rule.
	b.NewStore(constant.NewInt(lltypes.I16, 0), rowPtr)
	b.NewStore(constant.NewInt(lltypes.I16, 0), colPtr)

	b.NewCall(ioctl, constant.NewInt(lltypes.I32, 1),
		constant.NewInt(lltypes.I64, tiocgwinsz()), b.NewBitCast(ws, i8ptr))

	// The ioctl's own return value is ignored in favour of testing the values: a
	// terminal reporting a zero dimension is as unusable as a failed call, and this way
	// one test covers both.
	var rows value.Value = b.NewZExt(b.NewLoad(lltypes.I16, rowPtr), lltypes.I64)
	var cols value.Value = b.NewZExt(b.NewLoad(lltypes.I16, colPtr), lltypes.I64)
	rows = b.NewSelect(b.NewICmp(enum.IPredEQ, rows, constant.NewInt(lltypes.I64, 0)),
		constant.NewInt(lltypes.I64, terminalSizeFallbackRows), rows)
	cols = b.NewSelect(b.NewICmp(enum.IPredEQ, cols, constant.NewInt(lltypes.I64, 0)),
		constant.NewInt(lltypes.I64, terminalSizeFallbackCols), cols)

	agg := b.NewInsertValue(constant.NewUndef(structTy), cols, 0)
	b.NewRet(b.NewInsertValue(agg, rows, 1))

	l.terminalSize = fn
	return fn, nil
}

// lowerTerminalSizeCall lowers `terminal_size()` to a single call returning
// `(i64, i64)` — columns then rows. Both are scalars, so nothing here owns anything.
func (l *lowerer) lowerTerminalSizeCall(block *ir.Block, e *ast.FunctionCallExpr) (value.Value, *ir.Block, error) {
	if len(e.Arguments) != 0 {
		return nil, nil, fmt.Errorf("llvm: terminal_size expects 0 arguments, got %d", len(e.Arguments))
	}
	recorded, ok := l.recordedType(e)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: terminal_size call has no recorded type")
	}
	tt, ok := recorded.(types.TupleType)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: terminal_size must return a tuple, got %s", recorded)
	}
	fn, err := l.ensureTerminalSizeRuntime(tt)
	if err != nil {
		return nil, nil, err
	}
	return block.NewCall(fn), block, nil
}

// poll's flag bits and struct layout, all of which are **identical on both targets** —
// `struct pollfd` is `{int, short, short}` and POLLIN/POLLERR/POLLHUP are 0x1/0x8/0x10 on
// macOS and glibc alike. That is the opposite of TIOCGWINSZ above, and the reason this
// builtin needs no `runtime.GOOS` at all.
const (
	pollIn  = 0x0001
	pollErr = 0x0008
	pollHup = 0x0010
	// A poll timeout is an `int`, so an i64 argument is clamped to this before truncating.
	pollTimeoutMax = 0x7fffffff
)

// ensureWaitForKeyRuntime emits `lyra_wait_for_key_ms(i64) -> i1`, idempotent per module.
//
// # Why a bool rather than a timed read
//
// There are **three** outcomes to report — a key arrived, nothing arrived yet, input has
// ended — and the obvious `read_key_timeout(ms) -> Maybe<rune>` has only two answers for
// them, so it has to conflate two. Conflating "nothing yet" with "ended" is exactly the
// mistake `read_line`'s `Maybe` was introduced to avoid: it makes the natural loop spin
// forever once stdin closes.
//
// Splitting the question resolves it with no new type. This answers only "is there
// something to read", which a bool says exactly; `read_key` then answers "a key, or the
// end". The pairing is not a workaround but a property of poll: **a closed descriptor
// reports readable**, so at EOF this returns true and the read that follows returns
// `None`. Verified rather than assumed — a poll of a pipe whose write end is closed
// answers POLLIN|POLLHUP, and the read then returns 0.
//
// # The timeout
//
// Clamped into [0, INT_MAX] rather than rejected. A negative value is what deadline
// arithmetic naturally produces once the deadline has passed (`deadline - now()`), and
// "do not wait at all" is the right answer there — so this is not the silent
// reinterpretation the language dislikes, it is the meaning. Zero is a pure non-blocking
// poll, which is the useful degenerate case.
//
// A poll error (-1, typically EINTR) answers false rather than trapping: a signal
// arriving during the wait is not the program's fault, and the caller's loop will poll
// again.
func (l *lowerer) ensureWaitForKeyRuntime() *ir.Func {
	if l.waitForKey != nil {
		return l.waitForKey
	}
	i8ptr := lltypes.NewPointer(lltypes.I8)
	pollfdTy := lltypes.NewStruct(lltypes.I32, lltypes.I16, lltypes.I16)
	poll := l.module.NewFunc("poll", lltypes.I32,
		ir.NewParam("", i8ptr), ir.NewParam("", lltypes.I64), ir.NewParam("", lltypes.I32))

	timeout := ir.NewParam("timeout_ms", lltypes.I64)
	fn := l.module.NewFunc(ShimWaitForKey, lltypes.I1, timeout)
	b := fn.NewBlock("entry")

	pfd := b.NewAlloca(pollfdTy)
	i32zero := constant.NewInt(lltypes.I32, 0)
	fdPtr := b.NewGetElementPtr(pollfdTy, pfd, i32zero, constant.NewInt(lltypes.I32, 0))
	evPtr := b.NewGetElementPtr(pollfdTy, pfd, i32zero, constant.NewInt(lltypes.I32, 1))
	rePtr := b.NewGetElementPtr(pollfdTy, pfd, i32zero, constant.NewInt(lltypes.I32, 2))
	b.NewStore(constant.NewInt(lltypes.I32, 0), fdPtr) // stdin, the fd read_key reads
	b.NewStore(constant.NewInt(lltypes.I16, pollIn), evPtr)
	// revents is written by poll, but zero it anyway: on the error path below it is read
	// without poll having set it, and clock.go's rule is that a failed syscall must leave
	// a defined value rather than uninitialized stack.
	b.NewStore(constant.NewInt(lltypes.I16, 0), rePtr)

	belowZero := b.NewICmp(enum.IPredSLT, timeout, constant.NewInt(lltypes.I64, 0))
	atLeastZero := b.NewSelect(belowZero, constant.NewInt(lltypes.I64, 0), timeout)
	aboveMax := b.NewICmp(enum.IPredSGT, atLeastZero, constant.NewInt(lltypes.I64, pollTimeoutMax))
	clamped := b.NewSelect(aboveMax, constant.NewInt(lltypes.I64, pollTimeoutMax), atLeastZero)

	n := b.NewCall(poll, b.NewBitCast(pfd, i8ptr), constant.NewInt(lltypes.I64, 1),
		b.NewTrunc(clamped, lltypes.I32))

	// Branchless: 0 (timed out) and -1 (error) both answer false; otherwise the answer is
	// whether any of the three interesting bits came back. POLLERR and POLLHUP count as
	// readable because the read that follows is what turns them into `None`.
	failed := b.NewICmp(enum.IPredSLE, n, i32zero)
	revents := b.NewLoad(lltypes.I16, rePtr)
	interesting := b.NewAnd(revents, constant.NewInt(lltypes.I16, pollIn|pollErr|pollHup))
	ready := b.NewICmp(enum.IPredNE, interesting, constant.NewInt(lltypes.I16, 0))
	b.NewRet(b.NewSelect(failed, constant.NewInt(lltypes.I1, 0), ready))

	l.waitForKey = fn
	return fn
}

// lowerWaitForKeyCall lowers `wait_for_key_ms(timeout)`.
//
// The argument is an i64 and the result a bool — both scalars, so nothing here owns
// anything and there is no temp machinery to involve.
func (l *lowerer) lowerWaitForKeyCall(block *ir.Block, e *ast.FunctionCallExpr) (value.Value, *ir.Block, error) {
	if len(e.Arguments) != 1 {
		return nil, nil, fmt.Errorf("llvm: wait_for_key_ms expects 1 argument, got %d", len(e.Arguments))
	}
	ms, block, err := l.lowerExpr(block, e.Arguments[0])
	if err != nil {
		return nil, nil, err
	}
	return block.NewCall(l.ensureWaitForKeyRuntime(), ms), block, nil
}
