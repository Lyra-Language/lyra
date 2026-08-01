package modules_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Lyra-Language/lyra/pkg/driver"
	"github.com/Lyra-Language/lyra/pkg/modules"
)

// These tests exercise the **shipped** `std/prelude.lyra`, not a synthetic one.
//
// prelude_test.go's `testPrelude` covers the prelude *mechanism* — implicit import,
// shadowing, opt-out — and deliberately uses a fixture so those tests say nothing about
// the standard library's contents. Nothing exercised the real file, so a prelude that did
// not type-check, or one whose canonical markers had quietly stopped working, would ship
// green: every other test either declares its own `Maybe` or has no prelude at all.

// shippedPreludePath returns the repo's std/prelude.lyra, skipping if it is not there.
// The path is relative because a Go test runs in its own package directory.
func shippedPreludePath(t *testing.T) (root, path string) {
	t.Helper()
	root = filepath.Join("..", "..")
	path = filepath.Join(root, "std", "prelude.lyra")
	if _, err := os.Stat(path); err != nil {
		t.Skipf("no shipped prelude at %s: %v", path, err)
	}
	return root, path
}

// analyzeWith resolves an entry file against roots with the prelude enabled — the same
// call lyrac makes, so what these tests check is what a user gets.
func analyzeWith(t *testing.T, entry string, roots ...string) *driver.Result {
	t.Helper()
	units, diags := modules.Resolve(entry, roots, modules.Options{Prelude: modules.PreludeModule})
	if len(diags) != 0 {
		t.Fatalf("resolve failed: %v", diags)
	}
	return driver.AnalyzeUnits(units)
}

// The shipped prelude compiles on its own — the property that lets it be treated as an
// ordinary module. It is also the cheapest possible guard on adding to it: a declaration
// that does not type-check fails here rather than in whatever program first imports it.
func TestShippedPrelude_ChecksStandalone(t *testing.T) {
	_, path := shippedPreludePath(t)
	// Its own directory as the only root: the prelude does not import itself, so
	// resolution must terminate with exactly one unit.
	units, diags := modules.Resolve(path, []string{filepath.Dir(path)},
		modules.Options{Prelude: modules.PreludeModule})
	if len(diags) != 0 {
		t.Fatalf("resolve failed: %v", diags)
	}
	if len(units) != 1 {
		t.Errorf("the prelude must not pull in a second copy of itself; got %d units", len(units))
	}
	if errs := driver.AnalyzeUnits(units).Errors(); len(errs) != 0 {
		t.Errorf("the shipped prelude must type-check on its own; got %v", errs)
	}
}

// Its names are reachable unqualified, and its `Maybe` is the *canonical* one — `?` is
// accepted inside a function returning it, which only holds if the declaration carries a
// CanonicalKind stamp.
func TestShippedPrelude_ProvidesCanonicalMaybe(t *testing.T) {
	repo, _ := shippedPreludePath(t)
	dir := t.TempDir()
	write(t, filepath.Join(dir, "app.lyra"), `let mk = (n: i64) -> Maybe<i64> => Some(n)
let step = (n: i64) -> Maybe<i64> => {
  let v = mk(n)?
  Some(v)
}
let main = () -> u8 => {
  let m: Maybe<i64> = Some(40)
  let n: Maybe<i64> = None
  u8(unwrap_or(m, 0) + unwrap_or(n, 2))
}`)
	res := analyzeWith(t, filepath.Join(dir, "app.lyra"), dir, repo)
	if errs := res.Errors(); len(errs) != 0 {
		t.Errorf("the shipped prelude should supply Maybe, its constructors, unwrap_or and `?`; got %v", errs)
	}
}

// Its combinators are usable, including the higher-order one.
//
// `unwrap_or_else` takes a callback, so it needs the unifier to solve `t` through a
// function type — an omission that made every callback-taking combinator uncallable and
// went unnoticed because nothing exercised the shipped prelude's contents.
func TestShippedPrelude_CombinatorsAreCallable(t *testing.T) {
	repo, _ := shippedPreludePath(t)
	dir := t.TempDir()
	write(t, filepath.Join(dir, "app.lyra"), `let fortyTwo = () -> i64 => 42
let main = () -> u8 => {
  let n: Maybe<i64> = None
  let m: Maybe<i64> = Some(0)
  u8(unwrap_or(m, 1) + unwrap_or_else(n, fortyTwo))
}`)
	res := analyzeWith(t, filepath.Join(dir, "app.lyra"), dir, repo)
	if errs := res.Errors(); len(errs) != 0 {
		t.Errorf("the prelude's combinators should be callable; got %v", errs)
	}
}

