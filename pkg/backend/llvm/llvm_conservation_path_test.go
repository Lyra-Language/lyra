package llvm

import (
	"strings"
	"testing"

	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/constant"
	lltypes "github.com/llir/llvm/ir/types"

	"github.com/Lyra-Language/lyra/pkg/driver"
)

// emitModuleForTest is emitSource's structural twin: it returns the built
// *ir.Module so a test can walk the CFG instead of grepping the printed form.
func emitModuleForTest(t *testing.T, src string) *ir.Module {
	t.Helper()
	res := driver.Analyze([]byte(src))
	if res.HasErrors() {
		t.Fatalf("unexpected analysis errors: %v", res.Diagnostics)
	}
	ep, diags := driver.ResolveEntryPoint(res)
	if ep == nil {
		t.Fatalf("no entry point: %v", diags)
	}
	m, err := New().emitModule(res, ep)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	return m
}

// assertNoConservationLeak runs the path-sensitive check over a program — and
// asserts the program actually *exercises* it.
//
// The second half matters as much as the first. The analysis matches the runtime
// by name, and while it was doing so with llir's `Ident()` (which prefixes the
// sigil, `@lyra_rc_alloc`) it matched nothing at all: every program tracked zero
// allocations and the whole corpus passed vacuously. A check that silently stops
// checking is worse than no check, so every program here must contribute at least
// one allocation that is genuinely path-checked — allocated, not escaping, and
// therefore subject to the reachability walk.
func assertNoConservationLeak(t *testing.T, src string) {
	t.Helper()
	m := emitModuleForTest(t, src)
	for _, leak := range findConservationLeaks(m) {
		t.Errorf("conservation: %s", leak)
	}
	if tracked := pathCheckedAllocations(m); tracked == 0 {
		t.Errorf("no allocation was path-checked — this program no longer exercises the check")
	}
}

// pathCheckedAllocations counts allocations the analysis actually reasons about
// (those that do not escape), as opposed to merely present.
func pathCheckedAllocations(m *ir.Module) int {
	n := 0
	for _, fn := range m.Funcs {
		for _, block := range fn.Blocks {
			for _, inst := range block.Insts {
				call, ok := inst.(*ir.InstCall)
				if !ok || calleeName(call) != "lyra_rc_alloc" {
					continue
				}
				if !newBoxTracker(fn, call).escaped {
					n++
				}
			}
		}
	}
	return n
}

