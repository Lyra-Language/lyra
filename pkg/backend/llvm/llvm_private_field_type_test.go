package llvm

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Lyra-Language/lyra/pkg/driver"
	"github.com/Lyra-Language/lyra/pkg/modules"
)

// A type name written inside a declaration means what the *declaring* module says, not what
// the module holding the value says. The backend resolved such a name from whichever module
// it was lowering, so a public struct over a private field type failed outright
// ("unknown named type") the moment another module released one — and, worse, would have
// found a same-named declaration of the reader's own where it had one, lowering the struct
// against the wrong layout. A resolved key now rides on the name
// (types.UnresolvedType.Key); these are both halves.
func TestExec_PrivateFieldTypeFromAnotherModule(t *testing.T) {
	t.Parallel()
	out := runModules(t, map[string]string{
		"lib/inner.lyra": `module lib.inner
struct Inner { v: i64 }
pub struct Outer { xs: []Inner, n: i64 }
pub let make = pure (k: i64) -> Outer => {
  var xs: []Inner = []
  for i in 0..<k { xs.push(Inner { v: i }) }
  Outer { xs: xs, n: k }
}
pub let total = pure (o: Outer) -> i64 => {
  var t = 0
  for x in o.xs { t += x.v }
  t
}
`,
		"main.lyra": `module main
import lib.inner.{ make, total }
let main = () -> void => println(total(make(4)))
`,
	})
	if got := strings.TrimSpace(out); got != "6" {
		t.Errorf("total = %q; want \"6\" (0+1+2+3)", got)
	}
}

// The same shape, with the *reader* declaring its own `Inner` of a different layout — the
// case a name-based fallback would answer wrongly and silently, since the lookup succeeds.
// Both structs are used, so both layouts have to be right: three i64 fields summed through
// the library, one string field read in main.
func TestExec_PrivateFieldTypeShadowedByTheReader(t *testing.T) {
	t.Parallel()
	out := runModules(t, map[string]string{
		"lib/inner.lyra": `module lib.inner
struct Inner { a: i64, b: i64, c: i64 }
pub struct Outer { xs: []Inner, n: i64 }
pub let make = pure (k: i64) -> Outer => {
  var xs: []Inner = []
  for i in 0..<k { xs.push(Inner { a: i, b: i * 10, c: i * 100 }) }
  Outer { xs: xs, n: k }
}
pub let total = pure (o: Outer) -> i64 => {
  var t = 0
  for x in o.xs { t += x.a + x.b + x.c }
  t
}
`,
		"main.lyra": `module main
import lib.inner.{ make, total }
struct Inner { name: string }
let main = () -> void => {
  let mine = Inner { name: "mine" }
  let o = make(4)
  println("${total(o)} ${o.n} ${mine.name}")
}
`,
	})
	if got := strings.TrimSpace(out); got != "666 4 mine" {
		t.Errorf("output = %q; want \"666 4 mine\"", got)
	}
}

// runModules writes a program of several files to a temp directory, builds it with that
// directory as a module root, and answers what it printed. The multi-file shape is the
// point: a module boundary is what the bug needs, and emitWithPrelude takes one source.
func runModules(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	entry := filepath.Join(dir, "main.lyra")
	units, diags := modules.Resolve(entry, []string{dir, repoStdRoot(t)}, modules.Options{Prelude: modules.PreludeModule})
	if len(diags) != 0 {
		t.Fatalf("resolve: %v", diags)
	}
	res := driver.AnalyzeUnits(units)
	if res.HasErrors() {
		t.Fatalf("unexpected analysis errors: %v", res.Errors())
	}
	ep, epDiags := driver.ResolveEntryPoint(res)
	if ep == nil {
		t.Fatalf("no entry point: %v", epDiags)
	}
	ir, err := New().Emit(res, ep)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	out, err := exec.Command(compileCached(t, lookClang(t), string(ir))).Output()
	if err != nil {
		t.Fatalf("running the binary failed: %v", err)
	}
	return string(out)
}
