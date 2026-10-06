package driver_test

import (
	"strings"
	"testing"
)

// A call to a top-level function is scored as **the calling module's** function of that
// name, not whichever same-named function the merged program walked last.
//
// The purity pass resolved a callee the typechecker had not recorded (every call but an
// overloaded one) through the capture stack, whose top-level frame is keyed by bare name
// over the merged program — rule 4's last-writer-wins. Two modules each with a private
// `alu` shared one entry. Found by Sheliak: its 68000 core's `pure` helpers call their own
// `pure alu`, and the moment its Z80 core (with an impure `alu`) joined the build they were
// refused. The same collision the other way round is a soundness hole: a `pure` function
// calling its own impure `alu` passed if the other module's pure `alu` was the one kept.
func TestPurity_CalleeResolvesInTheCallingModule(t *testing.T) {
	const pureAlu = `module one
pub let alu = pure (x: i64) -> i64 => x + 1
pub let use_one = pure (x: i64) -> i64 => alu(x)
`
	const impureAlu = `module two
let alu = (x: i64) -> i64 => {
  println("impure")
  x
}
pub let use_two = (x: i64) -> i64 => alu(x)
`
	const main = `module main
import one.{ use_one }
import two.{ use_two }
let main = () -> void => println("${use_one(1)} ${use_two(2)}")
`
	t.Run("a pure caller of its own pure alu is clean", func(t *testing.T) {
		res := analyzeFiles(t, map[string]string{"one.lyra": pureAlu, "two.lyra": impureAlu, "main.lyra": main})
		for _, d := range res.Errors() {
			if strings.Contains(d.Message, "impure") {
				t.Errorf("one.use_one was charged two's impure alu: %s", d.Message)
			}
		}
	})
	t.Run("a pure caller of its own impure alu is refused", func(t *testing.T) {
		impureCaller := strings.Replace(impureAlu, "pub let use_two = (", "pub let use_two = pure (", 1)
		res := analyzeFiles(t, map[string]string{"one.lyra": pureAlu, "two.lyra": impureCaller, "main.lyra": main})
		var found bool
		for _, d := range res.Errors() {
			if strings.Contains(d.Message, `pure function calls impure function "alu"`) {
				found = true
			}
		}
		if !found {
			t.Errorf("two.use_two calls its own impure alu from `pure` and was not refused; diagnostics: %v",
				res.Diagnostics)
		}
	})
}

// A namespace call is resolved in its module, whatever the caller's locals are named:
// `pad.held(held, …)` inside a function with a parameter `held` found the parameter through
// the capture stack and charged the call every effect — a `pure` caller refused, and on the
// Genesis the function reported as allocating (10/06, Vega's platformer).
func TestPurity_ANamespaceCallIsNotHiddenByALocalOfTheMembersName(t *testing.T) {
	const pad = `module pad
pub let held = pure (buttons: u16, button: u16) -> bool => (buttons & button) != 0
`
	const main = `module main
import pad
let run = pure (held: u16) -> bool => pad.held(held, 1)
let main = () -> void => println("${run(1)}")
`
	res := analyzeFiles(t, map[string]string{"pad.lyra": pad, "main.lyra": main})
	for _, d := range res.Errors() {
		t.Errorf("unexpected: %s", d.Message)
	}
}
