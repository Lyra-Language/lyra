package ownership_test

import (
	"testing"

	"github.com/Lyra-Language/lyra/pkg/driver"
)

type counts struct {
	retains, temps, transfers, drops int
}

// analyze runs the front-end and returns the ownership annotation counts, failing
// on any analysis error so a test only ever sees a well-typed program.
func analyze(t *testing.T, src string) counts {
	t.Helper()
	res := driver.Analyze([]byte(src))
	if res.HasErrors() {
		t.Fatalf("unexpected analysis errors: %v", res.Diagnostics)
	}
	if res.Ownership == nil {
		t.Fatal("no ownership table")
	}
	o := res.Ownership
	return counts{len(o.Retain), len(o.ReleaseTemp), len(o.LastUseTransfer), len(o.LastUseDrop)}
}

// A copy of a managed binding is its last use, so it *transfers* (Perceus — no
// dup); the copy `b` is then dropped at its own last use (the comparison).
func TestOwnership_CopyRetains(t *testing.T) {
	c := analyze(t, `let main = () -> u8 => {
	   let a: string = "x" ++ "y"
	   let b: string = a
	   if b == "xy" { 1 } else { 0 }
	 }`)
	if c.retains != 0 {
		t.Errorf("copy: want 0 retains (last use transfers), got %d", c.retains)
	}
	if c.transfers != 1 {
		t.Errorf("copy: want 1 transfer (let b = a), got %d", c.transfers)
	}
	if c.drops != 1 {
		t.Errorf("copy: want 1 last-use drop (b == ...), got %d", c.drops)
	}
}

// A concat bound to a `let` and compared once: the concat transfers into the
// binding (no retain/temp), and the binding is dropped at its last use.
func TestOwnership_ConcatBindingTransfers(t *testing.T) {
	c := analyze(t, `let main = () -> u8 => {
	   let a: string = "x" ++ "y"
	   if a == "xy" { 1 } else { 0 }
	 }`)
	if c.retains != 0 || c.temps != 0 {
		t.Errorf("concat binding: want 0/0 retain/temp, got %d/%d", c.retains, c.temps)
	}
	if c.drops != 1 {
		t.Errorf("concat binding: want 1 last-use drop, got %d", c.drops)
	}
}

// A concat consumed directly by a comparison (never bound) is an owned temporary
// released after the statement.
func TestOwnership_ConcatTemporaryReleased(t *testing.T) {
	c := analyze(t, `let main = () -> u8 => if ("x" ++ "y") == "xy" { 1 } else { 0 }`)
	if c.retains != 0 {
		t.Errorf("temp: want 0 retains, got %d", c.retains)
	}
	if c.temps != 1 {
		t.Errorf("temp: want 1 temp-release, got %d", c.temps)
	}
}

// Returning a local from an owned-return function is its last use, so it
// transfers to the caller (Perceus — no dup).
func TestOwnership_ReturnLocalRetains(t *testing.T) {
	c := analyze(t, `let make = () -> string => {
	   let s: string = "a" ++ "b"
	   s
	 }
	 let main = () -> u8 => if make() == "ab" { 1 } else { 0 }`)
	if c.retains != 0 {
		t.Errorf("return local: want 0 retains (last use transfers), got %d", c.retains)
	}
	if c.transfers != 1 {
		t.Errorf("return local: want 1 transfer (tail `s`), got %d", c.transfers)
	}
}

// A borrow (`ref`/bare) parameter isn't owned by the callee; passing an owned
// argument to it leaves the temporary to the caller to release. An `own`
// parameter takes it (transfer, no caller release).
func TestOwnership_OwnParamTransfers(t *testing.T) {
	// bare param: the argument temporary is released by the caller.
	bare := analyze(t, `let f = (s: string) -> u8 => if s == "hi" { 1 } else { 0 }
	 let main = () -> u8 => f("h" ++ "i")`)
	if bare.temps != 1 {
		t.Errorf("bare param: want 1 temp-release (caller frees), got %d", bare.temps)
	}
	// own param: ownership transfers to the callee, so the caller does not release.
	own := analyze(t, `let f = (s: own string) -> u8 => if s == "hi" { 1 } else { 0 }
	 let main = () -> u8 => f("h" ++ "i")`)
	if own.temps != 0 {
		t.Errorf("own param: want 0 temp-releases (callee frees), got %d", own.temps)
	}
}

