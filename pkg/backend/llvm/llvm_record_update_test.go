package llvm

import "testing"

// `P { base | f: v }` — a copy of `base` with the listed fields replaced — and its anonymous
// twin `{ base | f: v }`. Both type-checked and the backend refused them until 09/13 ("struct
// record-update syntax not implemented yet"); the anonymous one was not even checked, its
// result being the updates alone.
//
// Written with a `module` header, since the headerless snippet is what hid this feature's
// other spelling. Run under ASan, with leaks reported on Linux: a kept managed field is a copy
// with a retain of its own, and a base whose last use is the update is released after it.
func TestExec_RecordUpdate(t *testing.T) {
	t.Parallel()
	src := `module main
struct Player { name: string, health: i64, tags: []string }
let hurt = (p: Player, by: i64) -> Player => Player { p | health: p.health - by }
let main = () -> u8 => {
  let base = Player { name: "a" ++ "b", health: 10, tags: ["x" ++ "y"] }
  let h = hurt(base, 7)
  let renamed = Player { h | name: "c" ++ "d", tags: [] }
  let shared_p: shared Player = Player { name: "s" ++ "!", health: 1, tags: ["t" ++ "u"] }
  let copy = Player { shared_p | health: 2 }
  let only = Player { name: "o" ++ "k", health: 5, tags: [] }
  let last = Player { only | health: 6 }
  let anon = { x: 1, label: "q" ++ "r" }
  let anon2 = { anon | x: 9 }
  var score = 0
  if base.name == "ab" && base.health == 10 && base.tags[0] == "xy" { score += 1 }
  if h.name == "ab" && h.health == 3 && h.tags.len() == 1 { score += 2 }
  if renamed.name == "cd" && renamed.health == 3 && renamed.tags.len() == 0 { score += 4 }
  if copy.name == "s!" && copy.health == 2 && shared_p.health == 1 && copy.tags[0] == "tu" { score += 8 }
  if last.name == "ok" && last.health == 6 { score += 16 }
  if anon2.x == 9 && anon2.label == "qr" && anon.x == 1 { score += 32 }
  u8(score)
}`
	if got := buildAndRunASanWithPrelude(t, src); got != 63 {
		t.Errorf("exited %d; want 63 (each bit is one form that must hold)", got)
	}
}
