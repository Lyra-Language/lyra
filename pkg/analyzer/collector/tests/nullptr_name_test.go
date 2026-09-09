package collector_test

import (
	"strings"
	"testing"

	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
)

// lyra-E070 — `nullptr` may not be bound as a name.
//
// **The grammar cannot refuse it, which is why this rule exists here.** tree-sitter lexes
// against the tokens valid in the current parse state, so in *name* position `nullptr` is
// an ordinary identifier and `let nullptr = 5` parses as a perfectly ordinary binding. It
// is the *reads* that then break: every later mention sits in value position, where the
// literal is valid, so the binding can never be referred to again. Left alone, the program
// fails at the use with a message about pointers, for a binding holding an integer.
//
// This is the same context-sensitivity that keeps `let type = 5` and `let extern = 5`
// legal, which is deliberate — those words are keywords only in *declaration* position, so
// the binding they make is still readable. `nullptr` is the one where it is not, and that
// is what earns it a rule the others do not need.
func TestNullPtr_CannotBeBoundAsAName(t *testing.T) {
	errors := parseAndCollectErrors(t, `
		let main = () -> void => {
		  let nullptr = 5
		  println(nullptr)
		}
	`)
	found := false
	for _, e := range errors {
		if d, ok := e.(diag.Diagnostic); ok && d.Code == diag.CodeReservedName {
			found = true
			if !strings.Contains(d.Message, "cannot be used as a name") {
				t.Errorf("lyra-E070 message does not name the problem: %q", d.Message)
			}
		}
	}
	if !found {
		t.Errorf("expected lyra-E070 for a binding named nullptr, got: %v", errors)
	}
}

// `var` and `const` take the same rule: the hazard is the *name*, not the binding kind.
func TestNullPtr_CannotBeBoundAsAVarOrConst(t *testing.T) {
	for _, kind := range []string{"var", "const"} {
		errors := parseAndCollectErrors(t, "\n"+kind+" nullptr = 5\n")
		found := false
		for _, e := range errors {
			if d, ok := e.(diag.Diagnostic); ok && d.Code == diag.CodeReservedName {
				found = true
			}
		}
		if !found {
			t.Errorf("%s nullptr: expected lyra-E070, got: %v", kind, errors)
		}
	}
}
