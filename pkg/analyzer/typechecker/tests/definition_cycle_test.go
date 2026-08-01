package typechecker_test

import (
	"strings"
	"testing"
)

// A binding whose type comes from its initializer can reach itself, and that used to
// recurse until the Go stack was exhausted and **the process died** — `lyrac` printing a
// runtime traceback instead of a diagnostic, and `lyra-lsp` (same `driver.Analyze`
// pipeline) vanishing mid-keystroke, which is precisely when a half-written cycle exists.
//
// These are ordinary programs, not malformed ones: the crash was first found through error
// recovery on a curried call, but it reduces to two lines with no syntax error at all. So
// the guard is in `inferExprType`, the single entry point every recursion goes through,
// rather than in the call path that happened to expose it.
//
// The assertion is that a *diagnostic* comes back. A test that merely ran the checker
// would prove nothing here — the failure mode was the test binary dying, which no
// assertion survives to report.
func TestDefinitionCycle_SelfReferentialBinding(t *testing.T) {
	res := parseCollectAndCheck(t, `let f = f(1)`, false)
	assertHasErrorContaining(t, res, "its definition depends on itself")
}

func TestDefinitionCycle_MutuallyReferentialBindings(t *testing.T) {
	res := parseCollectAndCheck(t, `let a = b(1)
let b = a(1)`, false)
	assertHasErrorContaining(t, res, "its definition depends on itself")
}

func TestDefinitionCycle_ThreeWay(t *testing.T) {
	res := parseCollectAndCheck(t, `let a = b(1)
let b = c(1)
let c = a(1)`, false)
	assertHasErrorContaining(t, res, "its definition depends on itself")
}

// The guard must not catch a binding that merely *calls* something to get its value —
// `let add5 = makeAdder(5)` infers its initializer exactly the same way, and is the case
// a too-broad guard would break by returning nil for a perfectly determinable type.
func TestDefinitionCycle_IndirectCallBindingStillResolves(t *testing.T) {
	res := parseCollectAndCheck(t, `let makeAdder = (n: i64) -> (i64) -> i64 => (x: i64) -> i64 => x + n
let add5 = makeAdder(5)
let use = () -> i64 => add5(3)`, false)
	assertNoErrors(t, res)
}

// And a genuinely non-callable binding keeps its own diagnostic, with the type named.
// That message used to be shared with the cycle case, which reached it holding a nil type
// and rendered `identifier "f" is not callable (type %!s(<nil>))` — a Go format verb
// leaking into a user-facing diagnostic.
func TestDefinitionCycle_NonCallableKeepsItsOwnMessage(t *testing.T) {
	res := parseCollectAndCheck(t, `let n = 5
let use = () -> i64 => n(1)`, false)
	assertHasErrorContaining(t, res, `identifier "n" is not callable (type i64)`)
	for _, e := range res.errors {
		if strings.Contains(e.Message, "%!") {
			t.Errorf("a format verb leaked into a diagnostic: %s", e.Message)
		}
	}
}