// TestConservation_Corpus is the durable net: every program here must release
// every allocation on every path out of its function.
//
// The programs are deliberately weighted toward *branching* around managed
// values — that is where a path-sensitive leak can hide and a count-based check
// cannot see it. The `[head, ...tail]` guard leak that motivated this had one
// allocation and one release, perfectly balanced, with the guard-false edge
// carrying the box past its only release.
func TestConservation_Corpus(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
	}{
		{
			// The shape that motivated the check: a guard tested after the tail was
			// allocated, so the guard-false edge must release before falling through.
			"tail binding with a failing guard", `let pick = (xs: []string) -> i64 => match xs {
  [h, ...t] if h == "yes" => 10 + t.len(),
  [h, ...t] if h == "no" => 20 + t.len(),
  [...rest] => 30 + rest.len(),
}
let main = () -> u8 => u8(pick(["other" ++ "", "c" ++ "3"]))`,
		},
		{
			// The tail is read locally rather than handed onward, so it stays inside
			// the analysis. (The recursive form passes the tail to a call, which the
			// escape rule conservatively treats as a possible ownership transfer —
			// correct, but then there is no path claim left to check.)
			"tail binding, unguarded", `let firstAndRest = (xs: []i64) -> i64 => match xs {
  [] => 0,
  [h, ...t] => h + t.len(),
}
let main = () -> u8 => u8(firstAndRest([1, 2, 3, 4]))`,
		},
		{
			// A concatenation consumed in a branching condition.
			"concat inside a branch", `let main = () -> u8 => {
  let a = "x" ++ "y"
  if a == "xy" { 0 } else { 1 }
}`,
		},
		{
			// Both arms of an if produce a fresh box; the merged value is bound locally,
			// so it must be released once on the way out. (Returning it instead would
			// make it escape — legitimate, but then the program proves nothing here.)
			"if merges two fresh boxes", `let pick = (f: bool) -> i64 => {
  let s = if f { "a" ++ "1" } else { "b" ++ "2" }
  if s == "a1" { 0 } else { 1 }
}
let main = () -> u8 => u8(pick(true))`,
		},
		{
			// A loop allocating each iteration — the release must be inside the loop,
			// or the box leaks once per turn. The allocation is now written directly in
			// the body; it lived in a helper while a `let` declared in a loop body was
			// invisible there (fixed by making the loop bodies pointers).
			"allocation in a loop", `let main = () -> u8 => {
  var n = 0
  for var i = 0; i < 5; i += 1 {
    let s = "a" ++ "b"
    if s == "ab" { n = n + 1 } else { n = n + 2 }
  }
  u8(n)
}`,
		},
		{
			// An early return past a live box.
			"early return with a live box", `let f = (flag: bool) -> i64 => {
  let s = "a" ++ "b"
  if flag { return 1 }
  if s == "ab" { 2 } else { 3 }
}
let main = () -> u8 => u8(f(true))`,
		},
		{
			// A break leaving a scope that holds a box: the early exit must release it.
			"break past a live box", `let f = (limit: i64) -> i64 => {
  let s = "a" ++ "b"
  var n = 0
  for var i = 0; i < limit; i += 1 {
    if s == "ab" { return n }
    n = n + 1
  }
  n
}
let main = () -> u8 => u8(f(5))`,
		},
		{
			// A match on a string scrutinee that is itself a fresh box.
			"match on a fresh string", `let main = () -> u8 => {
  match "a" ++ "b" {
    "ab" => 0,
    _ => 1,
  }
}`,
		},
		{
			// Interpolation allocates one box per evaluation, inside a branch.
			"interpolation in a branch", `let main = () -> u8 => {
  let n = 7
  let s = if n > 3 { "big ${n}" } else { "small ${n}" }
  if s == "big 7" { 0 } else { 1 }
}`,
		},
		{
			// A dynamic array of managed elements, matched and indexed.
			"managed dynamic array", `let main = () -> u8 => {
  let xs: []string = ["a" ++ "1", "b" ++ "2"]
  match xs {
    [a, b] => { if a == "a1" { 0 } else { 1 } },
    _ => 1,
  }
}`,
		},
		{
			// A closure environment is an allocation like any other, and a branch is
			// where one can be dropped: whichever closure is built, its environment
			// must be released on the way out.
			//
			// The closures are deliberately never *called* — an indirect call hands
			// the environment to a call instruction, which the escape rule stops
			// reasoning about (correctly, since a callee may take ownership), leaving
			// no path claim to check. What is pinned here is creation and release,
			// which is exactly the half a branch can get wrong.
			"closure environment in a branch", `let main = () -> u8 => {
  let n = 5
  let f = if n > 3 { (x: i64) -> i64 => x + n } else { (x: i64) -> i64 => x - n }
  0
}`,
		},
		{
			// A capturing closure created once per iteration: the environment must be
			// released inside the loop, or it leaks once per turn.
			"closure created in a loop", `let main = () -> u8 => {
  var total = 0
  for var i = 0; i < 3; i += 1 {
    let f = (x: i64) -> i64 => x + i
    total = total + 1
  }
  u8(total)
}`,
		},
		{
			// A newtype over a managed base, branched on. The wrapper is nominal only,
			// so the box behind it must be released on both edges exactly as a bare
			// string's is — a name is not a place to lose a reference.
			"newtype over string in a branch", `newtype Email = string
let main = () -> u8 => {
  let e: Email = "a" ++ "b"
  let s: string = e
  if s == "ab" { 0 } else { 1 }
}`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			assertNoConservationLeak(t, c.src)
		})
	}
}

