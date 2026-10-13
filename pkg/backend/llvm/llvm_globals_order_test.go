package llvm

import "testing"

// A module's unannotated `var` below the functions reading and writing it — the order the
// house style asks for — is typed above its declaration by its literal. It was not typed at
// all there, and what stood beside the read went untyped with it: `shown || buttons != 0`
// left the `0` beside a `u16` at the backend's default width ("comparison of mismatched
// integer widths", i16 vs i64), and `"${shown}"` an interpolation segment with no type
// (10/13, std.game's on-screen pad).
func TestExec_AnUnannotatedModuleVarIsTypedAboveItsDeclaration(t *testing.T) {
	t.Parallel()
	src := `let touch = (buttons: u16) -> void => {
  shown = shown || buttons != 0
  count = count + 1
}

let main = () -> u8 => {
  touch(3)
  if "${shown}" != "true" { return 1 }
  if count != 2 { return 2 }
  if "${name}!" != "hero!" { return 3 }
  0
}

var shown = false
var count = 1
let name = "hero"
`
	if got := exitCode(t, command(preludeBinary(t, src)).Run()); got != 0 {
		t.Errorf("case %d failed", got)
	}
}
