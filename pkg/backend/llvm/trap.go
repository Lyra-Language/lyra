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
	// A string is indexed and sliced in *runes*, not elements, and both had been
	// reporting the array message — which sends the reader looking for an array. The
	// slice message says "range" rather than "index" because it covers a second
	// mistake the index form cannot make: `start > end`, an inverted range, which
	// traps rather than quietly yielding "".
	stringIndexOOBTrapMessage = "lyra: string index out of bounds\n"
	stringSliceOOBTrapMessage = "lyra: string slice out of range\n"
	matchFailedTrapMessage    = "lyra: match not exhaustive\n"
	shiftOverflowTrapMessage  = "lyra: shift amount out of range\n"
	rangeStepTrapMessage      = "lyra: range step must be positive\n"
	constraintTrapMessage     = "lyra: value violates its newtype's constraint\n"
	// The user message and its newline follow this at run time, so unlike the four
	// above it carries neither.
	panicPrefixMessage = "lyra: panic: "
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

func (l *lowerer) panicStringIndexOOBFunc() *ir.Func {
	return l.panicFunc("lyra_panic_string_index_out_of_bounds", stringIndexOOBTrapMessage)
}

func (l *lowerer) panicStringSliceOOBFunc() *ir.Func {
	return l.panicFunc("lyra_panic_string_slice_out_of_range", stringSliceOOBTrapMessage)
}

func (l *lowerer) panicMatchFailedFunc() *ir.Func {
	return l.panicFunc("lyra_panic_match_failed", matchFailedTrapMessage)
}

// panicShiftOverflowFunc is the trap for a shift whose amount is at or beyond the
// shifted type's width (or negative). LLVM's `shl`/`lshr`/`ashr` are *undefined
// behavior* there, exactly as div/rem are on a zero divisor, so it is checked for
// the same reason: the alternative is a silently platform-shaped answer, which is
// what the fixed-width primitives exist to rule out.
func (l *lowerer) panicShiftOverflowFunc() *ir.Func {
	return l.panicFunc("lyra_panic_shift_overflow", shiftOverflowTrapMessage)
}

// panicRangeStepFunc is the trap for a for-in range whose *runtime* step is zero or
// negative — a step that never advances, so the loop would spin forever. It rides the
// ladder a shift amount rides: a constant is refused at check time
// (types.InvalidStepReason), and a value only knowable at run time gets the same rule
// as this trap, because the alternative is a silent infinite loop. A comprehension
// answers the same degenerate step with an empty array instead — its count is computed
// up front, so "never advances" has a defined size there (see rangeSource).
func (l *lowerer) panicRangeStepFunc() *ir.Func {
	return l.panicFunc("lyra_panic_range_step", rangeStepTrapMessage)
}

// panicConstraintFunc is the trap for a value that reaches a constrained newtype
// without satisfying its `range`/`values`/`step` constraint — the second rung of the
// ladder those constraints did not have until 08/13.
//
// A constraint was a compile-time assertion and nothing else: it caught a literal and
// silently admitted everything else, so `Percent(n)` with a runtime `n` of 200 built,
// ran and printed 200 on a `range(0..<=100)`. That is the shape the language rejects
// everywhere else — provable → compile error, otherwise → trap — and the values a
// constrained newtype sees at run time (parsed input, computed results) are exactly
// the ones a range mistake lives in.
//
// One trap for all three constraint kinds rather than one each: the message names
// what happened, and the *location* is the construction, which is what a reader
// needs. Splitting them would multiply runtime strings to distinguish cases the
// source already distinguishes.
func (l *lowerer) panicConstraintFunc() *ir.Func {
	return l.panicFunc("lyra_panic_constraint", constraintTrapMessage)
}

