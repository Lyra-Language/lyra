package llvm

import (
	"errors"
	"os/exec"
	"testing"

	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/constant"
	"github.com/llir/llvm/ir/enum"
	lltypes "github.com/llir/llvm/ir/types"
	"github.com/llir/llvm/ir/value"
)

// runModule compiles a hand-built module with clang and returns the exit code of
// the resulting binary — the raw-IR counterpart to buildAndRun (which starts
// from Lyra source). Used to exercise the emitted runtime directly, since no
// Lyra-level construct calls retain/release yet.
func runModule(t *testing.T, m *ir.Module) int {
	t.Helper()
	clang := lookClang(t)
	runErr := exec.Command(compileCached(t, clang, m.String())).Run()
	if runErr == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(runErr, &ee) {
		return ee.ExitCode()
	}
	t.Fatalf("running the binary failed: %v", runErr)
	return -1
}

// TestExec_RCRuntime exercises the emitted ref-counted runtime end to end by
// hand-building a `main` that drives it and checks the observable refcount
// transitions, returning 42 only if every check holds:
//
//   - alloc sets rc = 1
//   - retain bumps it to 2
//   - release drops it back to 1 (and does NOT free while rc > 0)
//   - a pinned box (rc = PinnedRC) ignores both retain and release
//
// It then releases the live box to 0, which frees it — the program still exits
// cleanly, so free() received a valid box pointer (a bad pointer would crash).
func TestExec_RCRuntime(t *testing.T) {
	t.Parallel()
	m := ir.NewModule()
	l := &lowerer{module: m}
	l.ensureRCRuntime()

	i64ptr := lltypes.NewPointer(lltypes.I64)
	pinned := constant.NewInt(lltypes.I64, -1) // PinnedRC bit pattern
	null := constant.NewNull(lltypes.NewPointer(lltypes.I8))

	main := m.NewFunc("main", lltypes.I32)
	b := main.NewBlock("entry")

	loadRC := func(box value.Value) value.Value {
		return b.NewLoad(lltypes.I64, b.NewBitCast(box, i64ptr))
	}

	// Non-pinned box: alloc → retain → release, observing rc at each step.
	box := b.NewCall(l.rcAlloc, constant.NewInt(lltypes.I64, 24))
	rcAlloc := loadRC(box)
	b.NewCall(l.rcRetain, box)
	rcRetain := loadRC(box)
	b.NewCall(l.rcRelease, box, null)
	rcRelease := loadRC(box)

	// Pinned box: force rc = PinnedRC, then retain/release must leave it untouched.
	pbox := b.NewCall(l.rcAlloc, constant.NewInt(lltypes.I64, 24))
	b.NewStore(pinned, b.NewBitCast(pbox, i64ptr))
	b.NewCall(l.rcRetain, pbox)
	rcPinRetain := loadRC(pbox)
	b.NewCall(l.rcRelease, pbox, null)
	rcPinRelease := loadRC(pbox)

	eq := func(v value.Value, n int64) value.Value {
		return b.NewICmp(enum.IPredEQ, v, constant.NewInt(lltypes.I64, n))
	}
	ok := b.NewAnd(eq(rcAlloc, 1), eq(rcRetain, 2))
	ok = b.NewAnd(ok, eq(rcRelease, 1))
	ok = b.NewAnd(ok, b.NewICmp(enum.IPredEQ, rcPinRetain, pinned))
	ok = b.NewAnd(ok, b.NewICmp(enum.IPredEQ, rcPinRelease, pinned))

	// Free the still-live box (rc 1 → 0): must not crash on exit.
	b.NewCall(l.rcRelease, box, null)

	b.NewRet(b.NewSelect(ok, constant.NewInt(lltypes.I32, 42), constant.NewInt(lltypes.I32, 0)))

	if got := runModule(t, m); got != 42 {
		t.Errorf("RC runtime checks failed: exit %d, want 42", got)
	}
}

