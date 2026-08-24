package llvm

import (
	"strings"
	"testing"
)

// Two match arms naming one constructor.
//
// The second is unreachable — the first binds the payload unconditionally, so it matches
// that tag every time — and `match` is first-match-wins, so dropping it is the language's
// own semantics rather than a choice the backend makes.
//
// It used to emit both, producing two `i8 0` cases in one LLVM `switch`. llir builds that
// without complaint and **clang refuses it**: "duplicate case value in switch". So the
// failure arrives as a compile error against generated IR, quoting a line number in a .ll
// file, on a program `lyrac check` passed clean — the front end does not report an
// unreachable match arm yet (todo.md), so nothing upstream refuses it either.
//
// Dropping the arm is only sound because of where this path sits: a payload test or a guard
// routes the match to the ladder instead (`dataMatchHasPayloadTest || matchHasGuard`), so
// every arm reaching the switch is a bind-only pattern. The two tests below the first are
// what pin that — they share a tag *and* must keep both arms, and they go down the other
// path to do it.
func TestExec_DuplicateConstructorArmIsDropped(t *testing.T) {
	t.Parallel()
	out, code := buildAndRunCapture(t, `
data D = Wrap(i64) | Nil
let main = () -> void => {
  let d: D = Wrap(7)
  println(match d {
    Wrap(a) => a,
    Wrap(b) => b + 100,
    Nil => 0
  })
}
`)
	if code != 0 || strings.TrimSpace(out) != "7" {
		t.Errorf("exit %d, output %q; want exit 0 and \"7\" — the first arm wins and the "+
			"second must not reach the switch as a second case for tag 0", code, strings.TrimSpace(out))
	}
}

// A literal subpattern sharing a tag with a binding arm. The first arm is **refutable**, so
// the second is genuinely reachable and dropping it would be a miscompile. It takes the
// ladder rather than the switch, which is what keeps the dedupe above sound.
func TestExec_LiteralSubpatternSharingATagKeepsBothArms(t *testing.T) {
	t.Parallel()
	out, code := buildAndRunCapture(t, `
data D = Wrap(i64) | Nil
let main = () -> void => {
  println(match Wrap(7) { Wrap(0) => 100, Wrap(b) => b, Nil => 0 })
  println(match Wrap(0) { Wrap(0) => 100, Wrap(b) => b, Nil => 0 })
}
`)
	if code != 0 || strings.TrimSpace(out) != "7\n100" {
		t.Errorf("exit %d, output %q; want \"7\\n100\" — a refutable first arm must not "+
			"suppress the later arm for the same constructor", code, strings.TrimSpace(out))
	}
}

// The same, with a guard doing the refuting.
func TestExec_GuardedArmSharingATagKeepsBothArms(t *testing.T) {
	t.Parallel()
	out, code := buildAndRunCapture(t, `
data D = Wrap(i64) | Nil
let main = () -> void => {
  println(match Wrap(7) { Wrap(a) if a > 100 => 1, Wrap(b) => b, Nil => 0 })
  println(match Wrap(500) { Wrap(a) if a > 100 => 1, Wrap(b) => b, Nil => 0 })
}
`)
	if code != 0 || strings.TrimSpace(out) != "7\n1" {
		t.Errorf("exit %d, output %q; want \"7\\n1\" — a guarded first arm must not "+
			"suppress the later arm for the same constructor", code, strings.TrimSpace(out))
	}
}
