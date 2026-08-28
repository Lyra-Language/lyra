package llvm

import "testing"

// A top-level binding holding a function *value* — a closure returned by a call, or a
// constructor-built newtype over a function type — is a global, and calling it is the
// same indirect call a local closure binding takes (08/28). The identifier-call ladder
// had arms for l.funcs, locals, conversions, specializations and overloads, and none
// for a global closure value, so `let add1 = mk(1)` at top level failed with `call to
// unknown function` while the identical binding inside a body worked.
func TestExec_TopLevelFunctionValuedBindingIsCallable(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
		want int
	}{
		{
			"a closure returned by a call",
			`let mk = (k: i64) -> (i64) -> i64 => (n: i64) -> i64 => n + k
			 let add1 = mk(1)
			 let main = () -> u8 => u8(add1(41))`,
			42,
		},
		{
			// The shape that surfaced the gap: a constructor-built function-type
			// newtype at top level, called directly.
			"a constructor-built function-type newtype",
			`newtype Handler = (i64) -> i64
			 let h: Handler = Handler((n: i64) -> i64 => n + 1)
			 let main = () -> u8 => u8(h(41))`,
			42,
		},
		{
			// The arm sits before the builtin switch, matching the typechecker's
			// resolution order: a user binding shadows a builtin.
			"a global closure shadowing a builtin name",
			`let mk = (k: i64) -> (i64) -> i64 => (n: i64) -> i64 => n + k
			 let print = mk(1)
			 let main = () -> u8 => u8(print(41))`,
			42,
		},
		{
			// A `var` global rebinds like a local: the call reads whatever the slot
			// holds at that moment.
			"a var global, rebound between calls",
			`let mk = (k: i64) -> (i64) -> i64 => (n: i64) -> i64 => n + k
			 var f = mk(1)
			 let main = () -> u8 => {
			   let a = f(1)
			   f = mk(10)
			   u8(a + f(1) + f(2))
			 }`,
			25, // 2 + 11 + 12
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := buildAndRun(t, c.src); got != c.want {
				t.Errorf("%s: exited %d; want %d", c.name, got, c.want)
			}
		})
	}
}

// Two modules may each have a private top-level data binding of one name: the global's
// slot is keyed and named per module (funcKey resolution, rule 9's arrangement), like a
// function's. Keyed by bare name, the second module's slot overwrote the first's map
// entry and clang refused the duplicate `lyra_global_stash` symbol — the same disease
// l.funcs and l.structTypes were each cured of.
func TestExec_PrivateGlobalsInTwoModulesGetDistinctSlots(t *testing.T) {
	got := buildAndRunModules(t, map[string]string{
		"one.lyra": `module one
let stash = "a" ++ "b"
pub let get_one = () -> i64 => stash.len()`,
		"two.lyra": `module two
let stash = "cde" ++ "fg"
pub let get_two = () -> i64 => stash.len()`,
		"app.lyra": `import one.{ get_one }
import two.{ get_two }
let main = () -> u8 => u8(get_one() + get_two())`,
	})
	if got != 7 {
		t.Errorf("expected 2 + 5 = 7, got %d", got)
	}
}