// TestExec_RCReleaseCallsDrop verifies the drop_fn path: release-to-zero calls
// drop_fn(payload) before free. The drop function writes a sentinel through a
// global flag pointer stashed in the payload, and main returns it — proving the
// drop ran, received the payload pointer (box + header), and ran before free.
func TestExec_RCReleaseCallsDrop(t *testing.T) {
	t.Parallel()
	m := ir.NewModule()
	l := &lowerer{module: m}
	l.ensureRCRuntime()

	i8ptr := lltypes.NewPointer(lltypes.I8)

	// A global i64 the drop function sets to 7.
	flag := m.NewGlobalDef("drop_flag", constant.NewInt(lltypes.I64, 0))

	// void @dropper(i8* %payload): *drop_flag = 7. (Ignores the payload; its job
	// here is just to prove it was invoked on the release-to-zero path.)
	dropper := m.NewFunc("dropper", lltypes.Void, ir.NewParam("payload", i8ptr))
	db := dropper.NewBlock("entry")
	db.NewStore(constant.NewInt(lltypes.I64, 7), flag)
	db.NewRet(nil)

	main := m.NewFunc("main", lltypes.I32)
	b := main.NewBlock("entry")
	box := b.NewCall(l.rcAlloc, constant.NewInt(lltypes.I64, 16))
	dropAsI8 := b.NewBitCast(dropper, i8ptr)
	b.NewCall(l.rcRelease, box, dropAsI8) // rc 1 → 0: drops then frees
	got := b.NewLoad(lltypes.I64, flag)
	b.NewRet(b.NewTrunc(got, lltypes.I32))

	if got := runModule(t, m); got != 7 {
		t.Errorf("drop_fn not invoked on release-to-zero: exit %d, want 7", got)
	}
}

// TestExec_I128MulOverflowHelper drives the emitted signed-128-bit checked multiply
// directly, because two of its cases cannot be written in Lyra at all: a 128-bit
// literal is not representable yet (IntegerLiteralExpr.Value is an int64), so
// INT128_MIN has no source spelling, and it is exactly the value the helper special-
// cases.
//
// That special case is not decoration. The general test is `(a*b)/a != b`, but for
// a == -1 and b == INT128_MIN the product wraps back to INT128_MIN and the division
// would be `sdiv INT128_MIN, -1` — itself undefined in LLVM, so checking that case
// through the division it exists to detect would reintroduce the fault.
//
// Returns 42 only if every case agrees.
func TestExec_I128MulOverflowHelper(t *testing.T) {
	t.Parallel()
	m := ir.NewModule()
	l := &lowerer{module: m}
	helper := l.i128MulOverflow()

	i128 := lltypes.I128
	main := m.NewFunc("main", lltypes.I32)
	entry := main.NewBlock("entry")
	fail := main.NewBlock("fail")

	min := intMinConst(i128)
	max := intMaxConst(i128)
	k := func(n int64) *constant.Int { return constant.NewInt(i128, n) }

	cases := []struct {
		name     string
		a, b     value.Value
		wantOv   bool
		wantProd value.Value // nil to skip the product check
	}{
		// Zero short-circuits before any division.
		{"0 * max", k(0), max, false, k(0)},
		{"max * 0", max, k(0), false, k(0)},
		// -1 is the special case: fine for anything but the minimum...
		{"-1 * max", k(-1), max, false, nil},
		// ...and an overflow for it, since -INT128_MIN is unrepresentable.
		{"-1 * min", k(-1), min, true, nil},
		{"min * -1", min, k(-1), true, nil},
		// The ordinary paths, through the division check.
		{"6 * 7", k(6), k(7), false, k(42)},
		{"max * 2", max, k(2), true, nil},
		{"min * 2", min, k(2), true, nil},
		{"-6 * 7", k(-6), k(7), false, k(-42)},
	}

	block := entry
	for _, c := range cases {
		agg := block.NewCall(helper, c.a, c.b)
		gotOv := block.NewExtractValue(agg, 1)
		want := constant.False
		if c.wantOv {
			want = constant.True
		}
		next := main.NewBlock("")
		block.NewCondBr(block.NewICmp(enum.IPredEQ, gotOv, want), next, fail)
		block = next

		if c.wantProd != nil {
			gotProd := block.NewExtractValue(agg, 0)
			next2 := main.NewBlock("")
			block.NewCondBr(block.NewICmp(enum.IPredEQ, gotProd, c.wantProd), next2, fail)
			block = next2
		}
	}
	block.NewRet(constant.NewInt(lltypes.I32, 42))
	fail.NewRet(constant.NewInt(lltypes.I32, 1))

	if got := runModule(t, m); got != 42 {
		t.Errorf("i128 checked-multiply helper disagreed with the expected results (exit %d; 42 means all cases passed)", got)
	}
}
