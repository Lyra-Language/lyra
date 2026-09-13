package llvm

import "testing"

// **A constructor's payload type belongs to the module that declared the constructor.**
// `Cons(n)` matched in an importer binds `n` at `lib`'s `Node`, whatever `Node` means — or
// fails to mean — where the match is written. Until 09/13 the payload's name was resolved
// from the *use* site, so a private payload type was "undefined" in every importer, and an
// importer declaring its own `Node` read the library's value through the wrong struct
// (`Node has no field "v"`).
//
// Both shapes are here: a recursive `data` type whose payload is a private struct that
// refers back to it (so resolving the payload must not unfold the type it sits in), and a
// generic one, `Box<t> = Full(Pair<t>)`, where the payload is a ParameterizedType and the
// same rule has to reach the instantiation's layout in the backend. The importer shadows
// both names with unrelated structs of its own, which is the case a use-site lookup gets
// silently wrong rather than loudly.
func TestExec_ConstructorPayloadResolvesFromItsDeclaration(t *testing.T) {
	t.Parallel()
	got := buildAndRunModules(t, map[string]string{
		"app.lyra": `import lib.{ List, Cons, Nil, build, Box, Full, Empty, boxed }
struct Node { unrelated: bool }
struct Pair { z: bool }
let sum = pure (l: shared List) -> i64 => match l {
  Cons(n) => n.v + sum(n.next),
  Nil => 0,
}
let main = () -> u8 => {
  let mine = Node { unrelated: true }
  let pair = match boxed(5) { Full(p) => p.a * p.b, Empty => 0 }
  if mine.unrelated { u8(sum(build()) * 10 + pair) } else { 0 }
}
`,
		"lib.lyra": `module lib
struct Node { v: i64, next: shared List }
pub data List = Cons(Node) | Nil
struct Pair<t> { a: t, b: t }
pub data Box<t> = Full(Pair<t>) | Empty
pub let build = pure () -> shared List => {
  let tail: shared List = Nil
  let one: shared List = Cons(Node { v: 2, next: tail })
  Cons(Node { v: 1, next: one })
}
pub let boxed = pure (x: i64) -> Box<i64> => Full(Pair { a: x, b: x + 1 })
`,
	})
	if got != 60 {
		t.Errorf("program exited %d; want 60 (sum 3 * 10 + 5 * 6)", got)
	}
}
