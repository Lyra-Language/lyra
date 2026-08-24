package llvm

import (
	"os/exec"
	"strings"
	"testing"
)

// Ownership inside `unsafe { … }`, and the rule that had to land first.
//
// The two are one change. Ownership's expression walk had no arm for UnsafeBlockExpr, so
// nothing — no retain, drop or transfer — was recorded anywhere inside an unsafe block,
// which is the whole of a program's FFI and raw-pointer code. That was conservative rather
// than unsound: every drop deferred to scope exit, so it leaked rather than dangled.
//
// Adding the obvious arm broke every FFI test, because Perceus rests on a premise `&`
// breaks. Last-use says a binding's final *textual mention* is its final *use*; taking an
// address separates the two, since a raw pointer keeps storage alive without counting as a
// reference to it. `noteAddressTaken` pins an address-taken binding against last-use, and
// only then can the block be walked.

// **The pinning rule, at its motivating shape.** `&xs[0]` is the last *mention* of `xs`,
// and the pointer outlives it — so `xs` must not be freed there.
//
// The read has to happen in a **later statement** than the one that took the address, which
// is the detail a first version of this test got wrong: last-use drops are emitted after the
// statement they belong to, so a program that reads through the pointer inside that same
// statement is unaffected either way and passes with or without the rule. An allocation in
// between makes the reuse visible rather than merely possible.
//
// Without noteAddressTaken this prints 0. It is the shape `std.ffi`'s own CBuffer has —
// `CBuffer { ptr: unsafe { &xs[0] }, len: 3 }`, read on a later line — which is why adding
// the UnsafeBlockExpr arm alone broke every FFI test.
func TestExec_AddressTakenBindingSurvivesItsLastMention(t *testing.T) {
	t.Parallel()
	out := buildAndRunWithPrelude(t, `
module main
let main = () -> void => {
  var xs: []u8 = [65, 66, 67]
  let p = unsafe { &xs[0] }
  var filler: []u8 = [9, 9, 9, 9, 9, 9, 9, 9]
  println(filler[0])
  unsafe { println(p^) }
}
`, "")
	if got := strings.TrimSpace(out); got != "9\n65" {
		t.Errorf("read through an address-taken binding = %q; want \"9\\n65\" — "+
			"a 0 means the array was freed at its last mention while the pointer still named it", got)
	}
}

// The same rule under ASan, across the three places an address is taken: an element, a whole
// binding, and an element of an array that owns a managed value.
func TestExec_AddressTakenBindingASan(t *testing.T) {
	t.Parallel()
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Skip("clang not found on PATH; skipping ASan test")
	}
	if !asanAvailable(t, clang) {
		t.Skip("ASan runtime not available; skipping")
	}
	for _, src := range []string{
		`let main = () -> u8 => {
  var xs: []u8 = [65, 66, 67]
  var total: u8 = 0
  unsafe {
    let p = &xs[0]
    total = p^ + p.offset(2)^
  }
  total - 132
}
`,
		`let main = () -> u8 => {
  var n: i64 = 7
  var out: i64 = 0
  unsafe {
    let p = &n
    out = p^
  }
  u8(out - 7)
}
`,
		`let main = () -> u8 => {
  let s: string = "a" ++ "b"
  var xs: []string = [s]
  var n: i64 = 0
  unsafe {
    let p = &xs[0]
    n = p^.len()
  }
  u8(n - 2)
}
`,
	} {
		if code := buildAndRunASan(t, clang, src); code != 0 {
			t.Errorf("ASan run: exit %d for %q", code, src)
		}
	}
}

// **The arm the pin unlocked**, and the fault it closes.
//
// A managed value read inside an unsafe block *in an owning position* — here `s`, initializing
// `t` — needs a retain, because two bindings then name one box. With no arm nothing was
// recorded anywhere inside an unsafe block, so no retain was minted and both releases ran on
// one box: this program crashes without it, printing nothing.
//
// That is worth stating precisely, because the hole reads as merely conservative and mostly
// is: a missing *drop* defers to scope exit, where the managed frame is a leak-safe backstop.
// A missing *retain* has no backstop. It is why counting emitted releases cannot see this —
// the counts match either way — and why the whole test suite passed with the arm removed
// until this case existed.
func TestExec_UnsafeBlockRetainsWhatItShares(t *testing.T) {
	t.Parallel()
	out := buildAndRunWithPrelude(t, `
module main
let main = () -> void => {
  let s: string = "a" ++ "b"
  let t: string = unsafe { s }
  println(s)
  println(t)
}
`, "")
	if got := strings.TrimSpace(out); got != "ab\nab" {
		t.Errorf("sharing a managed value out of an unsafe block = %q; want \"ab\\nab\" — "+
			"no output means the box was released twice", got)
	}
}

// The same shape under ASan, which names the fault as a double free rather than leaving it
// as a missing line of output.
func TestExec_UnsafeBlockOwnershipASan(t *testing.T) {
	t.Parallel()
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Skip("clang not found on PATH; skipping ASan test")
	}
	if !asanAvailable(t, clang) {
		t.Skip("ASan runtime not available; skipping")
	}
	for _, src := range []string{
		// Shared out of the block into another binding: needs a retain.
		`let main = () -> u8 => {
  let s: string = "a" ++ "b"
  let t: string = unsafe { s }
  u8(s.len() + t.len() - 4)
}
`,
		// Built and consumed entirely inside the block: needs a drop.
		`let main = () -> u8 => {
  var n: i64 = 0
  unsafe {
    let s: string = "x" ++ "y"
    n = s.len()
  }
  u8(n - 2)
}
`,
	} {
		if code := buildAndRunASan(t, clang, src); code != 0 {
			t.Errorf("ASan run: exit %d for %q", code, src)
		}
	}
}

// The block's tail is its value, which is why it is analyzed as a block rather than walked
// as a list of statements: an owning tail has to transfer to whatever binds it.
func TestExec_UnsafeBlockTailIsItsValue(t *testing.T) {
	t.Parallel()
	out := buildAndRunWithPrelude(t, `
module main
let main = () -> void => {
  let s: string = unsafe { "he" ++ "llo" }
  println(s)
  var n: i64 = 3
  let doubled = unsafe { n * 2 }
  println(doubled)
}
`, "")
	if got := strings.TrimSpace(out); got != "hello\n6" {
		t.Errorf("unsafe block as a value = %q; want \"hello\\n6\"", got)
	}
}
