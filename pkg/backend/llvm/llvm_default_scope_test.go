package llvm

import (
	"os"
	"os/exec"
	"testing"
)

const shapesModule = `module shapes

pub struct Box {
  w: i64,
  h: i64,
}

const UNIT: Box = Box { w: 1, h: 2 }

const SECRET: i64 = 7

let factor: i64 = 3

// Built at run time, so a release too many frees it (a literal is never freed).
let label: string = "mod${"ule"}"

pub struct Holder {
  a: Box = UNIT,
  c: i64 = SECRET,
  n: i64 = factor,
  text: string = label,
}

pub let scaled = pure (n: i64, by: i64 = factor) -> i64 => n * by

pub let tag = (s: string = label) -> string => "${s}!"

pub let module_label = () -> string => label
`

// A default argument or field default names its declaring module's top level, wherever it
// is filled in (10/01). Read in the caller's scope it was "undefined identifier" for a
// name the caller does not import — a private const above all — and, once the typechecker
// read it in the right module, the backend still lowered a bare name against the caller's
// locals first, so a caller's own `factor` replaced the module's. Each answer is checked
// against what the module says, with the caller holding locals of the same names.
func TestExec_ADefaultIsReadInItsDeclaringModule(t *testing.T) {
	t.Parallel()
	got := buildAndRunModules(t, map[string]string{
		"shapes.lyra": shapesModule,
		"app.lyra": `import shapes.{ Holder, scaled, tag }

let main = () -> u8 => {
  let factor = 100
  let label = "the caller's"
  var held: []Holder = []
  for _ in 0..<3 { held.push(Holder {}) }
  let h = held[2]
  if h.a.w != 1 || h.a.h != 2 { return 1 }
  if h.c != 7 { return 2 }
  if h.n != 3 { return 3 }
  if h.text != "module" || held[0].text != "module" { return 4 }
  if scaled(2) != 6 { return 5 }
  if tag() != "module!" { return 6 }
  if factor != 100 || label != "the caller's" { return 7 }
  0
}
`,
	})
	if got != 0 {
		t.Errorf("exit %d: check %d read a default in the caller's scope", got, got)
	}
}

// buildAndRunModulesASan is buildAndRunModules under AddressSanitizer, its functions
// instrumented as buildAndRunASan's are.
func buildAndRunModulesASan(t *testing.T, files map[string]string) int {
	t.Helper()
	clang := lookClang(t)
	cmd := exec.Command(compileCached(t, clang, instrumentForASan(emitModules(t, files)), "-fsanitize=address"))
	cmd.Env = append(os.Environ(), asanOptions())
	asanRunSlots <- struct{}{}
	defer func() { <-asanRunSlots }()
	return exitCodeOf(t, cmd.Run())
}

// The ownership side of the same rule (10/01): Perceus finds a binding's last use by name,
// and a filled-in default's `label` was taken for the caller's local `label` — its last
// mention here — so the literal "moved" the local in and retained nothing, while what it
// actually read was the module's string. Dropping the struct then released the module's
// string, and module_label() read it freed.
func TestExec_AManagedDefaultIsNotACallersLocal(t *testing.T) {
	t.Parallel()
	got := buildAndRunModulesASan(t, map[string]string{
		"shapes.lyra": shapesModule,
		"app.lyra": `import shapes.{ Holder, module_label }

let main = () -> u8 => {
  let label = "${7}: the caller's"
  if read_one() != 3 { return 1 }
  if module_label() != "module" { return 2 }
  0
}

/// A Holder made and dropped, the module's label in it.
let read_one = () -> i64 => {
  let label = "${8}: another"
  let h = Holder {}
  h.n
}
`,
	})
	if got != 0 {
		t.Errorf("exit %d", got)
	}
}
