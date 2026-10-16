package llvm

import "testing"

// A field of a struct with storage is read in place (lowerMemberExpr's structInPlace): from
// a local, a module's `var`, a `mut` parameter (the caller's storage), through fields and
// through fixed arrays' elements — each must read what was last written there, as the
// whole-struct read did.
func TestExec_FieldsReadInPlace(t *testing.T) {
	t.Parallel()
	got := buildAndRunWithPrelude(t, `
module main
struct Voice { note: i16, level: i16 }
struct Player { voices: [3]Voice, keys: u8, inner: Voice }
var player = Player { voices: #[Voice { note: 1, level: 2 }; 3], keys: 4, inner: Voice { note: 5, level: 6 } }
let bump = (p: mut Player, k: i64) -> i16 => {
  p.voices[k].note += 10
  p.inner.level += 1
  p.voices[k].note + p.inner.level
}
let main = () -> void => {
  var local = Player { voices: #[Voice { note: 7, level: 8 }; 3], keys: 9, inner: Voice { note: 10, level: 11 } }
  local.voices[2].level = 30
  var k = 0
  while k < 3 {
    print("${local.voices[k].level} ")
    k += 1
  }
  println("${local.keys} ${local.inner.note}")
  player.voices[1].note = 40
  println("${player.voices[1].note} ${player.voices[0].note} ${player.inner.level}")
  println("${bump(player, 1)} ${player.voices[1].note} ${player.inner.level}")
}
`, "")
	want := "8 8 30 9 10\n40 1 6\n57 50 7\n"
	if got != want {
		t.Errorf("got %q; want %q", got, want)
	}
}

// A field read through an element out of range traps, as the element read does: the
// in-place read asks the same bounds check (elementPtr).
func TestExec_AFieldOfAnElementOutOfRangeTraps(t *testing.T) {
	t.Parallel()
	assertTraps(t, `
module main
struct Voice { note: i16, level: i16 }
let main = () -> void => {
  let voices: [3]Voice = #[Voice { note: 1, level: 2 }; 3]
  var i: i64 = 0
  for _ in 0..<5 { i += 1 }
  println("${voices[i].level}")
}
`, "array index out of bounds")
}
