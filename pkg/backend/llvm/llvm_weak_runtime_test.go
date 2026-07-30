package llvm

import (
	"strings"
	"testing"

	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/value"
)

// `weak` gets runtime semantics: it can be created, it can be upgraded, and the
// upgrade *fails* once the referent is gone.
//
// Before this, `weak T` was a type and nothing more — it collected, resolved, and
// broke E014 size cycles, but no expression produced one, so a struct with a weak
// field could be declared and never built. What makes the feature real is the
// two-count box header: the payload dies when the strong count hits 0, and the
// memory is freed only when the weak count also hits 0, so a weak reference always
// has a live header to read a strong count out of. That is what lets the failed
// upgrade below be safe rather than a read of freed memory.
func TestExec_WeakRuntime(t *testing.T) {
	t.Parallel()
	clang := lookClang(t)
	cases := []struct {
		name string
		src  string
		want int
	}{
		{
			// The upgrade succeeds while the referent is alive, and the branch sees a
			// real `shared T`.
			"upgrade succeeds while the referent lives",
			`struct Node { value: i64 }
			 let main = () -> u8 => {
			   let n: shared Node = Node { value: 7 }
			   let w = n.weak()
			   var out = 0
			   if let s = w { out = s.value } else { out = 42 }
			   u8(out)
			 }`,
			7,
		},
		{
			// The case the whole design exists for: the strong reference died at the
			// helper's exit, the weak one outlived it, and the upgrade reports that
			// rather than handing back a dangling pointer. Reading the strong count
			// here is safe precisely because the weak count kept the header alive.
			"upgrade fails once the referent is gone",
			`struct Node { value: i64 }
			 let makeDead = () -> weak Node => {
			   let n: shared Node = Node { value: 7 }
			   n.weak()
			 }
			 let main = () -> u8 => {
			   let w = makeDead()
			   var out = 0
			   if let s = w { out = s.value } else { out = 42 }
			   u8(out)
			 }`,
			42,
		},
		{
			// An upgrade with no else branch: nothing happens when the referent is gone.
			"upgrade with no else branch",
			`struct Node { value: i64 }
			 let main = () -> u8 => {
			   let n: shared Node = Node { value: 5 }
			   let w = n.weak()
			   var out = 1
			   if let s = w { out = out + s.value }
			   u8(out)
			 }`,
			6,
		},
		{
			// Two upgrades of the same weak reference: each takes and gives back its
			// own strong reference, so the second still succeeds.
			"upgraded twice",
			`struct Node { value: i64 }
			 let main = () -> u8 => {
			   let n: shared Node = Node { value: 3 }
			   let w = n.weak()
			   var out = 0
			   if let a = w { out = out + a.value }
			   if let b = w { out = out + b.value }
			   u8(out)
			 }`,
			6,
		},
		{
			// A weak reference passed into and out of a function: it is an ordinary
			// value, so it flows through parameters and returns like any other.
			"passed through a function",
			`struct Node { value: i64 }
			 let peek = (w: weak Node) -> i64 => {
			   var out = 0
			   if let s = w { out = s.value } else { out = 42 }
			   out
			 }
			 let main = () -> u8 => {
			   let n: shared Node = Node { value: 5 }
			   u8(peek(n.weak()))
			 }`,
			5,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := buildAndRun(t, c.src); got != c.want {
				t.Errorf("%s: exited %d; want %d", c.name, got, c.want)
			}
			if got := buildAndRunASan(t, clang, c.src); got != c.want {
				t.Errorf("%s: ASan run exited %d; want %d", c.name, got, c.want)
			}
		})
	}
}

// A weak reference has a lifecycle of its own: it holds a weak count, so its death
// gives one back. Getting this wrong leaks the box's *memory* — the payload is
// already gone, so nothing observable misbehaves and ASan (which cannot see leaks
// on macOS) stays quiet. The counts are the detector.
func TestEmit_WeakReferenceIsReleased(t *testing.T) {
	t.Parallel()
	src := `struct Node { value: i64 }
	 let makeDead = () -> weak Node => {
	   let n: shared Node = Node { value: 7 }
	   n.weak()
	 }
	 let main = () -> u8 => {
	   let w = makeDead()
	   var out = 0
	   if let s = w { out = s.value } else { out = 42 }
	   u8(out)
	 }`
	ir, err := emitSource(t, src)
	if err != nil {
		t.Fatal(err)
	}
	weakRetains := strings.Count(ir, "call void @lyra_rc_weak_retain")
	weakReleases := strings.Count(ir, "call void @lyra_rc_weak_release")
	if weakRetains != 1 {
		t.Errorf("expected 1 weak retain (the `n.weak()` downgrade), got %d", weakRetains)
	}
	if weakReleases != weakRetains {
		t.Errorf("weak accounting: %d retains != %d releases — the box's memory leaks",
			weakRetains, weakReleases)
	}
	// The upgrade goes through the runtime rather than dereferencing the pointer.
	if !strings.Contains(ir, "call i8* @lyra_rc_upgrade") {
		t.Errorf("expected the upgrade to call lyra_rc_upgrade:\n%s", ir)
	}
}