// panicMessageFunc lazily emits the trap behind a user-written `panic(msg)`:
// `void @lyra_panic(i8* data, i64 len)`, noreturn, writing "lyra: panic: " then the
// caller's bytes then a newline to stderr before exit(trapExitCode).
//
// It takes the message as a (pointer, length) pair rather than baking it in like
// the four compiler traps above, because a user message is a runtime `string` — an
// interpolated one ("index ${i} out of range") is the case worth having — and a
// Lyra string is exactly that fat pointer (STRING_LAYOUT.md). Cached like the
// others, so N panic sites share one function and pay a `call`+`unreachable` each.
//
// The three writes are separate calls rather than one assembled buffer: assembling
// would mean allocating, and a trap must work when the reason for trapping is that
// something is already wrong. stderr is unbuffered, so the ordering holds.
func (l *lowerer) panicMessageFunc() *ir.Func {
	const name = "lyra_panic"
	if fn, ok := l.panics[name]; ok {
		return fn
	}
	data := ir.NewParam("data", lltypes.NewPointer(lltypes.I8))
	length := ir.NewParam("len", lltypes.I64)
	fn := l.module.NewFunc(name, lltypes.Void, data, length)
	fn.FuncAttrs = append(fn.FuncAttrs, enum.FuncAttrNoReturn)
	b := fn.NewBlock("entry")
	stderr := constant.NewInt(lltypes.I32, 2)
	b.NewCall(l.writeFunc(), stderr, l.cString(panicPrefixMessage),
		constant.NewInt(lltypes.I64, int64(len(panicPrefixMessage))))
	b.NewCall(l.writeFunc(), stderr, data, length)
	b.NewCall(l.writeFunc(), stderr, l.newlinePtr(), constant.NewInt(lltypes.I64, 1))
	b.NewCall(l.exitFunc(), constant.NewInt(lltypes.I32, trapExitCode))
	b.NewUnreachable()
	l.panics[name] = fn
	return fn
}

// lowerPanicCall lowers `panic(msg)` to that call plus an `unreachable`, and returns
// **no value** with the block sealed.
//
// Sealing is the whole mechanism: every construct that joins branch values already
// guards its fall-through and its phi incoming on `end.Term == nil` — the two-armed
// `if` (control_flow.go), and each of the three match lowerings — because a branch
// ending in `return`/`break`/`continue` had to be handled long before this existed.
// A panicking arm is that same shape, so `never` needed no new code there at all.
func (l *lowerer) lowerPanicCall(block *ir.Block, e *ast.FunctionCallExpr) (value.Value, *ir.Block, error) {
	if len(e.Arguments) != 1 {
		return nil, nil, fmt.Errorf("llvm: panic expects 1 argument, got %d", len(e.Arguments))
	}
	arg := e.Arguments[0]
	argType, ok := l.recordedType(arg)
	if !ok {
		return nil, nil, fmt.Errorf("llvm: no type recorded for panic message")
	}
	val, block, err := l.lowerExpr(block, arg)
	if err != nil {
		return nil, nil, err
	}
	// formatForPrint on a string hands back its data pointer and byte length — the
	// same extraction print does, rather than a second copy of the fat-pointer layout.
	dataPtr, length, err := l.formatForPrint(block, val, argType)
	if err != nil {
		return nil, nil, err
	}
	block.NewCall(l.panicMessageFunc(), dataPtr, length)
	block.NewUnreachable()
	return nil, block, nil
}

