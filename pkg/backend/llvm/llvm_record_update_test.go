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

// **A record update's base may be any postfix expression** — a call, a field, an index —
// not only a name (09/19). Until then every builder bound a throwaway local first, and
// `std.temporal` did it eleven times in one file.
//
// The danger is not the parse but the **lifetime**: a named base outlives the update, while
// a call's result is a temporary of the statement doing the updating. The update copies the
// base's managed fields with a retain of their own and the temporary is released at the end
// of the statement, so the copy must survive it — and must not be released twice. Under ASan
// a double free aborts and LeakSanitizer catches the opposite, so both directions fail
// loudly rather than quietly (./asan.sh).
//
// Each base is used *twice* where that is what makes the case: a second read of a temporary
// already consumed is where a release placed one statement too early shows up.
func TestExec_RecordUpdateBaseIsAnyPostfixExpr(t *testing.T) {
	t.Parallel()
	src := `module main
struct Tag { name: string, n: i64, xs: []string }
struct Holder { tag: Tag, all: []Tag }
let make = (s: string, n: i64) -> Tag => Tag { name: s ++ "!", n: n, xs: [s ++ "?"] }
let holder = () -> Holder => Holder { tag: make("h", 1), all: [make("a", 2), make("b", 3)] }
let main = () -> u8 => {
  var score = 0
  // A call as the base, twice over, and once nested inside the other's field list.
  let from_call = Tag { make("c", 1) | n: 7 }
  let again = Tag { make("c", 1) | n: 8, xs: make("d", 0).xs }
  if from_call.name == "c!" && from_call.n == 7 && from_call.xs[0] == "c?" { score += 1 }
  if again.n == 8 && again.xs[0] == "d?" && again.name == "c!" { score += 2 }
  // A field of a call's result, and an index into one — the temporary is the whole
  // Holder, of which the update keeps only a part.
  let from_field = Tag { holder().tag | n: 9 }
  let from_index = Tag { holder().all[1] | name: "z" ++ "z" }
  if from_field.name == "h!" && from_field.n == 9 && from_field.xs[0] == "h?" { score += 4 }
  if from_index.name == "zz" && from_index.n == 3 && from_index.xs[0] == "b?" { score += 8 }
  // A base built by an update of another update, and a parenthesized expression, which
  // is how a base that needs an operator is still spelled.
  let chained = Tag { Tag { make("e", 1) | n: 2 } | name: "y" ++ "y" }
  let parens = Tag { (if chained.n == 2 { make("f", 4) } else { make("g", 5) }) | n: 6 }
  if chained.name == "yy" && chained.n == 2 && chained.xs[0] == "e?" { score += 16 }
  if parens.name == "f!" && parens.n == 6 && parens.xs[0] == "f?" { score += 32 }
  // The base's own fields still read afterwards, so nothing was moved out of a local.
  let named = make("k", 1)
  let copy = Tag { named | n: 2 }
  if named.n == 1 && copy.n == 2 && named.xs[0] == "k?" && copy.name == named.name { score += 64 }
  u8(score)
}`
	if got := buildAndRunASanWithPrelude(t, src); got != 127 {
		t.Errorf("exited %d; want 127 (each bit is one base form that must hold)", got)
	}
}

// **A generic struct can be updated** (10/21): `Box { b | tag: t }` on a `b: Box<e>` was
// refused as "base struct has type Box<e>, expected Box" — the base compared with the bare
// declaration rather than the instantiation — so no generic type could be updated at all,
// inside a generic function or out of one, by name or through an `impl` block's `self`.
// The base's type arguments are the literal's now, which a listed field may confirm but not
// change: an update to a value of another type is refused at its field.
func TestExec_RecordUpdateOfAGenericStruct(t *testing.T) {
	t.Parallel()
	src := `module main
struct Box<e> { value: e, tag: string, items: []e }
let retag<e> = pure (b: Box<e>, tag: string) -> Box<e> => Box { b | tag: tag }
impl Box<e> {
  let with_value = pure (self, v: e) -> Box<e> => Box { self | value: v }
}
let main = () -> u8 => {
  var score = 0
  let b = Box { value: 5, tag: "a" ++ "b", items: [1, 2] }
  let r = retag(b, "c" ++ "d")
  if r.tag == "cd" && r.value == 5 && r.items.len() == 2 && b.tag == "ab" { score += 1 }
  let w = r.with_value(9)
  if w.value == 9 && w.tag == "cd" && w.items[1] == 2 { score += 2 }
  let s = Box { value: "x" ++ "y", tag: "t", items: [] }
  let direct = Box { s | tag: "u" }
  if direct.value == "xy" && direct.tag == "u" { score += 4 }
  u8(score)
}`
	if got := buildAndRunASanWithPrelude(t, src); got != 7 {
		t.Errorf("exited %d; want 7 (each bit is one update that must hold)", got)
	}
}