// An `if let s = w` binding exists only on the path where the upgrade succeeded, so
// its slot is written only there — and its release has to sit on that same path. Put
// it in the enclosing scope instead and the release lands in the *merge* block, which
// the failure path also reaches, reading a slot that was never stored to.
//
// This is checked against the CFG rather than by running the program because running
// it does not reliably fail: releasing an uninitialized alloca is undefined, and
// macOS ASan let it pass locally while Linux CI caught it. The IR, in contrast, says
// so unconditionally. It is the same shape as the `[head, ...tail]` guard bug, whose
// lesson was that a conditional binding needs a branch-scoped frame.
func TestEmit_WeakUpgradeBindingIsBranchScoped(t *testing.T) {
	t.Parallel()
	src := `struct Node { value: i64 }
	 let makeDead = () -> weak Node => {
	   let n: shared Node = Node { value: 7 }
	   n.weak()
	 }
	 let main = () -> u8 => {
	   let w = makeDead()
	   var out = 0
	   if let s = w { out = s.value } else { out = 42 }
	   u8(out)
	 }`
	m := emitModuleForTest(t, src)
	var fn *ir.Func
	for _, f := range m.Funcs {
		if f.GlobalIdent.Name() == "main" {
			fn = f
		}
	}
	if fn == nil {
		t.Fatal("no @main in the emitted module")
	}

	// The block asking whether the referent is alive ends in the two-way branch this
	// test is about: one edge has the strong reference, the other has nothing.
	var head *ir.Block
	var upgraded value.Value
	for _, b := range fn.Blocks {
		for _, inst := range b.Insts {
			if c, ok := inst.(*ir.InstCall); ok && calleeName(c) == "lyra_rc_upgrade" {
				head, upgraded = b, c
			}
		}
	}
	if head == nil {
		t.Fatal("expected @main to call lyra_rc_upgrade")
	}
	cond, ok := head.Term.(*ir.TermCondBr)
	if !ok {
		t.Fatalf("expected the upgrade to be followed by a conditional branch, got %T", head.Term)
	}
	fail, _ := cond.TargetFalse.(*ir.Block)
	if fail == nil {
		t.Fatal("the upgrade's branch has no failure target")
	}

	// The slot the upgraded reference is stored into. Written only on the alive path,
	// so it is exactly the slot the failure path must not touch.
	slot := storeDestinationOf(fn, upgraded)
	if slot == nil {
		t.Skip("the upgraded reference is not stored to a slot — nothing to scope")
	}

	for _, b := range blocksReachableFrom(fail) {
		for _, inst := range b.Insts {
			ld, ok := inst.(*ir.InstLoad)
			if ok && ld.Src == slot {
				t.Fatalf("the upgraded binding's slot is read on the failed-upgrade path "+
					"(block %q), where it was never stored — its release belongs in the "+
					"then-branch, not the enclosing scope:\n%s", b.LocalIdent.Ident(), m.String())
			}
		}
	}
}

// storeDestinationOf returns the slot that `v` (or a bitcast of it) is stored into,
// or nil if it is never stored.
func storeDestinationOf(fn *ir.Func, v value.Value) value.Value {
	aliases := map[value.Value]bool{v: true}
	for _, b := range fn.Blocks {
		for _, inst := range b.Insts {
			if bc, ok := inst.(*ir.InstBitCast); ok && aliases[bc.From] {
				aliases[bc] = true
			}
		}
	}
	for _, b := range fn.Blocks {
		for _, inst := range b.Insts {
			if st, ok := inst.(*ir.InstStore); ok && aliases[st.Src] {
				return st.Dst
			}
		}
	}
	return nil
}

// blocksReachableFrom returns every block reachable from `start`, inclusive.
func blocksReachableFrom(start *ir.Block) []*ir.Block {
	seen := map[*ir.Block]bool{start: true}
	queue := []*ir.Block{start}
	var out []*ir.Block
	for len(queue) > 0 {
		b := queue[0]
		queue = queue[1:]
		out = append(out, b)
		for _, s := range successors(b) {
			if !seen[s] {
				seen[s] = true
				queue = append(queue, s)
			}
		}
	}
	return out
}

// (The two refusals that keep an unsound read unexpressible — a weak reference has
// no fields, and weak() needs a `shared` receiver — are typechecker diagnostics;
// see typechecker/tests/weak_test.go.)
