package llvm

import (
	"strings"
	"testing"
)

// A module's function named as a value — bound, passed, stored — is the
// function as a closure, as a bare name is (namedFunctionValue). Until 10/19 `helper.twice`
// in value position fell through to field access and failed in the backend, "no type
// recorded for member-access object", on a program the front end had checked clean. (The
// todo entry it was found beside — a module-qualified *call* inside a lambda — had already
// been fixed by the captures pass.)
func TestExec_AModulesFunctionAsAValue(t *testing.T) {
	t.Parallel()
	got := buildAndRunModules(t, map[string]string{
		"helper.lyra": "module helper\npub let twice = (x: i64) -> i64 => x * 2\n",
		"app.lyra": `import helper
import helper as hp
let apply_to = (f: (i64) -> i64, v: i64) -> i64 => f(v)
let main = () -> u8 => {
  let h = helper.twice
  let fs = [helper.twice, hp.twice]
  u8(h(5) + apply_to(helper.twice, 6) + fs[1](7))
}
`,
	})
	// 10 + 12 + 14
	if got != 36 {
		t.Errorf("exit = %d, want 36", got)
	}
}

// A `?.` refused on a Result is one error: its arms are the collector's and go unchecked,
// and are published as such, so purity no longer reported the desugaring's own binding as
// "pure function calls impure function `__opt_2_42.len`" beside lyra-E083 (10/19).
func TestCheck_ARefusedOptionalChainIsOneError(t *testing.T) {
	t.Parallel()
	errs := checkWithPrelude(t, `
module main
let make = pure () -> Result<[]u8, string> => Ok([1, 2])
let n = pure () -> Maybe<i64> => make()?.len()
let main = () -> void => {}
`)
	if len(errs) != 1 || !strings.Contains(errs[0], "read through a Maybe") {
		t.Fatalf("want only the E083 refusal, got %v", errs)
	}
}
