package main

import (
	"os"
	"path/filepath"
	"testing"
)

// A private generic struct of a library, `Cached<e>`, is another module's type argument —
// `HashMap<string, Cached<e>>` — instantiated at the caller's own private type, `Ev`, and that
// instantiation is first laid out from a non-generic function of the caller's (`remember`'s
// `mut Cache<Ev>` parameter), outside any specialization. Laying out the `Maybe` the map's
// slots hold resolved `Ev` from `Cached`'s declaration, where `main`'s private type is not
// visible, and failed: "cannot lay out data type Maybe" (10/21). `ui.layout`'s `Cached<e>` and
// `ui.interact`'s `Opened<e>` were `pub` for it until 10/30, which hid it — the instantiation
// was then first laid out from elsewhere.
func TestRun_APrivateGenericStructIsAnotherModulesTypeArgument(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	setenv(t, "LYRA_STD", root)
	dir := t.TempDir()
	for name, text := range map[string]string{
		"lib.lyra": `module lib
import std.collections.{ HashMap, hashmap_new }
pub data Kind<e> = Press(e, i64) | Plain
pub struct Node<e> { kind: Kind<e>, label: string }
struct Cached<e> { node: Node<e>, hits: i64 }
pub struct Cache<e> { by_key: HashMap<string, Cached<e>> }
pub let cache_new<e> = () -> Cache<e> => Cache { by_key: hashmap_new::<string, Cached<e>>() }
pub let put<e> = (c: mut Cache<e>, key: string, node: Node<e>) -> void => {
  c.by_key.insert(key, Cached { node: node, hits: 1 })
}
pub let pressed<e> = (c: Cache<e>, key: string) -> Maybe<e> => match c.by_key.get(key) {
  Some(found) => match found.node.kind { Press(ev, _) => Some(ev), Plain => None },
  None => None,
}
`,
		"main.lyra": `import lib.{ Cache, cache_new, put, pressed, Node, Press }
data Ev = Edited(string)
let main = () -> u8 => {
  var cache: Cache<Ev> = cache_new()
  remember(cache)
  match pressed(cache, "ok") { Some(Edited(t)) => u8(t.len()), None => 0 }
}
let remember = (cache: mut Cache<Ev>) -> void =>
  put(cache, "ok", Node { kind: Press(Edited("a" ++ "bc"), 1), label: "x" })
`,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, stderr, code := captureRun(t, "run", filepath.Join(dir, "main.lyra")); code != 3 {
		t.Errorf("expected \"abc\"'s length, 3; exit %d, stderr: %s", code, stderr)
	}
}
