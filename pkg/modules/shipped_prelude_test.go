package modules_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Lyra-Language/lyra/pkg/backend/llvm"
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

// shippedPreludePath returns the repo's std/prelude/ directory, skipping if it is not
// there. The path is relative because a Go test runs in its own package directory.
//
// A directory since 08/07: the prelude is several files in one module. `path` is what
// these tests hand to Resolve as an entry, and resolution takes either form — so the
// only thing that changed at the call sites is that `filepath.Dir(path)` is no longer
// the root containing `std/`.
func shippedPreludePath(t *testing.T) (root, path string) {
	t.Helper()
	root = filepath.Join("..", "..")
	path = filepath.Join(root, "std", "prelude")
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
// Entered at **one** of its files, which is the multi-file half of the same property:
// compiling `std/prelude/strings.lyra` has to bring its siblings, or the standalone
// check would only ever see a fragment of the module and report the rest undefined.
func TestShippedPrelude_ChecksStandalone(t *testing.T) {
	_, dir := shippedPreludePath(t)
	files := preludeFiles(t, dir)
	root := filepath.Dir(filepath.Dir(dir)) // the root *containing* std/
	for _, entry := range files {
		t.Run(filepath.Base(entry), func(t *testing.T) {
			units, diags := modules.Resolve(entry, []string{root},
				modules.Options{Prelude: modules.PreludeModule})
			if len(diags) != 0 {
				t.Fatalf("resolve failed: %v", diags)
			}
			// Every file of the module, once: the prelude does not import itself, so
			// entering at one of its files must not pull in a second copy.
			if len(units) != len(files) {
				t.Errorf("entering at %s gave %d units; want the module's %d files, each once",
					filepath.Base(entry), len(units), len(files))
			}
			for _, u := range units {
				if u.Path != modules.PreludeModule {
					t.Errorf("unit %s came in as module %q; want %q", u.File, u.Path, modules.PreludeModule)
				}
			}
			if errs := driver.AnalyzeUnits(units).Errors(); len(errs) != 0 {
				t.Errorf("the shipped prelude must type-check on its own; got %v", errs)
			}
		})
	}
}

// preludeFiles lists the prelude's source files. It also asserts the split is real —
// a prelude that collapsed back to one file would make every multi-file assertion here
// pass vacuously.
func preludeFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".lyra") {
			files = append(files, filepath.Join(dir, e.Name()))
		}
	}
	if len(files) < 2 {
		t.Fatalf("%s holds %d source files; these tests exist to cover the multi-file case", dir, len(files))
	}
	return files
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
  u8(m.unwrap_or(0) + n.unwrap_or(2))
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
  u8(m.unwrap_or(1) + n.unwrap_or_else(fortyTwo))
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
	repo, preludeDir := shippedPreludePath(t)

	// Rename the *type* only. The marker keeps naming the kind `Maybe`, so identity can
	// now come from nothing but the marker.
	//
	// Rewriting every type use rather than an enumerated few: `Maybe<` catches the
	// declaration, parameters and return types alike, and cannot touch `@builtin(Maybe)`,
	// which carries no `<`. Listing the positions individually meant the test broke — as
	// "a renamed type should behave like Maybe" — the moment the prelude grew a function
	// *returning* a `Maybe`, which is a prelude doing its job, not a regression.
	//
	// The rewrite runs over **every** file of the prelude, since the declaration and its
	// uses now live in different ones — rewriting only the file holding `data Maybe<t>`
	// would leave `parse_i64` returning a type that no longer exists.
	dir := t.TempDir()
	var declares, marked bool
	for _, file := range preludeFiles(t, preludeDir) {
		source, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		renamed := strings.ReplaceAll(string(source), "Maybe<", "Option<")
		declares = declares || strings.Contains(renamed, "data Option<t>")
		marked = marked || strings.Contains(renamed, "@builtin(Maybe)")
		write(t, filepath.Join(dir, "std", "prelude", filepath.Base(file)), renamed)
	}
	if !declares {
		t.Fatal("the prelude no longer declares `data Maybe<t>`, which this test rewrites")
	}
	// Checked separately, and *not* phrased as "this test is out of date": a missing
	// argument is the regression, so the message has to point at the prelude.
	if !marked {
		t.Fatal("the prelude's Maybe carries no `@builtin(Maybe)` marker — a bare `@builtin` " +
			"collects as no marker at all, leaving canonical identity to come from the literal " +
			"name, which is what this test exists to prevent")
	}

	write(t, filepath.Join(dir, "app.lyra"), `let mk = (n: i64) -> Option<i64> => Some(n)
let step = (n: i64) -> Option<i64> => {
  let v = mk(n)?
  Some(v)
}
let main = () -> u8 => u8(mk(42).unwrap_or(0))`)

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
		write(t, entry, `let pipeline = pure (m: Maybe<i64>) -> i64 => {
  let doubled = m.map((x: i64) -> i64 => x * 2)
  doubled.unwrap_or_else(() -> i64 => 0)
}
let main = () -> u8 => u8(pipeline(Some(4)))`)
		if errs := analyzeWith(t, entry, filepath.Dir(entry), dir, repo).Errors(); len(errs) != 0 {
			t.Errorf("a pure pipeline over the shipped combinators should check; got %v", errs)
		}
	})

	t.Run("impure callback is still rejected", func(t *testing.T) {
		entry := filepath.Join(t.TempDir(), "app.lyra")
		write(t, entry, `var log = 0
let sneaky = (x: i64) -> i64 => { log = x; x }
let pipeline = pure (m: Maybe<i64>) -> Maybe<i64> => m.map(sneaky)
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

// A module may declare a method for its own type under a name the prelude also uses.
//
// This is the case that broke on 08/04 and had no diagnostic: the prelude's `map` took the
// bare declaration key, the other module's moved to a qualified one, and the UFCS rung —
// which consulted a single candidate by name — reported "member access on non-struct type"
// for a receiver the other module's `map` accepted perfectly well. Two correct features
// composing into a method that silently was not there.
//
// Both calls must resolve: `Maybe` through the prelude, `Box` through the import. Different
// return types, so picking the wrong candidate fails rather than merely taking a different
// path to the same answer.
func TestShippedPrelude_ImportedModuleMayReusePreludeMethodName(t *testing.T) {
	repo, path := shippedPreludePath(t)
	std := filepath.Dir(filepath.Dir(path))

	app := t.TempDir()
	write(t, filepath.Join(app, "app.lyra"), `import util.box
let main = () -> u8 => {
  let m: Maybe<i64> = Some 3
  let b = Box { v: 4 }
  let viaPrelude: Maybe<i64> = m.map((x: i64) -> i64 => x * 2)
  let viaImport: i64 = b.map((x: i64) -> i64 => x * 5)
  u8(viaPrelude.unwrap_or(0) + viaImport)
}`)
	write(t, filepath.Join(app, "util", "box.lyra"), `module util.box
pub struct Box { v: i64 }
pub let map = pure (self: Box, f: (i64) -> i64) -> i64 => f(self.v)
`)

	if errs := analyzeWith(t, filepath.Join(app, "app.lyra"), app, std, repo).Errors(); len(errs) != 0 {
		t.Errorf("a module's own `map` and the prelude's should both resolve, each on its own receiver; got %v", errs)
	}
}

// The other half: an unimported module's same-named method stays unreachable. Gathering
// every candidate must not turn into "every pub function in the program is a method on its
// own type" — what a file may call is a property of its own import list.
func TestShippedPrelude_UnimportedModuleMethodStaysUnreachable(t *testing.T) {
	repo, path := shippedPreludePath(t)
	std := filepath.Dir(filepath.Dir(path))

	app := t.TempDir()
	write(t, filepath.Join(app, "app.lyra"), `let main = () -> u8 => {
  let b = Box { v: 4 }
  u8(b.scaled(2))
}`)
	write(t, filepath.Join(app, "util", "box.lyra"), `module util.box
pub struct Box { v: i64 }
pub let scaled = pure (self: Box, by: i64) -> i64 => self.v * by
`)

	errs := analyzeWith(t, filepath.Join(app, "app.lyra"), app, std, repo).Errors()
	if len(errs) == 0 {
		t.Fatal("a method from a module this file never imported should not be callable")
	}
}

// Two reachable candidates that both accept the receiver is genuinely ambiguous, and is
// reported rather than broken by visit order — the resolver gathers candidates from a map
// of modules, so "whichever came first" would not even be stable between runs.
//
// The message must name a qualifier the reader can actually type, so this asserts the
// suggested form as well as the refusal.
func TestShippedPrelude_AmbiguousMethodIsReported(t *testing.T) {
	repo, path := shippedPreludePath(t)
	std := filepath.Dir(filepath.Dir(path))

	app := t.TempDir()
	write(t, filepath.Join(app, "app.lyra"), `import util.dup
let main = () -> u8 => {
  let m: Maybe<i64> = Some 3
  u8(m.map((x: i64) -> i64 => x).unwrap_or(0))
}`)
	write(t, filepath.Join(app, "util", "dup.lyra"), `module util.dup
pub let map<t,u> = pure (self: Maybe<t>, f: (t) -> u) -> Maybe<u> => match self {
  Some v => Some(f(v)),
  None => None,
}
`)

	errs := analyzeWith(t, filepath.Join(app, "app.lyra"), app, std, repo).Errors()
	if len(errs) == 0 {
		t.Fatal("two modules defining `map` for Maybe should make `m.map(f)` ambiguous")
	}
	var joined strings.Builder
	for _, e := range errs {
		joined.WriteString(e.Message)
	}
	for _, want := range []string{"ambiguous", "std.prelude", "util.dup", "dup.map("} {
		if !strings.Contains(joined.String(), want) {
			t.Errorf("expected the ambiguity message to contain %q; got %s", want, joined.String())
		}
	}
}

// A file's *own* module wins a tie, which is the rule the scope chain applies everywhere
// else — so declaring your own `map` for Maybe shadows the prelude's rather than colliding
// with it. Pinned by return type: the local one returns i64, so resolving to the prelude's
// (which returns Maybe<u>) fails the annotation.
func TestShippedPrelude_LocalDeclarationWinsTheTie(t *testing.T) {
	repo, path := shippedPreludePath(t)
	std := filepath.Dir(filepath.Dir(path))

	app := t.TempDir()
	write(t, filepath.Join(app, "app.lyra"), `let map<t> = pure (self: Maybe<t>, f: (t) -> t) -> i64 => 7
let main = () -> u8 => {
  let m: Maybe<i64> = Some 3
  let mine: i64 = m.map((x: i64) -> i64 => x)
  u8(mine)
}`)

	if errs := analyzeWith(t, filepath.Join(app, "app.lyra"), app, std, repo).Errors(); len(errs) != 0 {
		t.Errorf("a file's own `map` should win over the prelude's; got %v", errs)
	}
}

// `unwrap` and `expect` on both types, and the overload set they form.
//
// `expect` takes the message as a `string` rather than interpolating the value: `t` is a
// type variable, and interpolation is checked against the types the backend can format, so
// `${value}` on a generic is not expressible until a Show-style capability exists. Passing
// the message moves the formatting to the caller, who has the concrete type — which is the
// property this pins, since it is the whole reason the signature is shaped that way.
func TestShippedPrelude_UnwrapAndExpect(t *testing.T) {
	repo, path := shippedPreludePath(t)
	std := filepath.Dir(filepath.Dir(path))

	app := t.TempDir()
	write(t, filepath.Join(app, "app.lyra"), `let main = () -> u8 => {
  let m: Maybe<i64> = Some 7
  let r: Result<i64, string> = Ok 9
  let name = "database"
  let viaUnwrap: i64 = m.unwrap() + r.unwrap()
  let viaExpect: i64 = m.expect("m must be set") + r.expect("config for ${name} is required")
  u8(viaUnwrap + viaExpect)
}`)

	if errs := analyzeWith(t, filepath.Join(app, "app.lyra"), app, std, repo).Errors(); len(errs) != 0 {
		t.Errorf("unwrap/expect should resolve on both receivers, with an interpolated caller message; got %v", errs)
	}
}

// Both are `pure noalloc`, so a `pure` function may call them: `panic` is EffectNone, and a
// trap is not an effect the system tracks. If that ever changes, the whole combinator layer
// stops being callable from pure code, so it is worth asserting rather than assuming.
func TestShippedPrelude_UnwrapIsCallableFromPureCode(t *testing.T) {
	repo, path := shippedPreludePath(t)
	std := filepath.Dir(filepath.Dir(path))

	app := t.TempDir()
	write(t, filepath.Join(app, "app.lyra"), `let force = pure (m: Maybe<i64>) -> i64 => m.expect("must be set")
let main = () -> u8 => u8(force(Some(4)))`)

	if errs := analyzeWith(t, filepath.Join(app, "app.lyra"), app, std, repo).Errors(); len(errs) != 0 {
		t.Errorf("a pure function should be able to call expect; got %v", errs)
	}
}

// The shipped prelude **lowers**, not merely type-checks.
//
// This is the gap that let a non-building prelude ship green. Every other test here stops
// at analysis (`driver.AnalyzeUnits`), and the backend's own tests use hand-written
// declarations rather than `std/`, so nothing ever asked the code generator to emit the
// real combinators. A prelude whose `unwrap` delegated to `expect` passed `lyrac check`
// and the entire suite, and failed only when someone ran `lyrac build` — "type variable t
// has no concrete type here", three layers from the edit that caused it.
//
// Emitting is the whole assertion; the IR is not inspected. What is being guarded is that
// every construct the prelude uses has a lowering at all, which is exactly what analysis
// cannot see.
func TestShippedPrelude_Lowers(t *testing.T) {
	repo, path := shippedPreludePath(t)
	std := filepath.Dir(filepath.Dir(path))

	app := t.TempDir()
	// Exercises each combinator, both receivers, and the generic-calls-generic chain
	// (`unwrap` is written in terms of `expect`), at a managed payload as well as a
	// scalar one — a specialization at `string` is where the ownership tables matter.
	write(t, filepath.Join(app, "app.lyra"), `let main = () -> u8 => {
  let m: Maybe<i64> = Some 7
  let r: Result<i64, string> = Ok 9
  let s: Maybe<string> = Some "hi"
  let a = m.unwrap() + r.unwrap()
  let b = m.expect("m") + r.expect("r")
  let c = m.unwrap_or(0) + r.unwrap_or(0) + m.unwrap_or_else(() -> i64 => 1)
  let d = m.map((x: i64) -> i64 => x).unwrap_or(0)
  let e = r.map((x: i64) -> i64 => x).unwrap_or(0)
  let f = m.flat_map((x: i64) -> Maybe<i64> => Some(x)).unwrap_or(0)
  let g = m.filter((x: i64) -> bool => true).unwrap_or(0)
  let h = if m.is_some() && r.is_ok() && m.is_none() == false { 1 } else { 0 }
  let i = m.ok_or("e").unwrap_or(0) + r.ok().unwrap_or(0)
  let j = s.unwrap()
  let ns: []i64 = [1, 2, 3]
  let k = ns.map((n: i64) -> i64 => n * 2).len()
  let l = ns.filter((n: i64) -> bool => n != 2).len()
  u8(a + b + c + d + e + f + g + h + i + k + l)
}`)

	units, diags := modules.Resolve(filepath.Join(app, "app.lyra"), []string{app, std, repo},
		modules.Options{Prelude: modules.PreludeModule})
	if len(diags) != 0 {
		t.Fatalf("resolve failed: %v", diags)
	}
	res := driver.AnalyzeUnits(units)
	if res.HasErrors() {
		t.Fatalf("the prelude should analyze cleanly: %v", res.Errors())
	}
	entry, epDiags := driver.ResolveEntryPoint(res)
	if entry == nil {
		t.Fatalf("no entry point: %v", epDiags)
	}
	if _, err := llvm.New().Emit(res, entry); err != nil {
		t.Fatalf("the shipped prelude does not lower: %v", err)
	}
}

// **A bare call resolves across modules the way a method call does.**
//
// The two spellings had different resolution machinery: `m.f(x)` gathers every reachable
// declaration and picks by receiver, while `f(m, x)` resolved a *name* through the scope
// chain — module → prelude → global — and stopped at the first hit. So with a `map` for
// `Box` in an imported module, `b.map(f)` resolved and `map(b, f)` did not, because the
// prelude's scope sits nearer than the global one the import exports into. Same call, same
// receiver, two answers.
//
// Both forms are exercised against the *imported* module and the *prelude* in one program,
// since the fix must not cost the prelude case its resolution.
func TestShippedPrelude_BareCallResolvesLikeAMethodCall(t *testing.T) {
	repo, path := shippedPreludePath(t)
	std := filepath.Dir(filepath.Dir(path))

	app := t.TempDir()
	write(t, filepath.Join(app, "app.lyra"), `import util.box
let main = () -> u8 => {
  let b = Box { v: 4 }
  let m: Maybe<i64> = Some 5
  let viaMethod = b.map((x: i64) -> i64 => x * 2)
  let viaCall = map(b, (x: i64) -> i64 => x * 2)
  let preludeMethod = m.map((x: i64) -> i64 => x).unwrap_or(0)
  let preludeCall = unwrap_or(map(m, (x: i64) -> i64 => x), 0)
  u8(viaMethod + viaCall + preludeMethod + preludeCall)
}`)
	write(t, filepath.Join(app, "util", "box.lyra"), `module util.box
pub struct Box { v: i64 }
pub let map = pure (self: Box, f: (i64) -> i64) -> i64 => f(self.v)
`)

	if errs := analyzeWith(t, filepath.Join(app, "app.lyra"), app, std, repo).Errors(); len(errs) != 0 {
		t.Errorf("both spellings should resolve, for the import and the prelude; got %v", errs)
	}
}

// A **singly-declared** name shadowed by the prelude's, which reported the resolution
// failure as a unification failure — `is_some: cannot infer type variable t from these
// arguments` — because the scope chain had already committed to the prelude's
// `is_some<t>(self: Maybe<t>)`.
func TestShippedPrelude_BareCallReachesASinglyDeclaredImport(t *testing.T) {
	repo, path := shippedPreludePath(t)
	std := filepath.Dir(filepath.Dir(path))

	app := t.TempDir()
	write(t, filepath.Join(app, "app.lyra"), `import util.box
let main = () -> u8 => {
  let b = Box { v: 4 }
  u8(if is_some(b) { 7 } else { 0 })
}`)
	write(t, filepath.Join(app, "util", "box.lyra"), `module util.box
pub struct Box { v: i64 }
pub let is_some = pure (self: Box) -> bool => self.v > 0
`)

	if errs := analyzeWith(t, filepath.Join(app, "app.lyra"), app, std, repo).Errors(); len(errs) != 0 {
		t.Errorf("a bare call should reach the imported declaration; got %v", errs)
	}
}

// The fallback runs **only after** the scope chain has failed, so a plain (non-receiver)
// function whose first argument does not fit is still an ordinary argument-type error.
// Dispatching there would turn a typo into a call to something else entirely.
func TestShippedPrelude_BareCallToAPlainFunctionStillErrors(t *testing.T) {
	repo, path := shippedPreludePath(t)
	std := filepath.Dir(filepath.Dir(path))

	app := t.TempDir()
	write(t, filepath.Join(app, "app.lyra"), `let scale = (n: i64, by: i64) -> i64 => n * by
let main = () -> u8 => u8(scale("not a number", 2))`)

	errs := analyzeWith(t, filepath.Join(app, "app.lyra"), app, std, repo).Errors()
	if len(errs) == 0 {
		t.Fatal("a wrong first argument to a plain function should still be an error")
	}
	if !strings.Contains(errs[0].Message, "cannot assign") {
		t.Errorf("expected an argument-type error, got %v", errs[0].Message)
	}
}
