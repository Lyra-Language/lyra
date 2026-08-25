package driver_test

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/Lyra-Language/lyra/pkg/ast/symbols"
	"github.com/Lyra-Language/lyra/pkg/driver"
	"github.com/Lyra-Language/lyra/pkg/modules"
)

// fingerprint renders everything about a Result that a consumer can observe, so two runs can
// be compared as wholes rather than on whichever field a test author thought to check.
func fingerprint(res *driver.Result) string {
	var b strings.Builder
	msgs := make([]string, 0, len(res.Diagnostics))
	for _, d := range res.Diagnostics {
		msgs = append(msgs, fmt.Sprintf("%v|%s|%d:%d|%s|%s",
			d.Severity, d.Code, d.Location.StartLine, d.Location.StartCol, d.File, d.Message))
	}
	// Diagnostic *order* is not part of the contract — passes contribute in a fixed
	// sequence, but a map-ordered pass could reorder within it — so compare as a set.
	sort.Strings(msgs)
	for _, m := range msgs {
		b.WriteString(m + "\n")
	}
	st := res.SymbolTable
	b.WriteString(fmt.Sprintf("statements=%d\n", len(res.Program.Statements)))
	for _, section := range []struct {
		name string
		keys []string
	}{
		{"types", keysOf(st.Types)},
		{"functions", keysOf(st.Functions)},
		{"traits", keysOf(st.Traits)},
		{"moduleOf", keysOf(st.ModuleOf)},
		{"moduleScopes", keysOf(st.ModuleScopes)},
		{"importScopes", keysOf(st.ImportScopes)},
		{"overloads", keysOf(st.OverloadSets)},
		{"preludeNames", keysOf(st.PreludeNames)},
	} {
		b.WriteString(section.name + ": " + strings.Join(section.keys, ",") + "\n")
	}
	b.WriteString(fmt.Sprintf("shadowed=%d moduleDocs=%d importedModules=%d moduleOfFile=%d\n",
		len(st.Shadowed), len(st.ModuleDocs), len(st.ImportedModules), len(st.ModuleOfFile)))
	for _, m := range keysOf(st.ModuleDocs) {
		if d := st.ModuleDocs[m]; d != nil {
			b.WriteString("doc[" + m + "]: " + d.Summary + "\n")
		}
	}
	for _, sh := range st.Shadowed {
		b.WriteString("shadow: " + sh.Name + "|" + sh.Source + "\n")
	}
	b.WriteString("globalSymbols: " + strings.Join(scopeNames(st.GlobalScope), ",") + "\n")
	b.WriteString("preludeSymbols: " + strings.Join(scopeNames(st.PreludeScope), ",") + "\n")
	// Module and import scopes are where declarations actually live — the global scope
	// holds nothing in this configuration — so these are the ones a leak would show up in.
	for _, m := range keysOf(st.ModuleScopes) {
		b.WriteString("module[" + m + "]: " + strings.Join(scopeNames(st.ModuleScopes[m]), ",") + "\n")
	}
	for _, m := range keysOf(st.ImportScopes) {
		b.WriteString("imports[" + m + "]: " + strings.Join(scopeNames(st.ImportScopes[m]), ",") + "\n")
	}
	return b.String()
}