// A binding used once as a borrow, then again later, drops at its true last use.
func TestOwnership_LastUseDropAfterFinalBorrow(t *testing.T) {
	c := analyze(t, `let eq = (a: string, b: string) -> u8 => if a == b { 1 } else { 0 }
	 let main = () -> u8 => {
	   let s: string = "a" ++ "b"
	   let x: u8 = eq(s, "ab")
	   if x == 1 && s == "ab" { 1 } else { 0 }
	 }`)
	// s's two borrow uses (eq(s,..), s == "ab"); the later one is the last → 1 drop,
	// no retain (borrows don't dup), no transfer (last use is a borrow).
	if c.drops != 1 || c.retains != 0 || c.transfers != 0 {
		t.Errorf("last-use drop: want drops=1 retains=0 transfers=0, got drops=%d retains=%d transfers=%d",
			c.drops, c.retains, c.transfers)
	}
}

// reuseInfo pulls the reuse-analysis results out of the ownership table for a
// program (Perceus stage 3): how many matches reclaim their scrutinee's box, how
// many constructions consume a reuse token, and how many arm-binding references
// transfer (move, no dup).
func reuseInfo(t *testing.T, src string) (matches, targets, transfers int) {
	t.Helper()
	res := driver.Analyze([]byte(src))
	if res.HasErrors() {
		t.Fatalf("unexpected analysis errors: %v", res.Diagnostics)
	}
	o := res.Ownership
	return len(o.ReuseMatch), len(o.ReuseTarget), len(o.LastUseTransfer)
}

// A recursive map over a `shared` list: each `own`-param match is a reuse source,
// both its arms construct the same type (two reuse targets), and the tail binding
// `t` transfers into the recursive call (keeping the reused cell unique).
func TestReuse_RecursiveMapFires(t *testing.T) {
	matches, targets, transfers := reuseInfo(t, `data List = Nil | Cons(i64, shared List)
	 let inc = (xs: own shared List) -> shared List => match xs {
	   Nil => Nil,
	   Cons(h, t) => Cons(h + 1, inc(t))
	 }
	 let main = () -> u8 => {
	   let xs: shared List = Cons(1, Nil)
	   let ys: shared List = inc(xs)
	   0
	 }`)
	if matches != 1 {
		t.Errorf("want 1 reuse match (inc's match), got %d", matches)
	}
	if targets != 2 {
		t.Errorf("want 2 reuse targets (Nil and Cons arms), got %d", targets)
	}
	// The tail `t` transfers into inc(t); `xs` transfers into the inc(xs) call site.
	if transfers < 1 {
		t.Errorf("want the arm binding `t` to transfer (>=1), got %d", transfers)
	}
}

// A borrowed scrutinee (a bare `shared` param) is NOT consumed, so its box must
// not be reclaimed — no reuse match, no arm-binding transfer.
func TestReuse_BorrowedScrutineeNotReused(t *testing.T) {
	matches, targets, _ := reuseInfo(t, `data List = Nil | Cons(i64, shared List)
	 let rebuild = (xs: shared List) -> shared List => match xs {
	   Nil => Nil,
	   Cons(h, t) => Cons(h, t)
	 }
	 let main = () -> u8 => {
	   let xs: shared List = Cons(1, Nil)
	   let ys: shared List = rebuild(xs)
	   0
	 }`)
	if matches != 0 {
		t.Errorf("borrowed scrutinee: want 0 reuse matches, got %d", matches)
	}
	if targets != 0 {
		t.Errorf("borrowed scrutinee: want 0 reuse targets, got %d", targets)
	}
}

// A guarded match can't lower via the reuse-wired tag switch, so reuse must not be
// claimed for it (else the suppressed scrutinee drop would leak on the ladder path).
func TestReuse_GuardedMatchNotReused(t *testing.T) {
	matches, _, _ := reuseInfo(t, `data Box = Empty | Full(i64)
	 let f = (b: own shared Box) -> shared Box => match b {
	   Full(n) if n > 0 => Full(n),
	   Full(n) => Empty,
	   Empty => Empty
	 }
	 let main = () -> u8 => {
	   let b: shared Box = Full(1)
	   let c: shared Box = f(b)
	   0
	 }`)
	if matches != 0 {
		t.Errorf("guarded match: want 0 reuse matches, got %d", matches)
	}
}
