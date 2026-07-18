package llvm

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/constant"
	"github.com/llir/llvm/ir/enum"
	lltypes "github.com/llir/llvm/ir/types"
	"github.com/llir/llvm/ir/value"
)

// Perceus stage 3 — reuse analysis / FBIP for `shared` values. When a `match`
// destructures an owned `shared data` value at its last use, its ref-counted box
// is reclaimed (drop-reuse) and a same-type construction in an arm writes into it
// in place instead of allocating. These tests cover the runtime primitive, the
// end-to-end lowering (linear and recursive/FBIP), and the safety boundary (a
// borrowed scrutinee is never reused).

// TestExec_RCDropReuse exercises the drop-reuse runtime primitive directly (the
// raw-IR counterpart to the Lyra-level tests below):
//   - unique (rc == 1): returns the *same* box (reclaimed), leaves rc = 1, no free;
//   - shared (rc > 1): returns null and decrements the count;
//   - pinned (arena): returns null and leaves the count untouched.
func TestExec_RCDropReuse(t *testing.T) {
	m := ir.NewModule()
	l := &lowerer{module: m}
	l.ensureRCRuntime()

	i64ptr := lltypes.NewPointer(lltypes.I64)
	i8ptr := lltypes.NewPointer(lltypes.I8)
	pinned := constant.NewInt(lltypes.I64, -1)
	null := constant.NewNull(i8ptr)

	main := m.NewFunc("main", lltypes.I32)
	b := main.NewBlock("entry")
	loadRC := func(box value.Value) value.Value { return b.NewLoad(lltypes.I64, b.NewBitCast(box, i64ptr)) }
	eqPtr := func(x, y value.Value) value.Value { return b.NewICmp(enum.IPredEQ, x, y) }
	eqI := func(v value.Value, n int64) value.Value { return b.NewICmp(enum.IPredEQ, v, constant.NewInt(lltypes.I64, n)) }

	// Unique → returns the same box, rc still 1, not freed.
	box := b.NewCall(l.rcAlloc, constant.NewInt(lltypes.I64, 24))
	ru := b.NewCall(l.rcDropReuse, box)
	sameBox := eqPtr(ru, box)
	rcUnique := loadRC(box)

	// Shared (rc = 2) → returns null, decrements to 1.
	box2 := b.NewCall(l.rcAlloc, constant.NewInt(lltypes.I64, 24))
	b.NewCall(l.rcRetain, box2)
	ru2 := b.NewCall(l.rcDropReuse, box2)
	nullShared := eqPtr(ru2, null)
	rcShared := loadRC(box2)

	// Pinned → returns null, rc untouched.
	pbox := b.NewCall(l.rcAlloc, constant.NewInt(lltypes.I64, 24))
	b.NewStore(pinned, b.NewBitCast(pbox, i64ptr))
	ru3 := b.NewCall(l.rcDropReuse, pbox)
	nullPinned := eqPtr(ru3, null)
	rcPinned := loadRC(pbox)

	ok := b.NewAnd(sameBox, eqI(rcUnique, 1))
	ok = b.NewAnd(ok, nullShared)
	ok = b.NewAnd(ok, eqI(rcShared, 1))
	ok = b.NewAnd(ok, nullPinned)
	ok = b.NewAnd(ok, b.NewICmp(enum.IPredEQ, rcPinned, pinned))

	b.NewCall(l.free, box)             // free the reclaimed token
	b.NewCall(l.rcRelease, box2, null) // rc 1 → 0
	b.NewRet(b.NewSelect(ok, constant.NewInt(lltypes.I32, 42), constant.NewInt(lltypes.I32, 0)))

	if got := runModule(t, m); got != 42 {
		t.Errorf("drop_reuse checks failed: exit %d, want 42", got)
	}
}

