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
// default; wraparound explicit"). Integer arithmetic that could go wrong at run
// time — `+`/`-`/`*` overflow, `-INT_MIN`, division overflow (`INT_MIN / -1`), and
// divide-by-zero — lowers to a check that, on the bad case, branches to a trap
// that reports and aborts. The explicit escape hatches for the overflow cases are
// `wrapping_*`/`saturating_*` (the builtin-method registry, wrapping.go).
//
// This file provides the runtime traps + the check-and-branch helper; arithmetic.go
// emits the specific conditions. Each trap is a single per-module noreturn function
// (not inlined at each site), so N sites cost one `call`+`unreachable` each rather
// than N copies of the message-write + exit.

// trapExitCode is the process exit status on any arithmetic trap. 101 follows
// Rust's panic convention — a distinctive, non-zero, deterministic code (nothing
// else in a Lyra program exits with it), so a test can assert the trap fired
// rather than merely that the process failed.
const trapExitCode = 101

// Trap messages, written to stderr (fd 2) before the process exits.
const (
	overflowTrapMessage     = "lyra: arithmetic overflow\n"
	divideByZeroTrapMessage = "lyra: divide by zero\n"
	indexOOBTrapMessage     = "lyra: array index out of bounds\n"
	matchFailedTrapMessage  = "lyra: match not exhaustive\n"
)

// overflowTrapExitCode is retained as the name existing tests use; it is trapExitCode.
const overflowTrapExitCode = trapExitCode

// exitFunc lazily declares libc's `void @exit(i32)` (noreturn).
func (l *lowerer) exitFunc() *ir.Func {
	if l.exit == nil {
		l.exit = l.module.NewFunc("exit", lltypes.Void, ir.NewParam("", lltypes.I32))
		l.exit.FuncAttrs = append(l.exit.FuncAttrs, enum.FuncAttrNoReturn)
	}
	return l.exit
}

// panicFunc lazily emits a noreturn `void @name()` into the module: write msg to
// stderr, then exit(trapExitCode). Defined as a real body (like the rc runtime),
// so `lyrac build`'s single `clang out.ll` stays self-contained. Cached by name,
// so every trap site of a given kind shares the one function.
func (l *lowerer) panicFunc(name, msg string) *ir.Func {
	if fn, ok := l.panics[name]; ok {
		return fn
	}
	fn := l.module.NewFunc(name, lltypes.Void)
	fn.FuncAttrs = append(fn.FuncAttrs, enum.FuncAttrNoReturn)
	b := fn.NewBlock("entry")
	b.NewCall(l.writeFunc(),
		constant.NewInt(lltypes.I32, 2), // stderr
		l.cString(msg),
		constant.NewInt(lltypes.I64, int64(len(msg))))
	b.NewCall(l.exitFunc(), constant.NewInt(lltypes.I32, trapExitCode))
	b.NewUnreachable()
	l.panics[name] = fn
	return fn
}

func (l *lowerer) panicOverflowFunc() *ir.Func {
	return l.panicFunc("lyra_panic_overflow", overflowTrapMessage)
}

func (l *lowerer) panicDivideByZeroFunc() *ir.Func {
	return l.panicFunc("lyra_panic_divide_by_zero", divideByZeroTrapMessage)
}

func (l *lowerer) panicIndexOOBFunc() *ir.Func {
	return l.panicFunc("lyra_panic_index_out_of_bounds", indexOOBTrapMessage)
}

func (l *lowerer) panicMatchFailedFunc() *ir.Func {
	return l.panicFunc("lyra_panic_match_failed", matchFailedTrapMessage)
}

// sealMatchFallthrough terminates a match ladder's (or tag switch's) unmatched
// fall-through edge with the standard trap instead of a bare `unreachable`.
//
// Exhaustiveness is a hard error only for `bool` and `data` scrutinees; for
// int/string/rune/float/array/tuple/struct it is a *warning*, and warnings never
// gate a build — so a non-exhaustive match reaches this edge with a value no arm
// covers. `unreachable` made that undefined behavior (SIGTRAP at -O0, arbitrary
// under optimization); a guarded-only match reached it deterministically. Trapping
// keeps the failure inside the language's own discipline: a message on stderr and
// exit 101, exactly like a failed bounds check or a divide by zero.
//
// Used on the edges the compiler cannot prove dead. Where exhaustiveness *is*
// enforced (a bool ladder, a `data` tag switch) this is defense in depth: the call
// is unreachable in a well-typed program and costs one basic block.
func (l *lowerer) sealMatchFallthrough(block *ir.Block) {
	block.NewCall(l.panicMatchFailedFunc())
	block.NewUnreachable()
}

// emitTrapIf branches to a trap when cond is true and continues otherwise: it
// creates a trap block (call trapFn; unreachable) and a continuation block, cond-
// brs `block` between them, and returns the continuation — the caller keeps
// lowering into that. The shared shape behind every arithmetic check.
func (l *lowerer) emitTrapIf(block *ir.Block, cond value.Value, trapFn *ir.Func) *ir.Block {
	fn := block.Parent
	trap := fn.NewBlock("")
	cont := fn.NewBlock("")
	block.NewCondBr(cond, trap, cont)
	trap.NewCall(trapFn)
	trap.NewUnreachable()
	return cont
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
	cont := l.emitTrapIf(block, overflowed, l.panicOverflowFunc())
	return result, cont, nil
}
