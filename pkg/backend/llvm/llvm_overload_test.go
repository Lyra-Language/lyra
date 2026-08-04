package llvm

import (
	"strings"
	"testing"
)

// Receiver-keyed overloading, lowered.
//
// The front end picks the member; the backend's job is to emit each one under a symbol of
// its own and to send a call to the one that was picked. Both halves fail *silently* if
// they are wrong — a shared symbol is a clang redefinition error at best and the wrong
// body at worst, and a call resolved by name would reach whichever member was declared
// last — so these run the program and check the value, rather than reading the IR.

const overloadedUnwrap = `
data Maybe<t> = None | Some t
data Result<t, e> = Ok(t) | Err(e)

let unwrap_or<t> = (self: Maybe<t>, fallback: t) -> t => match self {
  Some v => v,
  None => fallback,
}

let unwrap_or<t, e> = (self: Result<t, e>, fallback: t) -> t => match self {
  Ok v => v,
  Err _ => fallback,
}
`

// Each overload is reached from a receiver of its own type, and each returns a different
// value, so picking the wrong member changes the exit code rather than merely the path.
// 7 from the Maybe, 9 from the Result.
func TestExec_OverloadDispatchesOnReceiver(t *testing.T) {
	t.Parallel()
	got := buildAndRun(t, overloadedUnwrap+`
let main = () -> u8 => {
  let m: Maybe<i64> = Some 7
  let r: Result<i64, string> = Ok 9
  u8(m.unwrap_or(0) + r.unwrap_or(0))
}
`)
	if got != 16 {
		t.Errorf("expected 16 (7 from the Maybe overload + 9 from the Result one), got %d", got)
	}
}

// The fallback arms, which run the *other* branch of each member's body — a member picked
// correctly for the `Some` case could still be wrong for `None` if the two bodies were
// somehow sharing an emitted function. 3 + 4.
func TestExec_OverloadFallbackArms(t *testing.T) {
	t.Parallel()
	got := buildAndRun(t, overloadedUnwrap+`
let main = () -> u8 => {
  let m: Maybe<i64> = None
  let r: Result<i64, string> = Err "no"
  u8(m.unwrap_or(3) + r.unwrap_or(4))
}
`)
	if got != 7 {
		t.Errorf("expected 7 from the two fallbacks, got %d", got)
	}
}

// Written as plain calls rather than method-style. The receiver is argument 0 either way,
// so this must lower to exactly the same two functions.
func TestExec_OverloadCallForm(t *testing.T) {
	t.Parallel()
	got := buildAndRun(t, overloadedUnwrap+`
let main = () -> u8 => {
  let m: Maybe<i64> = Some 5
  let r: Result<i64, string> = Ok 6
  u8(unwrap_or(m, 0) + unwrap_or(r, 0))
}
`)
	if got != 11 {
		t.Errorf("expected 11, got %d", got)
	}
}

// Non-generic overloads, which are emitted as themselves rather than as specializations —
// the path where two members would otherwise take the *same* module-qualified symbol.
// The distinct `$head` suffix is what keeps them apart, so this pins the symbols too.
func TestExec_OverloadDistinctSymbols(t *testing.T) {
	t.Parallel()
	src := `
struct Cat { n: i64 }
struct Dog { n: i64 }

let speak = (self: Cat) -> i64 => 1
let speak = (self: Dog) -> i64 => 2

let main = () -> u8 => {
  let c = Cat { n: 0 }
  let d = Dog { n: 0 }
  u8(c.speak() * 10 + d.speak())
}
`
	if got := buildAndRun(t, src); got != 12 {
		t.Errorf("expected 12 (Cat=1 then Dog=2), got %d", got)
	}
	ir, err := emitSource(t, src)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	for _, want := range []string{"@lyra.speak$Cat", "@lyra.speak$Dog"} {
		if !strings.Contains(ir, want) {
			t.Errorf("expected the emitted module to define %s\n%s", want, ir)
		}
	}
}
