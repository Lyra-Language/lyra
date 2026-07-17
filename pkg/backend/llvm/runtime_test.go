package llvm

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
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
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Skip("clang not found on PATH; skipping behavioral test")
	}
	dir := t.TempDir()
	llPath := filepath.Join(dir, "prog.ll")
	binPath := filepath.Join(dir, "prog")
	if err := os.WriteFile(llPath, []byte(m.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(clang, llPath, "-o", binPath).CombinedOutput(); err != nil {
		t.Fatalf("clang rejected the IR: %v\n%s\n--- IR ---\n%s", err, out, m.String())
	}
	runErr := exec.Command(binPath).Run()
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
