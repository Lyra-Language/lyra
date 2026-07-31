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
	if err != nil {
		t.Fatalf("emit: %v", err)
	}

	runErr := exec.Command(compileCached(t, clang, string(ir))).Run()
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
