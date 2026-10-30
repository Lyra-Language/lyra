package main

import (
	"os"
	"path/filepath"
	"testing"
)

// **`m.X` reaches a module's `const` and top-level `var`** (09/30), as `m.f()` reaches its
// functions — `pad.LEFT` is how a game reads a button. Run, not just checked: the backend
// resolves the binding from its declaration, so a local of the same name in the reading
// function must not capture it.
func TestRun_ANamespaceReachesAModulesBindings(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	setenv(t, "LYRA_STD", root)
	dir := t.TempDir()
	for name, text := range map[string]string{
		"util/settings.lyra": "module util.settings\n" +
			"pub const LIMIT: i64 = 30\n" +
			"pub const TABLE: [3]i64 = #[4, 5, 6]\n" +
			"pub var count: i64 = 2\n",
		"main.lyra": "import util.settings\n" +
			"let main = () -> u8 => {\n" +
			"  let count = 1000\n" + // a local sharing the name must not win
			"  u8(settings.LIMIT + settings.TABLE[2] + settings.count + count - 1000)\n" +
			"}\n",
	} {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, stderr, code := captureRun(t, "run", filepath.Join(dir, "main.lyra")); code != 38 {
		t.Errorf("expected 30 + 6 + 2 = 38; exit %d, stderr: %s", code, stderr)
	}
}

// **A namespace reaches a module's constructors and types** (10/29): `shapes.Circle(2)`,
// `shapes.Dot`, `shapes.Pt { … }`, `shapes.Circle(r) =>`, `p: shapes.Pt` and a generic
// `shapes.Box<i64>`, beside this module's own `Circle` and `Shape`. Run, because the two
// `Circle`s have different payloads and tags: one taken for the other is a wrong answer
// here, not just a wrong type.
func TestRun_ANamespaceReachesAModulesConstructorsAndTypes(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	setenv(t, "LYRA_STD", root)
	dir := t.TempDir()
	for name, text := range map[string]string{
		"geo/shapes.lyra": `module geo.shapes
pub data Shape = Square(i64) | Circle(i64) | Dot
pub struct Pt { x: i64, y: i64 }
pub data Box<t> = Wrap(t)
pub let area = pure (s: Shape) -> i64 => match s {
  Square(n) => n * n,
  Circle(r) => 3 * r * r,
  Dot => 0,
}
`,
		"main.lyra": `import geo.shapes
import std.collections
data Shape = Circle(string) | Other
let mine = pure (s: Shape) -> i64 => match s { Circle(n) => n.len(), Other => 0 }
let radius = pure (s: shapes.Shape) -> i64 => match s {
  shapes.Circle(r) => r,
  shapes.Square(n) => n * 10,
  shapes.Dot => 0,
}
let sum = pure (p: shapes.Pt) -> i64 => p.x + p.y
let main = () -> u8 => {
  let all: []shapes.Shape = [shapes.Circle(2), shapes.Dot, shapes.Square 1]
  var total = 0
  for s in all { total = total + shapes.area(s) + radius(s) }
  let b: shapes.Box<i64> = shapes.Wrap(4)
  let shapes.Wrap(inner) = b else { return 1 }
  let m: collections.HashMap<string, i64> = collections.hashmap_new()
  let built = [5].map(shapes.Circle)
  u8(total + mine(Circle("abc")) + sum(shapes.Pt { x: 1, y: 2 }) + inner + m.len() + radius(built[0]))
}
`,
	} {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// areas 12 + 0 + 1 and radii 2 + 0 + 10 = 25; mine 3; sum 3; inner 4; built 5.
	if _, stderr, code := captureRun(t, "run", filepath.Join(dir, "main.lyra")); code != 40 {
		t.Errorf("expected 25 + 3 + 3 + 4 + 5 = 40; exit %d, stderr: %s", code, stderr)
	}
}
