package llvm

import (
	"fmt"

	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/constant"
	lltypes "github.com/llir/llvm/ir/types"
	"github.com/llir/llvm/ir/value"

	"github.com/Lyra-Language/lyra/pkg/ast"
)

// The clock — the `wall_clock_nanos()` builtin, `() -> i64`.
//
// Same division of labour as `random_seed` beside the prelude's `Rng`, and the same
// reason: asking the operating system what time it is cannot be expressed in Lyra,
// while everything derived from the answer — seconds, elapsed durations, formatting —
// is ordinary arithmetic and belongs in the prelude. Only the syscall is a builtin.
//
// It replaced a `wallClock` entry in `checker/effects.go`'s builtinEffects table that
// named nothing at all: tagged EffectTime, with no typechecker signature and no
// lowering anywhere. That is precisely the shape `Random.global()` had, and leaving it
// there is what let *that* one look implemented for months — a program writing it got
// a clean `lyrac check` and a backend crash. Deleting the entry was the alternative,
// and was rejected for leaving EffectTime a bit no expression could carry, which is
// the same phantom one rung down.
//
// Three decisions the old name did not record:
//
//   - **snake_case**, because every other name in the language is
//     (`random_seed`, `read_line`, `parse_i64`, `println`). `wallClock` matched
//     nothing.
//   - **The unit is in the name.** A clock returning a bare number invites the
//     reader to guess seconds, millis or nanos, and the guess is silent when wrong;
//     Go's `UnixNano` and Zig's `nanoTimestamp` both spell it out for this reason.
//   - **`i64`, not `u64`.** The useful operation on two instants is subtraction, and
//     a difference is signed. Nanoseconds in an i64 run to the year 2262, which is
//     the same trade Go makes.
//
// Wall-clock, not monotonic: it can jump backwards when the system clock is adjusted,
// so it answers "what time is it" and not "how long did that take". A monotonic clock
// is a second builtin if something needs one, not a reinterpretation of this one.

// ShimWallClockNanos returns nanoseconds since the Unix epoch.
const ShimWallClockNanos = "lyra_wall_clock_nanos"

// ensureWallClockRuntime emits `lyra_wall_clock_nanos` into the module the first
// time it is needed, caching the handle on the lowerer (idempotent).
//
// `clock_gettime(CLOCK_REALTIME, &ts)` is the portable spelling on both targets
// (macOS 10.12+, any glibc), and CLOCK_REALTIME is 0 on both. `struct timespec` is
// two 64-bit words there — `{ time_t tv_sec; long tv_nsec; }` — so it is built here
// as `[2 x i64]` rather than a named struct type; nothing else in the module needs
// the type, and an array keeps the GEPs obvious.
//
// **The slot is zeroed before the call**, for the reason `random_seed`'s fallback is
// written before its getentropy call rather than after a failure test: a failed
// clock_gettime leaves the struct unspecified, so a program that ignored the return
// value would be reading uninitialized stack. Zeroing makes the failure answer "the
// epoch" — a wrong time, but a defined one, and one a caller can recognize.
func (l *lowerer) ensureWallClockRuntime() *ir.Func {
	if l.wallClock != nil {
		return l.wallClock
	}
	i8ptr := lltypes.NewPointer(lltypes.I8)
	tsType := lltypes.NewArray(2, lltypes.I64)

	clockGettime := l.module.NewFunc("clock_gettime", lltypes.I32,
		ir.NewParam("", lltypes.I32), ir.NewParam("", i8ptr))

	fn := l.module.NewFunc(ShimWallClockNanos, lltypes.I64)
	b := fn.NewBlock("entry")
	ts := b.NewAlloca(tsType)

	zero := constant.NewInt(lltypes.I32, 0)
	secPtr := b.NewGetElementPtr(tsType, ts, zero, constant.NewInt(lltypes.I32, 0))
	nsecPtr := b.NewGetElementPtr(tsType, ts, zero, constant.NewInt(lltypes.I32, 1))
	b.NewStore(constant.NewInt(lltypes.I64, 0), secPtr)
	b.NewStore(constant.NewInt(lltypes.I64, 0), nsecPtr)

	// CLOCK_REALTIME is 0 on both Linux and macOS.
	b.NewCall(clockGettime, constant.NewInt(lltypes.I32, 0), b.NewBitCast(ts, i8ptr))

	sec := b.NewLoad(lltypes.I64, secPtr)
	nsec := b.NewLoad(lltypes.I64, nsecPtr)
	// Plain mul/add, not the checked forms: this is runtime support, below the
	// language's arithmetic, and the product cannot overflow an i64 for any epoch
	// second a clock_gettime can report.
	b.NewRet(b.NewAdd(b.NewMul(sec, constant.NewInt(lltypes.I64, 1_000_000_000)), nsec))

	l.wallClock = fn
	return fn
}

// lowerWallClockNanosCall lowers `wall_clock_nanos()` to a single call.
//
// The result is an `i64` — a plain scalar owning nothing — so like `random_seed` and
// unlike `read_line` there is no ownership question and nothing for the temp
// machinery to do.
func (l *lowerer) lowerWallClockNanosCall(block *ir.Block, e *ast.FunctionCallExpr) (value.Value, *ir.Block, error) {
	if len(e.Arguments) != 0 {
		return nil, nil, fmt.Errorf("llvm: wall_clock_nanos expects 0 arguments, got %d", len(e.Arguments))
	}
	return block.NewCall(l.ensureWallClockRuntime()), block, nil
}