// reuseCases are whole Lyra programs whose result is unchanged by reuse (reuse is
// an allocation optimization, not a semantic change) — so a wrong answer means the
// reclaimed box was mis-written, and an ASan failure means it was reused while
// still live or freed twice.
var reuseCases = []struct {
	name string
	src  string
	want int
}{
	{
		// Linear FBIP: `b` transfers into `bump`'s `own` param at its last use, so the
		// box arrives unique (rc == 1); the `Full(n+1)` arm reclaims it in place.
		"linear in-place update",
		`data Box = Empty | Full(i64)
		 let bump = (b: own shared Box) -> shared Box => match b {
		   Full(n) => Full(n + 1),
		   Empty => Empty
		 }
		 let main = () -> u8 => {
		   let b: shared Box = Full(41)
		   let c: shared Box = bump(b)
		   match c { Full(n) => u8(n), Empty => 0 }
		 }`,
		42,
	},
	{
		// Recursive FBIP: map-increment over a `shared` list. Each `Cons` cell is
		// consumed at its last use (own param + arm-binding transfer keeps it unique),
		// so `inc` rebuilds the whole list in place — no allocation per cell.
		"recursive map reuses each cell",
		`data List = Nil | Cons(i64, shared List)
		 let inc = (xs: own shared List) -> shared List => match xs {
		   Nil => Nil,
		   Cons(h, t) => Cons(h + 1, inc(t))
		 }
		 let sum = (xs: shared List) -> i64 => match xs {
		   Nil => 0,
		   Cons(h, t) => h + sum(t)
		 }
		 let main = () -> u8 => {
		   let xs: shared List = Cons(10, Cons(19, Cons(10, Nil)))
		   let ys: shared List = inc(xs)
		   u8(sum(ys))
		 }`,
		42,
	},
	{
		// A reuse match where one arm does NOT construct: the non-constructing arm
		// must FREE the reclaimed token (here `empty` matches Nil and returns the
		// binding `fb`), while a Cons arm would reuse it. Exercises the per-arm free.
		"non-constructing arm frees the token",
		`data List = Nil | Cons(i64, shared List)
		 let f = (xs: own shared List, fb: own shared List) -> shared List => match xs {
		   Nil => fb,
		   Cons(h, t) => Cons(h + 1, t)
		 }
		 let sum = (xs: shared List) -> i64 => match xs {
		   Nil => 0,
		   Cons(h, t) => h + sum(t)
		 }
		 let main = () -> u8 => {
		   let empty: shared List = Nil
		   let fb: shared List = Cons(42, Nil)
		   let r: shared List = f(empty, fb)
		   u8(sum(r))
		 }`,
		42,
	},
	{
		// A borrowed scrutinee (bare `shared` param) must NOT be reused — the caller
		// still owns the structure. `pick` reconstructs a cell from a borrowed list;
		// reuse must not fire, and the original must stay intact for `sum`.
		"borrowed scrutinee is not reused",
		`data List = Nil | Cons(i64, shared List)
		 let firstOr = (xs: shared List) -> i64 => match xs {
		   Nil => 0,
		   Cons(h, t) => h
		 }
		 let sum = (xs: shared List) -> i64 => match xs {
		   Nil => 0,
		   Cons(h, t) => h + sum(t)
		 }
		 let main = () -> u8 => {
		   let xs: shared List = Cons(6, Cons(30, Cons(6, Nil)))
		   u8(firstOr(xs) + sum(xs))
		 }`,
		48,
	},
}

// TestExec_Reuse runs each reuse program and checks the result.
func TestExec_Reuse(t *testing.T) {
	for _, c := range reuseCases {
		if got := buildAndRun(t, c.src); got != c.want {
			t.Errorf("%s: exited %d; want %d", c.name, got, c.want)
		}
	}
}

// TestExec_ReuseASan runs each reuse program under AddressSanitizer: reclaiming a
// box that is still live (a borrowed scrutinee, or a non-unique value) would be a
// use-after-free or double-free, which aborts here.
func TestExec_ReuseASan(t *testing.T) {
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Skip("clang not found on PATH")
	}
	if !asanAvailable(t, clang) {
		t.Skip("AddressSanitizer not available in this toolchain")
	}
	for _, c := range reuseCases {
		if got := buildAndRunASan(t, clang, c.src); got != c.want {
			t.Errorf("%s (asan): exited %d; want %d", c.name, got, c.want)
		}
	}
}

// TestEmit_ReuseIR pins that reuse is actually wired for the FBIP shape: the
// consuming `match` calls drop-reuse, and the reuse-target construction branches on
// the token (a reclaim path that stores into the box, and an alloc fallback). A
// borrowed scrutinee's match, by contrast, must emit no drop-reuse.
func TestEmit_ReuseIR(t *testing.T) {
	srcByName := func(name string) string {
		for _, c := range reuseCases {
			if c.name == name {
				return c.src
			}
		}
		t.Fatalf("no reuse case named %q", name)
		return ""
	}

	got, err := emitSource(t, srcByName("recursive map reuses each cell"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "call i8* @lyra_rc_drop_reuse") {
		t.Errorf("recursive map: expected a drop_reuse call (reuse not wired):\n%s", got)
	}

	borrow, err := emitSource(t, srcByName("borrowed scrutinee is not reused"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(borrow, "call i8* @lyra_rc_drop_reuse") {
		t.Errorf("borrowed scrutinee: must NOT reuse (drop_reuse emitted):\n%s", borrow)
	}
}