// scopeNames lists a scope's own bindings, sorted. The scope graph is what a clone is most
// likely to get wrong, so the two scopes every module resolves through are compared directly.
func scopeNames(s *symbols.Scope) []string {
	if s == nil {
		return nil
	}
	out := make([]string, 0, len(s.Symbols))
	for k := range s.Symbols {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func keysOf[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// The gate for the whole cache: a cached run must be **indistinguishable** from an uncached
// one, over the real prelude, across a sequence of edits like the ones an editor produces.
//
// **How far this actually reaches, measured rather than assumed.** Sharing a SymbolTable
// field instead of copying it was tried one field at a time, and this test catches seven of
// the sixteen: Types, Functions, Traits, ModuleOf, ModuleDocs, ModuleScopes, OverloadSets.
// The first sweep caught only five, which is why the last four edits below exist — each was
// added to reach a field the fingerprint was blind to, and the overload edit had to be
// rewritten when it turned out to declare two differently-named functions rather than a
// receiver-keyed overload.
//
// The nine it does not catch are not gaps in the clone — the clone copies every field — but
// places where sharing happens to be harmless, each for its own reason:
//
//   - Imports, ImportScopes, ImportedModules: the import graph is part of the cache key, so
//     a change invalidates the snapshot rather than reaching a stale copy. ImportedModules is
//     also overwritten wholesale by SetImports on every run.
//   - PreludeScope, PreludeNames: derived from the prelude, which is inside the keyed prefix.
//   - GlobalScope: empty in this configuration — declarations live in module scopes and the
//     lookup maps. Measured, not assumed.
//   - CurrentScope: Restore resets it.
//   - ModuleOfFile: keyed by path, and the edited file's entry overwrites identically.
//   - Shadowed: a slice, and `append` on the clone cannot change the master's length.
//
// That list is the reason the clone copies all sixteen anyway. Six of those arguments depend
// on facts that could change — an empty GlobalScope, a key that covers imports — and a clone
// that copies everything is right whether or not they hold.
//
// It is written as a whole-Result comparison rather than a spot check because the failure
// mode this guards is not a wrong answer in a known place — it is state leaking between
// keystrokes, which shows up wherever that state happened to matter. Three edits in sequence
// against one cache is the shape that catches drift: a bug that shares one map with the
// master is invisible on the first edit and appears on the second.
func TestCollectCache_MatchesUncachedAnalysis(t *testing.T) {
	repoRoot, _ := filepath.Abs("../..")
	dir := t.TempDir()
	app := filepath.Join(dir, "main.lyra")
	roots := []string{repoRoot, dir}

	edits := []string{
		`module main
let main = () -> void => println("one")
`,
		`module main
let helper = pure (n: i64) -> i64 => n * 2
let main = () -> void => println(helper(21))
`,
		`module main
trait Shout { pure shout: (Self) -> string }
struct Cat { n: i64 }
impl Shout for Cat { shout = pure (self) => "meow" }
let helper = pure (n: i64) -> i64 => n * 2
let main = () -> void => {
  println(helper(21))
  println(Cat { n: 1 }.shout())
  let s = "a" ++ "b"
  println(s.slice(0, 1))
}
`,
		// Back to something small: the cache must not carry the larger program's state.
		`module main
let main = () -> void => println("one")
`,
		// An error, then a fix — a server sees far more broken states than valid ones.
		`module main
let main = () -> void => println(undefinedName())
`,
		`module main
let main = () -> void => println("recovered")
`,
		// The next three exist because the field sweep found the fingerprint blind to
		// them. Each names a piece of table state that **accumulates** per edit rather
		// than being overwritten, so sharing it instead of copying leaks from one
		// keystroke into the next — and the only way to see that is to add the thing and
		// then take it away again.
		//
		// Shadowed is an append-only slice: a declaration taking a prelude name records
		// an entry every run.
		`module main
let min = pure (a: i64, b: i64) -> i64 => a
let main = () -> void => println(min(1, 2))
`,
		// A module doc, which is keyed by module and joined across a module's files.
		`//! a module header that later goes away
module main
let main = () -> void => println("doc")
`,
		// Receiver-keyed overloads, which build an OverloadSet per name.
		`module main
struct Box { n: i64 }
struct Bag { n: i64 }
let size = pure (self: Box) -> i64 => self.n
let size = pure (self: Bag) -> i64 => self.n
let main = () -> void => println(Box { n: 1 }.size() + Bag { n: 2 }.size())
`,
		// …and back to the plain program, so anything the three above left behind shows
		// up as a difference here.
		`module main
let main = () -> void => println("one")
`,
	}

	cache := driver.NewCollectCache()
	for i, src := range edits {
		if err := os.WriteFile(app, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
		opts := modules.Options{Prelude: modules.PreludeModule}
		units, _ := modules.Resolve(app, roots, opts)
		if len(units) == 0 {
			t.Fatalf("edit %d: no units", i)
		}
		want := fingerprint(driver.AnalyzeUnits(units))

		units2, _ := modules.Resolve(app, roots, opts)
		got := fingerprint(driver.AnalyzeUnitsCached(units2, cache))

		if got != want {
			t.Errorf("edit %d: cached analysis differs from uncached.\n--- uncached ---\n%s\n--- cached ---\n%s",
				i, want, got)
		}
	}
}
