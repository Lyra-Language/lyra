package llvm

import (
	"strings"
	"testing"
)

// The six positions that gained a context on 10/27 (typechecker's
// TestGenericContext_ReachesTheValue), compiled and run: a map assigned to a field and
// built in a generic literal, a nullary constructor beside an empty array, a `None`
// default at two instantiations (each literal its own copy of the node), a lambda payload
// typed by its field, a generic element typed by its sibling, and `flatten` called as a
// method with its receiver typed by the binding.
func TestExec_AStatedTypeReachesTheValue(t *testing.T) {
	t.Parallel()
	out := buildAndRunWithPrelude(t, `
module main
import std.collections.{ HashMap, hashmap_new }
struct Cache { seen: HashMap<string, i64> }
struct Tagged<e> { seen: HashMap<string, e>, tag: e }
data K<e> = L | Ev(e)
struct Nd<e> { k: K<e>, xs: []e }
struct Laid<e> { v: e }
struct Host<e> { shown: Maybe<Laid<e>> = None, n: i64 }
struct Theme { dark: bool }
data Rule = Scope((Theme) -> i64) | Plain(i64)
struct Node<e> { ev: Maybe<e> }
data Click = Clicked
let mk<e> = () -> Nd<e> => Nd { k: L, xs: [] }
let boxed = () -> Node<Click> => Node { ev: Some(Clicked) }
let sized<e> = () -> Node<e> => Node { ev: None }
let text<e> = (s: string) -> Node<e> => Node { ev: None }
let main = () -> void => {
  var c = Cache { seen: hashmap_new() }
  c.seen = hashmap_new()
  c.seen.insert("a", 1)
  var t = Tagged { seen: hashmap_new(), tag: 7 }
  t.seen.insert("b", t.tag)
  let n: Nd<string> = mk()
  let a: Host<i64> = Host { n: 1 }
  let b: Host<string> = Host { n: 2 }
  let r = Scope((th) => if th.dark { 10 } else { 20 })
  let applied = match r { Scope(f) => f(Theme { dark: true }), Plain(x) => x }
  let nodes = [boxed(), sized()]
  let flat: []Node<Click> = [[text("a")], [text("b"), text("c")]].flatten()
  println("${c.seen.len()} ${t.seen.get_or("b", 0)} ${n.xs.len()} ${a.shown.is_none()} ${b.shown.is_none()} ${a.n + b.n}")
  println("${applied} ${nodes.len()} ${nodes[1].ev.is_none()} ${flat.len()}")
}
`, "")
	want := "1 7 0 true true 3\n10 2 true 3"
	if got := strings.TrimSpace(out); got != want {
		t.Errorf("stated types reaching values:\n%s\nwant:\n%s", got, want)
	}
}
