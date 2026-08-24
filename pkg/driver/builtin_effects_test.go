package driver_test

import (
	"strings"
	"testing"

	"github.com/Lyra-Language/lyra/pkg/driver"
)

// Every key in the purity pass's builtinEffects table must name a builtin a program can
// actually call.
//
// Nine did not, and were removed 08/24: `fmt.print`, `fmt.println`, `read`, `write`,
// `megabytes`, `kilobytes`, `bytes`, `Arena.new`, `Arena.alloc`. The last two named a form
// the language refuses outright — there are no type-namespaced associated functions
// (lyra-E035) — so no spelling of them could ever have reached the table.
//
// None was exploitable: both lookup sites consult the table only after `resolveCallee`
// fails, and resolution reaches imported and namespaced callees. They mattered anyway,
// because a name that resolves to nothing otherwise lands on the conservative AllEffects
// default and a phantom **intercepts** it — five of the nine were EffectNone, the
// difference between "assume the worst" and "certified pure". The ordering that made them
// harmless is one refactor from not holding, and this table has been on the wrong side of
// exactly that ordering before.
//
// They also kept a test green that could not otherwise have passed: an arena purity test
// over a program with three compile errors, certified by two builtins that do not exist.
//
// The table is unexported, so this test asserts the invariant from the outside — each name
// is called from a real program and must resolve. There is no builtin *registry* to compare
// against (each is its own `isBuiltinXFn` predicate in the typechecker), which is precisely
// why the table could drift from reality unnoticed.
func TestBuiltinEffects_EveryKeyNamesACallableBuiltin(t *testing.T) {
	// One call per key, in the shape the builtin actually takes.
	for _, c := range []struct{ key, call string }{
		{"print", `print("x")`},
		{"println", `println("x")`},
		{"panic", `panic("x")`},
		{"read_line", `read_line()`},
		{"set_raw_mode", `set_raw_mode(true)`},
		{"read_key", `read_key()`},
		{"terminal_size", `terminal_size()`},
		{"wait_for_key_ms", `wait_for_key_ms(10)`},
		{"random_seed", `random_seed()`},
		{"wall_clock_nanos", `wall_clock_nanos()`},
	} {
		t.Run(c.key, func(t *testing.T) {
			res := driver.Analyze([]byte("let main = () -> void => {\n  " + c.call + "\n}\n"))
			for _, d := range res.Errors() {
				// The failure that matters is the name not existing. Anything else (a
				// void-in-value-position complaint, say) is this test's fixture being
				// clumsy, not the key being a phantom.
				if strings.Contains(d.Message, "undefined") ||
					strings.Contains(d.Message, "unknown") ||
					strings.Contains(d.Message, "cannot call") {
					t.Errorf("builtinEffects key %q does not name a callable builtin: %s",
						c.key, d.Message)
				}
			}
		})
	}
}
