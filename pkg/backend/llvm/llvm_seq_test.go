package llvm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Lyra-Language/lyra/pkg/driver"
	"github.com/Lyra-Language/lyra/pkg/modules"
)

// Lazy sequences, stage 1 (seq_lower.go): a producer is lowered at its consumer. These
// run through the real prelude, since the combinators are prelude Lyra and the point is
// that ordinary `gen` functions fuse.

// Every consumer and control transfer at once: a `for` with `break` and `continue`
// inside a chain, the two-variable form, brackets, and the three terminals.
func TestExec_SeqConsumersAndControl(t *testing.T) {
	t.Parallel()
	out := buildAndRunWithPrelude(t, `module main
let nums = pure gen () -> Seq<i64> => { var n = 0; for { yield n; n += 1 } }
let main = () -> void => {
  var seen = 0
  for x in nums().filter((n) => n % 2 == 0) {
    if x == 4 { continue }
    if x > 8 { break }
    seen += x
  }
  print("${seen} ")
  for i, x in nums().map((n) => n * 10).take(3) { print("${i}:${x} ") }
  let evens = [x in nums().take(10) | x % 2 == 0 | x]
  print("${evens.join(",")} ${nums().take(5).sum()} ${nums().take_while((n) => n < 7).count()} ${nums().filter((n) => n > 100).first().unwrap_or(-1)}")
}
`, "")
	// 0+2+6+8: the 4 is skipped by continue, and the break at 10 ends the walk.
	if got := strings.TrimSpace(out); got != "16 0:0 1:10 2:20 0,2,4,6,8 10 7 101" {
		t.Errorf("printed %q; want \"16 0:0 1:10 2:20 0,2,4,6,8 10 7 101\"", got)
	}
}

// Managed elements through a chain, under ASan: each yielded string is a temporary of
// its yield statement and is released after the consumer body ran, on the resumed path
// and on a break alike; the elements the brackets keep are retained into the array.
func TestExec_SeqManagedElementsASan(t *testing.T) {
	t.Parallel()
	src := `module main
let names = pure gen () -> Seq<string> => {
  for i in 0..<6 { yield "name-" ++ "${i}" }
}
let main = () -> u8 => {
  var total = 0
  for s in names().map((s) => s ++ "!").filter((s) => s.len() > 6) {
    if s.ends_with("4!") { break }
    total += s.len()
  }
  let kept = [s in names().take(3) | s.len() > 0 | s ++ "?"]
  var seq_len = 0
  for s in kept.seq() { seq_len += s.len() }
  // four seven-character names before the break at name-4!
  if total == 28 && kept.len() == 3 && kept[2] == "name-2?" && seq_len == 21 { 3 } else { 1 }
}`
	if got := buildAndRunASanWithPrelude(t, src); got != 3 {
		t.Errorf("under ASan: exited %d; want 3", got)
	}
}

// `yield from` over another sequence, a producer written as a plain function whose body
// is a chain, and a sequence parameter forwarded through two levels.
func TestExec_SeqYieldFromAndProducerAlias(t *testing.T) {
	t.Parallel()
	out := buildAndRunWithPrelude(t, `module main
let small = pure gen () -> Seq<i64> => { yield 1; yield 2 }
let both = pure gen () -> Seq<i64> => { yield from small(); yield 10; yield from small() }
let odds = pure (s: Seq<i64>) -> Seq<i64> => s.filter((n) => n % 2 == 1)
let doubled = pure gen (s: Seq<i64>) -> Seq<i64> => { for x in s { yield x * 2 } }
let main = () -> void => {
  // Bound first: a comprehension inside an interpolation does not parse (todo.md).
  let all = [x in both() | x]
  let odd = [x in odds(both()) | x]
  print("${all.join(",")} ${odd.join(",")} ${doubled(odds(both())).sum()}")
}
`, "")
	if got := strings.TrimSpace(out); got != "1,2,10,1,2 1,1 4" {
		t.Errorf("printed %q; want \"1,2,10,1,2 1,1 4\"", got)
	}
}

// What stage 1 refuses, by name: a sequence held in a binding, and one used as a value.
func TestEmit_SeqStage1Refusals(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ name, body, want string }{
		{"bound", `let s = small(); for x in s { print("${x}") }`, "used as a value"},
		{"value", `let n = small(); print("${n.sum()}")`, "used as a value"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			_, err := emitWithPreludeErr(t, `module main
let small = pure gen () -> Seq<i64> => { yield 1 }
let main = () -> void => { `+c.body+` }
`)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("want a refusal mentioning %q; got %v", c.want, err)
			}
		})
	}
}

// emitWithPreludeErr is emitWithPrelude returning the backend's error rather than
// failing the test on it — for a test whose subject *is* the refusal.
func emitWithPreludeErr(t *testing.T, src string) (string, error) {
	t.Helper()
	dir := t.TempDir()
	entry := filepath.Join(dir, "app.lyra")
	if err := os.WriteFile(entry, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
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
	return string(ir), err
}
