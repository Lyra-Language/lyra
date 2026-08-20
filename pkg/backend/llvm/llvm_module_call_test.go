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
