package llvm

import (
	"fmt"

	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/constant"
	"github.com/llir/llvm/ir/enum"
	lltypes "github.com/llir/llvm/ir/types"
	"github.com/llir/llvm/ir/value"
)

// Checked arithmetic — the trap side (Pit-of-Success #2, "checked arithmetic by
// default; wraparound explicit"). Plain `+`/`-`/`*` on integers lower to LLVM's
// overflow-checking intrinsics; on overflow, control branches to a trap that
// reports and aborts. The explicit escape hatches are `wrapping_*`/`saturating_*`
// (the builtin-method registry), which lower to the raw ops instead.
//
// This file provides the runtime trap; arithmetic.go emits the check around each
// op. The trap is a single per-module noreturn function (not inlined at each
// site), so N overflow sites cost one `call`+`unreachable` each rather than N
// copies of the message-write + exit.

// overflowTrapExitCode is the process exit status on an arithmetic-overflow trap.
// 101 follows Rust's panic convention — a distinctive, non-zero, deterministic
// code (nothing else in a Lyra program exits with it), so a test can assert the
// trap fired rather than merely that the process failed.
const overflowTrapExitCode = 101

// overflowTrapMessage is written to stderr (fd 2) before the process exits.
const overflowTrapMessage = "lyra: arithmetic overflow\n"

// exitFunc lazily declares libc's `void @exit(i32)` (noreturn).
func (l *lowerer) exitFunc() *ir.Func {
	if l.exit == nil {
		l.exit = l.module.NewFunc("exit", lltypes.Void, ir.NewParam("", lltypes.I32))
		l.exit.FuncAttrs = append(l.exit.FuncAttrs, enum.FuncAttrNoReturn)
	}
	return l.exit
}

// panicOverflowFunc lazily emits `void @lyra_panic_overflow()` (noreturn) into
// the module: write the overflow message to stderr, then exit(101). Defined as a
// real body (like the rc runtime), so `lyrac build`'s single `clang out.ll` stays
// self-contained. Cached, so every overflow site shares the one function.
func (l *lowerer) panicOverflowFunc() *ir.Func {
	if l.panicOverflow != nil {
		return l.panicOverflow
	}
	fn := l.module.NewFunc("lyra_panic_overflow", lltypes.Void)
	fn.FuncAttrs = append(fn.FuncAttrs, enum.FuncAttrNoReturn)
	b := fn.NewBlock("entry")
	msg := l.cString(overflowTrapMessage)
	b.NewCall(l.writeFunc(),
		constant.NewInt(lltypes.I32, 2), // stderr
		msg,
		constant.NewInt(lltypes.I64, int64(len(overflowTrapMessage))))
	b.NewCall(l.exitFunc(), constant.NewInt(lltypes.I32, overflowTrapExitCode))
	b.NewUnreachable()
	l.panicOverflow = fn
	return l.panicOverflow
}

// overflowIntrinsic lazily declares the LLVM checked-arithmetic intrinsic for
// (op, signedness, width) — e.g. `llvm.sadd.with.overflow.i32` — and caches it by
// name. The intrinsic takes two iN and returns `{ iN, i1 }` (result, overflow
// bit). op must be add/sub/mul; the caller guards that.
func (l *lowerer) overflowIntrinsic(op string, signed bool, intTy *lltypes.IntType) (*ir.Func, error) {
	sign := "u"
	if signed {
		sign = "s"
	}
	name := fmt.Sprintf("llvm.%s%s.with.overflow.i%d", sign, op, intTy.BitSize)
	if fn, ok := l.overflowIntrinsics[name]; ok {
		return fn, nil
	}
	retTy := lltypes.NewStruct(intTy, lltypes.I1)
	fn := l.module.NewFunc(name, retTy, ir.NewParam("", intTy), ir.NewParam("", intTy))
	l.overflowIntrinsics[name] = fn
	return fn, nil
}

// emitCheckedIntOp emits an overflow-checked integer `+`/`-`/`*`: it calls the
// with-overflow intrinsic, and on overflow branches to a trap (report + abort);
// otherwise it falls through with the result. Because it splits the current block
// (the trap branch), it returns the block control ends up in — the caller must
// keep lowering into that one.
func (l *lowerer) emitCheckedIntOp(block *ir.Block, op string, left, right value.Value, signed bool) (value.Value, *ir.Block, error) {
	intTy, ok := left.Type().(*lltypes.IntType)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: checked %s on a non-integer operand (%s)", op, left.Type())
	}
	intrinsic, err := l.overflowIntrinsic(op, signed, intTy)
	if err != nil {
		return nil, nil, err
	}
	agg := block.NewCall(intrinsic, left, right)
	result := block.NewExtractValue(agg, 0)
	overflowed := block.NewExtractValue(agg, 1)

	fn := block.Parent
	trap := fn.NewBlock("")
	cont := fn.NewBlock("")
	block.NewCondBr(overflowed, trap, cont)

	trap.NewCall(l.panicOverflowFunc())
	trap.NewUnreachable()

	return result, cont, nil
}