// TestConservation_DetectsAPathThatSkipsARelease is the check on the check: an
// assertion that never fires is worthless, so this proves the analysis reports
// the shape it exists for.
//
// It reproduces the guard leak structurally — an allocation, a conditional, and a
// release on only one of the two edges — by building the IR directly, since the
// compiler no longer emits that shape (the bug is fixed).
func TestConservation_DetectsAPathThatSkipsARelease(t *testing.T) {
	t.Parallel()
	m := ir.NewModule()
	rcAlloc := m.NewFunc("lyra_rc_alloc", lltypes.NewPointer(lltypes.I8), ir.NewParam("size", lltypes.I64))
	rcRelease := m.NewFunc("lyra_rc_release", lltypes.Void, ir.NewParam("box", lltypes.NewPointer(lltypes.I8)), ir.NewParam("drop", lltypes.NewPointer(lltypes.I8)))
	fn := m.NewFunc("leaky", lltypes.I64, ir.NewParam("flag", lltypes.I1))
	entry := fn.NewBlock("entry")
	released := fn.NewBlock("released")
	skipped := fn.NewBlock("skipped")
	box := entry.NewCall(rcAlloc, i64c(24))
	entry.NewCondBr(fn.Params[0], released, skipped)
	released.NewCall(rcRelease, box, constant.NewNull(lltypes.NewPointer(lltypes.I8)))
	released.NewRet(i64c(1))
	skipped.NewRet(i64c(0)) // the leak: this edge never releases

	leaks := findConservationLeaks(m)
	if len(leaks) != 1 {
		t.Fatalf("expected exactly 1 leak, got %d: %v", len(leaks), leaks)
	}
	if got := leaks[0].String(); !strings.Contains(got, "leaky") || !strings.Contains(got, "skipped") {
		t.Errorf("leak = %q; want it to name @leaky's `skipped` exit", got)
	}
}

// The same function with the release on both edges is clean — pinning that the
// check keys on the *path*, not on the mere presence of a release.
func TestConservation_AcceptsReleaseOnEveryPath(t *testing.T) {
	t.Parallel()
	m := ir.NewModule()
	rcAlloc := m.NewFunc("lyra_rc_alloc", lltypes.NewPointer(lltypes.I8), ir.NewParam("size", lltypes.I64))
	rcRelease := m.NewFunc("lyra_rc_release", lltypes.Void, ir.NewParam("box", lltypes.NewPointer(lltypes.I8)), ir.NewParam("drop", lltypes.NewPointer(lltypes.I8)))
	fn := m.NewFunc("clean", lltypes.I64, ir.NewParam("flag", lltypes.I1))
	entry := fn.NewBlock("entry")
	a := fn.NewBlock("a")
	b := fn.NewBlock("b")
	box := entry.NewCall(rcAlloc, i64c(24))
	entry.NewCondBr(fn.Params[0], a, b)
	a.NewCall(rcRelease, box, constant.NewNull(lltypes.NewPointer(lltypes.I8)))
	a.NewRet(i64c(1))
	b.NewCall(rcRelease, box, constant.NewNull(lltypes.NewPointer(lltypes.I8)))
	b.NewRet(i64c(0))

	if leaks := findConservationLeaks(m); len(leaks) != 0 {
		t.Errorf("expected no leaks, got %v", leaks)
	}
}

// A returned box is the caller's problem, not a leak — the escape rule that keeps
// the check quiet on every allocating helper (`++`, array construction, …).
// Deliberately *not* using assertNoConservationLeak: this program's whole point is
// that its allocation escapes, so demanding a path-checked allocation (which that
// helper does) would contradict what is being tested.
func TestConservation_ReturnedBoxIsNotALeak(t *testing.T) {
	t.Parallel()
	m := emitModuleForTest(t, `let make = () -> string => "a" ++ "b"
let main = () -> u8 => { if make() == "ab" { 0 } else { 1 } }`)
	if leaks := findConservationLeaks(m); len(leaks) != 0 {
		t.Errorf("a returned box was reported as a leak: %v", leaks)
	}
	// @make's allocation must be the escaping one — if it were path-checked, the
	// analysis would be claiming something about a box it hands to its caller.
	for _, fn := range m.Funcs {
		if fn.GlobalIdent.Name() != "make" {
			continue
		}
		for _, block := range fn.Blocks {
			for _, inst := range block.Insts {
				call, ok := inst.(*ir.InstCall)
				if !ok || calleeName(call) != "lyra_rc_alloc" {
					continue
				}
				if !newBoxTracker(fn, call).escaped {
					t.Errorf("@make's returned allocation should escape, not be path-checked")
				}
			}
		}
	}
}
