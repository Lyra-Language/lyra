package llvm

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/Lyra-Language/lyra/pkg/driver"
	"github.com/Lyra-Language/lyra/pkg/modules"
)

// buildAndRunModules is buildAndRun (llvm_test.go) for a *multi-module* program. The
// single-source harness goes through driver.Analyze, which resolves no import graph, so
// until now nothing in this package could exercise a call across modules at all — which
// is why a namespace call to a generic function reached `lyrac build` unlowered. `files`
// is written to a temp dir; `app.lyra` is the entry.
func buildAndRunModules(t *testing.T, files map[string]string) int {
	t.Helper()
	clang := lookClang(t)
	return exitCodeOf(t, exec.Command(compileCached(t, clang, emitModules(t, files))).Run())
}

// emitModules is buildAndRunModules' front half: resolve, analyze and emit, returning the
// IR text. Split out for a test whose question is about what was *emitted* rather than
// what the program does — a foreign symbol declared by two modules must produce one
// `declare`, which running the program cannot show.
func emitModules(t *testing.T, files map[string]string) string {
	t.Helper()
	ir, err := emitModulesErr(t, files)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	return ir
}

// emitModulesErr hands the backend's error back instead of failing the test, for the
// cases whose subject *is* the refusal.
func emitModulesErr(t *testing.T, files map[string]string) (string, error) {
	t.Helper()
	root := t.TempDir()
	for name, body := range files {
		path := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	units, diags := modules.Resolve(filepath.Join(root, "app.lyra"), []string{root}, modules.Options{})
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

// exitCodeOf is the exit status of a finished command, with a non-zero exit reported as
// the code rather than as an error (it arrives as *exec.ExitError).
func exitCodeOf(t *testing.T, runErr error) int {
	t.Helper()
	if runErr == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(runErr, &ee) {
		return ee.ExitCode()
	}
	t.Fatalf("running the binary failed: %v", runErr)
	return -1
}

const optModule = `module util.opt
pub data Opt<t> = Nil | One(t)
pub let wrap<t> = (v: t) -> Opt<t> => One(v)
pub let unwrap<t> = (o: Opt<t>, fallback: t) -> t => match o {
  One(v) => v,
  Nil => fallback,
}
pub let double = (n: i64) -> i64 => n * 2
`

// A *generic* function called through an imported namespace lowers to the specialization
// the typechecker solved for that call site — the same resolution the by-name path does.
// The namespace path looked only in l.funcs, which holds the functions emitted as
// themselves; a generic function is not one of those (a type variable has no
// representation), so it found nothing and the call died as `unsupported method call
// "unwrap"` after type-checking cleanly.
//
// Two distinct type arguments, so the test also pins that each call site gets *its own*
// specialization rather than one shared function: 7 (i64) + 2 (u8) = 9.
func TestExec_GenericCallThroughNamespace(t *testing.T) {
	t.Parallel()
	got := buildAndRunModules(t, map[string]string{
		"util/opt.lyra": optModule,
		"app.lyra": `import util.opt
let main = () -> u8 => {
  let n = opt.unwrap(opt.wrap(7), 0)
  let m = opt.unwrap(opt.wrap(u8(2)), u8(1))
  u8(n) + m
}`,
	})
	if got != 9 {
		t.Errorf("exit code: got %d, want 9", got)
	}
}

// The non-generic namespace call still lowers the way it did — the generic lookup was
// added ahead of it, so this is the case that would break if the two were confused.
func TestExec_NonGenericCallThroughNamespace(t *testing.T) {
	t.Parallel()
	got := buildAndRunModules(t, map[string]string{
		"util/opt.lyra": optModule,
		"app.lyra": `import util.opt
let main = () -> u8 => u8(opt.double(3))`,
	})
	if got != 6 {
		t.Errorf("exit code: got %d, want 6", got)
	}
}

// A name overloaded on its receiver, called through the namespace: each call picks its
// member by the first argument, as `x.scaled(…)` and an imported `scaled(x, …)` do. The
// set is kept out of the function table, so until 10/02 the namespace lookup found no
// such member ("module has no member") — `std.genesis.collision.mirrored` gained a
// `Collider` overload and every `collision.mirrored(shape, w)` stopped compiling. One
// member generic in another parameter (as `hits_map` is), so the specialization path is
// crossed: 2*3 + 10 + 4 = 20.
func TestExec_OverloadedCallThroughNamespace(t *testing.T) {
	t.Parallel()
	got := buildAndRunModules(t, map[string]string{
		"util/shape.lyra": `module util.shape
pub struct Box { side: i64 }
pub struct Pair { a: i64, b: i64 }
pub let scaled = pure (self: Box, k: i64) -> i64 => self.side * k
pub let scaled = pure (self: Pair, k: i64) -> i64 => (self.a + self.b) * k
pub let total<const N: i64> = pure (self: Pair, more: ref [N]i64) -> i64 => {
  var sum = self.a + self.b
  for c in more { sum += c }
  sum
}
pub let total = pure (self: Box) -> i64 => self.side
`,
		"app.lyra": `import util.shape
import util.shape.{ Box, Pair }
let main = () -> u8 => {
  let more: [2]i64 = #[1, 3]
  u8(shape.scaled(Box { side: 2 }, 3) + shape.scaled(Pair { a: 2, b: 3 }, 2) + shape.total(Pair { a: 0, b: 0 }, more))
}`,
	})
	if got != 20 {
		t.Errorf("exit code: got %d, want 20", got)
	}
}

// A **private** function taking a `mut` parameter, called from inside its own module.
//
// Its parameters were looked up under the bare source name while they had been recorded
// under the module-qualified key a private declaration gets, so the call site read an
// empty parameter list — and with no parameters to consult, paramIsByRef is never asked
// and the argument is passed *by value* instead of by address. The mutation then lands on
// a copy, so the write the caller is waiting for never arrives. Nothing reports it: the
// arity guard is skipped by the same empty list.
func TestExec_PrivateMutParamPassedByReference(t *testing.T) {
	t.Parallel()
	got := buildAndRunModules(t, map[string]string{
		"app.lyra": `import util.counter
let main = () -> u8 => u8(counter.run())
`,
		"util/counter.lyra": `module util.counter
struct Box { n: i64 }
let bump = (b: mut Box) -> void => { b.n = b.n + 1 }
pub let run = () -> i64 => {
  var b = Box { n: 41 }
  bump(b)
  b.n
}
`,
	})
	if got != 42 {
		t.Errorf("expected 42 — the private callee's `mut` write should reach the caller's Box, got %d", got)
	}
}

// A file may declare its own version of a name an imported module exports; the local one
// wins a bare call and the imported one is still reached through its namespace. This
// exercises both from the backend's side, where each half was resolved *by name*: the
// membership test through DeclaringModule (last-writer-wins, so it answered with the
// entry file's declaration and rejected the call) and the callee through `l.funcs[name]`
// (which holds the imported one under a key the bare name no longer computes).
//
// The result separates the two: 3 * 10 = 30 from the local `scale`, 4 * 100 = 400 from
// the imported one, and 430 % 256 = 174 as an exit code. Either half resolving to the
// other function gives a different number rather than a failure to build.
func TestExec_LocalDeclarationShadowsImportedName(t *testing.T) {
	t.Parallel()
	got := buildAndRunModules(t, map[string]string{
		"util/scale.lyra": "module util.scale\npub let amplify = (n: i64) -> i64 => n * 100",
		"app.lyra": `import util.scale
let amplify = (n: i64) -> i64 => n * 10
let main = () -> u8 => u8((amplify(3) + scale.amplify(4)) %% 256)`,
	})
	if got != 174 {
		t.Errorf("exit code: got %d, want 174 (30 from the local amplify, 400 from the imported one)", got)
	}
}

// The same, for a name the **prelude** exports. This path was already broken before a
// local declaration over an *imported* name was allowed — a prelude shadow has qualified
// the shadowing declaration's key since 07/30, so `l.funcs[name]` missed here too — but
// it took a program that shadowed a prelude name *and* called into a module through a
// namespace, which nothing did. Pinned separately because the two shadow sources reach
// the same key rule by different routes.
func TestExec_PreludeShadowDoesNotBreakANamespaceCall(t *testing.T) {
	t.Parallel()
	got := buildAndRunModules(t, map[string]string{
		"util/seq.lyra": "module util.seq\npub let count = (n: i64) -> i64 => n * 100",
		"app.lyra": `import util.seq
let print = (n: i64) -> i64 => n * 10
let main = () -> u8 => u8((print(3) + seq.count(2)) %% 256)`,
	})
	if got != 230 {
		t.Errorf("exit code: got %d, want 230", got)
	}
}

// TestExec_AnExportedStructBesideAPrivateOneOfItsName reads a value of `model`'s exported
// `Animation` inside `rom`, which declares a private `Animation` of its own. The exported
// type carried no declaration key — it was taken to be program-wide — so in `rom` it was laid
// out as `rom`'s struct: `steps` read an i64 field where the exported one has a `[]i64`, and
// the backend panicked building a `.len()` on it (09/30, Vega). A name declared twice is
// keyed now, both declarations of it.
func TestExec_AnExportedStructBesideAPrivateOneOfItsName(t *testing.T) {
	t.Parallel()
	code := buildAndRunModules(t, map[string]string{
		"model.lyra": `module model

pub struct Animation {
  name: string,
  steps: []i64,
}

pub struct Sprite {
  animations: []Animation,
}
`,
		"rom.lyra": `module rom

import model.{ Sprite }

/// Another struct of the same name, private to this module.
struct Animation {
  sprite: i64,
  steps: i64,
}

pub let total = (s: Sprite) -> i64 => {
  let local = Animation { sprite: 7, steps: 10 }
  s.animations[0].steps.len() * 100 + s.animations[0].steps[2] + local.steps + local.sprite
}
`,
		"app.lyra": `import model.{ Sprite, Animation }
import rom

let main = () -> u8 => {
  let s = Sprite { animations: [Animation { name: "walk", steps: [3, 4, 5] }] }
  u8(rom.total(s) - 200)
}
`,
	})
	// 3 steps × 100 + steps[2] 5 + 10 + 7 = 322; less 200 is 122.
	if code != 122 {
		t.Errorf("exit %d, want 122", code)
	}
}
