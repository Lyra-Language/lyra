package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Two modules' same-named types are two types to a method call too. `alpha.Player` and
// `beta.Player` each with a `bump` made `p.bump()` "ambiguous" on either (10/16): a written
// `self` type was a bare name, so the receiver test compared names alone. It is keyed by
// the declaration it names from the method's own module now, and each call reaches its own
// module's `bump` — the beta one writing fields at offsets the alpha one does not have.
func TestMethodCall_SameNamedTypesInTwoModules(t *testing.T) {
	t.Parallel()
	requireCC(t)
	dir := t.TempDir()
	files := map[string]string{
		"alpha.lyra": `module alpha
pub struct Player { a: i64 = 1, b: i64 = 2 }
pub let new_player = pure () -> Player => Player {}
impl Player {
  pub let bump = (self: mut) -> void => { self.b += 10 }
}
`,
		"beta.lyra": `module beta
pub struct Player { x: i64 = 100, y: i64 = 200, z: bool = true, w: i64 = 300 }
pub let new_player = pure () -> Player => Player {}
impl Player {
  pub let bump = (self: mut) -> void => { self.w += 1 }
}
`,
		"main.lyra": `import alpha
import beta
let main = () -> void => {
  var p = alpha.new_player()
  var q = beta.new_player()
  p.bump()
  q.bump()
  println("${p.a} ${p.b} | ${q.x} ${q.y} ${q.z} ${q.w}")
}
`,
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	stdout, stderr, code := captureRun(t, "run", filepath.Join(dir, "main.lyra"))
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, stderr)
	}
	if got, want := strings.TrimSpace(stdout), "1 12 | 100 200 true 301"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// Generic methods of two modules' same-named types are two functions at one binding too. A
// generic method's specialization was named by its receiver's head, so `alpha.Counter`'s
// `tally_by<t>` and `beta.Counter`'s, both used at `i64`, were one symbol: one body was emitted
// and both modules' calls ran it, without a word (10/16 — a copy of std.chips.music beside
// the real one ran the real one's code over its own struct). The discriminant is the
// type's declaration key where its name is declared more than once.
func TestMethodCall_SameNamedTypesGenericMethods(t *testing.T) {
	t.Parallel()
	requireCC(t)
	dir := t.TempDir()
	files := map[string]string{
		"alpha.lyra": `module alpha
pub struct Counter { n: i64 = 0 }
pub let new_counter = pure () -> Counter => Counter {}
impl Counter {
  pub let tally_by<t> = (self: mut, by: t, f: (t) -> i64) -> void => { self.n += f(by) }
}
`,
		"beta.lyra": `module beta
pub struct Counter { label: i64 = 7, n: i64 = 1000 }
pub let new_counter = pure () -> Counter => Counter {}
impl Counter {
  pub let tally_by<t> = (self: mut, by: t, f: (t) -> i64) -> void => { self.n -= f(by) }
}
`,
		"main.lyra": `import alpha
import beta
let main = () -> void => {
  var a = alpha.new_counter()
  var b = beta.new_counter()
  a.tally_by(5, (x: i64) => x)
  b.tally_by(5, (x: i64) => x)
  println("${a.n} | ${b.label} ${b.n}")
}
`,
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	stdout, stderr, code := captureRun(t, "run", filepath.Join(dir, "main.lyra"))
	if code != 0 {
		t.Fatalf("exit %d:\n%s", code, stderr)
	}
	if got, want := strings.TrimSpace(stdout), "5 | 7 995"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