// diverged reports whether lowering an operand ended the block instead of yielding a
// value — today only a `panic(…)`, which sealed it with `unreachable`.
//
// A caller that consumes an operand's value must check this before touching it: the
// value is nil, and whatever the caller was about to emit (a store, a call) cannot
// run, because control does not reach it. `if v == nil` alone is not the test —
// several lowerings legitimately produce no value while still reaching the next
// statement (a void branch, a statement-position `if`) — so the *sealed block* is
// what distinguishes "diverged" from "produced nothing".
func diverged(v value.Value, block *ir.Block) bool {
	return v == nil && block != nil && block.Term != nil
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
	// Signed 128-bit multiply is not an intrinsic here: LLVM expands it into a call
	// to compiler-rt's `__muloti4`, which Linux clang does not link (see
	// i128MulOverflow). Our own helper has the same `{ iN, i1 }` shape, so the caller
	// is unaffected.
	if op == "mul" && signed && intTy.BitSize == 128 {
		return l.i128MulOverflow(), nil
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

// i128MulOverflow lazily defines `{ i128, i1 } @lyra_i128_mul_overflow(i128, i128)`
// — the signed 128-bit checked multiply — and caches it.
//
// It exists because `llvm.smul.with.overflow.i128` is not lowered inline: the backend
// expands it into a call to **`__muloti4`**, which lives only in compiler-rt. clang
// links compiler-rt by default on macOS but **libgcc on Linux**, and libgcc has no
// `__muloti4`, so `lyrac build` of an i128 multiply failed at link time there with an
// undefined reference — while the same IR linked fine on macOS. (Division is not
// affected: `__divti3` is in both. Unsigned 128-bit multiply is not affected either,
// since LLVM expands `llvm.umul.with.overflow.i128` inline. Signed multiply is the one
// operation that needs this.)
//
// Emitting the helper into the module keeps `clang out.ll` self-contained on every
// platform, which is the same reason the ref-counted runtime and the 128-bit decimal
// formatter are emitted as real function bodies rather than linked in. Defining
// `__muloti4` itself under its compiler-rt name would also have worked, but squatting
// on another runtime's ABI symbol invites a clash for no benefit; our own name cannot
// collide, and returning `{ iN, i1 }` means the call site is identical to the
// intrinsic's, so emitCheckedIntOp needs no special case.
//
// The overflow test is compiler-rt's: `a == 0` cannot overflow; `a == -1` overflows
// exactly when `b` is the minimum (whose negation is unrepresentable); otherwise
// `(a*b)/a != b`. The `a == -1` case is split out rather than folded into the division
// because `sdiv INT128_MIN, -1` is itself undefined — checking it via the division it
// exists to avoid would reintroduce the trap.
func (l *lowerer) i128MulOverflow() *ir.Func {
	if l.mulOverflowI128 != nil {
		return l.mulOverflowI128
	}
	i128 := lltypes.I128
	a := ir.NewParam("a", i128)
	b := ir.NewParam("b", i128)
	retTy := lltypes.NewStruct(i128, lltypes.I1)
	fn := l.module.NewFunc("lyra_i128_mul_overflow", retTy, a, b)

	entry := fn.NewBlock("entry")
	checkNegOne := fn.NewBlock("checkNegOne")
	negOne := fn.NewBlock("negOne")
	divCheck := fn.NewBlock("divCheck")
	noOverflow := fn.NewBlock("noOverflow")
	merge := fn.NewBlock("merge")

	konst := func(n int64) *constant.Int { return constant.NewInt(i128, n) }

	// The product is computed unconditionally (wrapping); only the *detection* branches.
	product := entry.NewMul(a, b)
	entry.NewCondBr(entry.NewICmp(enum.IPredEQ, a, konst(0)), noOverflow, checkNegOne)

	// a == -1 overflows only for b == INT128_MIN.
	bIsMin := checkNegOne.NewICmp(enum.IPredEQ, b, intMinConst(i128))
	checkNegOne.NewCondBr(checkNegOne.NewICmp(enum.IPredEQ, a, konst(-1)), negOne, divCheck)
	negOne.NewBr(merge)

	// a is neither 0 nor -1, so this division can neither divide by zero nor hit the
	// INT_MIN/-1 case: it is safe, and it is the check.
	roundTrip := divCheck.NewSDiv(product, a)
	divergent := divCheck.NewICmp(enum.IPredNE, roundTrip, b)
	divCheck.NewBr(merge)

	noOverflow.NewBr(merge)

	overflowed := merge.NewPhi(
		ir.NewIncoming(constant.False, noOverflow),
		ir.NewIncoming(bIsMin, negOne),
		ir.NewIncoming(divergent, divCheck),
	)
	out := merge.NewInsertValue(constant.NewUndef(retTy), product, 0)
	out2 := merge.NewInsertValue(out, overflowed, 1)
	merge.NewRet(out2)

	l.mulOverflowI128 = fn
	return fn
}