// The `@builtin(…)` marker confers canonical identity **independently of the spelling** —
// the entire reason it exists over the name+shape fallback.
//
// This is a regression test with a specific failure in mind: the argument is easy to omit,
// a bare `@builtin` collects as *no marker at all* (`collectBuiltin` returns "" when the
// attribute has no args), and the prelude then works anyway — silently, through the
// fallback that reads the literal name `Maybe`. Nothing observable changes until someone
// renames the type, at which point `?` stops resolving. Renaming it here is what makes the
// marker load-bearing in the test rather than decorative.
func TestShippedPrelude_BuiltinMarkerIsNameIndependent(t *testing.T) {
	repo, path := shippedPreludePath(t)
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Rename the *type* only. The marker keeps naming the kind `Maybe`, so identity can
	// now come from nothing but the marker.
	//
	// Rewriting every type use rather than an enumerated few: `Maybe<` catches the
	// declaration, parameters and return types alike, and cannot touch `@builtin(Maybe)`,
	// which carries no `<`. Listing the positions individually meant the test broke — as
	// "a renamed type should behave like Maybe" — the moment the prelude grew a function
	// *returning* a `Maybe`, which is a prelude doing its job, not a regression.
	renamed := strings.ReplaceAll(string(source), "Maybe<", "Option<")
	if !strings.Contains(renamed, "data Option<t>") {
		t.Fatalf("the prelude no longer declares `data Maybe<t>`, which this test rewrites:\n%s", renamed)
	}
	// Checked separately, and *not* phrased as "this test is out of date": a missing
	// argument is the regression, so the message has to point at the prelude.
	if !strings.Contains(renamed, "@builtin(Maybe)") {
		t.Fatalf("the prelude's Maybe carries no `@builtin(Maybe)` marker — a bare `@builtin` " +
			"collects as no marker at all, leaving canonical identity to come from the literal " +
			"name, which is what this test exists to prevent")
	}

	dir := t.TempDir()
	write(t, filepath.Join(dir, "std", "prelude.lyra"), renamed)
	write(t, filepath.Join(dir, "app.lyra"), `let mk = (n: i64) -> Option<i64> => Some(n)
let step = (n: i64) -> Option<i64> => {
  let v = mk(n)?
  Some(v)
}
let main = () -> u8 => u8(unwrap_or(mk(42), 0))`)

	res := analyzeWith(t, filepath.Join(dir, "app.lyra"), dir, repo)
	for _, d := range res.Errors() {
		// The tell-tale failure: with no effective marker, a type called `Option` is not
		// canonical, so `?` is rejected by lyra-E008 / the operand check.
		if strings.Contains(d.Message, "Result or Maybe") {
			t.Fatalf("`@builtin(Maybe)` is not conferring identity — the canonical kind is "+
				"coming from the name, not the marker (is the argument missing?): %s", d.Message)
		}
	}
	if errs := res.Errors(); len(errs) != 0 {
		t.Errorf("a renamed but marked canonical type should behave exactly like Maybe; got %v", errs)
	}
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The shipped combinators are callable from `pure` code — the property the whole
// effect-polymorphism pass exists for, checked against the real std/ rather than a
// synthetic module, because it is the shipped annotations that have to be right.
//
// Both directions matter. A pure callback must be accepted (before effect polymorphism a
// call through a function-typed parameter tainted every effect, so no combinator was
// callable from `pure` at all), and an impure one must still be rejected — at the call
// site, which is where the impurity actually is.
func TestShippedPrelude_CombinatorsAreUsableFromPureCode(t *testing.T) {
	repo, path := shippedPreludePath(t)
	dir := filepath.Dir(filepath.Dir(path)) // the root *containing* std/

	t.Run("pure callback", func(t *testing.T) {
		entry := filepath.Join(t.TempDir(), "app.lyra")
		write(t, entry, `import std.maybe
let pipeline = pure (m: Maybe<i64>) -> i64 => {
  let doubled = maybe.map(m, (x: i64) -> i64 => x * 2)
  unwrap_or_else(doubled, () -> i64 => 0)
}
let main = () -> u8 => u8(pipeline(Some(4)))`)
		if errs := analyzeWith(t, entry, filepath.Dir(entry), dir, repo).Errors(); len(errs) != 0 {
			t.Errorf("a pure pipeline over the shipped combinators should check; got %v", errs)
		}
	})

	t.Run("impure callback is still rejected", func(t *testing.T) {
		entry := filepath.Join(t.TempDir(), "app.lyra")
		write(t, entry, `import std.maybe
var log = 0
let sneaky = (x: i64) -> i64 => { log = x; x }
let pipeline = pure (m: Maybe<i64>) -> Maybe<i64> => maybe.map(m, sneaky)
let main = () -> u8 => 0`)
		errs := analyzeWith(t, entry, filepath.Dir(entry), dir, repo).Errors()
		if len(errs) == 0 {
			t.Fatal("a pure function passing an impure callback should be rejected")
		}
		if !strings.Contains(errs[0].Message, "impure") {
			t.Errorf("expected an impurity diagnostic, got %v", errs)
		}
	})
}
