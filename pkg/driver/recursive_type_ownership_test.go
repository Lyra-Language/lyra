package driver_test

import (
	"strings"
	"testing"

	"github.com/Lyra-Language/lyra/pkg/driver"
)

// **A refused program does not reach the ownership pass**, which is a soundness rule
// rather than a saving.
//
// `ownership.go` states the invariant it rests on — "a recursive type's cycle must pass
// through a `shared` field (lyra-E014), which is managed, so the recursion returns before
// re-entering the cycle". A program `lyra-E014` has just refused is precisely one where
// that does not hold, and running the pass anyway walked the cycle until the stack was
// gone: `ownsManaged` → `eachComponent` → `resolveNamedType`, forever.
//
// The compounding part is what this test is really for. The diagnostic had **already been
// produced** by the time the pass ran, and a process that dies prints nothing — so the
// compiler reported `fatal error: stack overflow` while holding the right answer. The
// cycle itself (a generic argument, `Maybe<Expr>`) is checker's to catch and is tested
// there; this pins that a program carrying the error survives to report it.
//
// A stack overflow is not recoverable, so a regression here fails by taking the test binary
// down rather than by a message. That is still a failure, and it is the only signal
// available for this class of fault.
func TestAnalyze_ARefusedRecursiveTypeStillReportsItsDiagnostic(t *testing.T) {
	t.Parallel()
	// A generic declared here rather than the prelude's `Maybe`: driver.Analyze runs
	// without a prelude, and the rule is about any generic's argument, not that type.
	const src = `data Opt<t> = Nothing | Just(t)
data Expr = Lit(i64) | Lam(Lambda)
struct Lambda { body: Opt<Expr> }
let main = () -> u8 => {
  let e = Lam(Lambda { body: Just(Lit(1)) })
  0
}`
	res := driver.Analyze([]byte(src))
	if !res.HasErrors() {
		t.Fatalf("a by-value cycle through Maybe must be refused; got %v", res.Diagnostics)
	}
	var found bool
	for _, d := range res.Diagnostics {
		if d.Code == "lyra-E014" && strings.Contains(d.Message, "recursive without indirection") {
			found = true
		}
	}
	if !found {
		t.Errorf("want the recursive-type diagnostic to survive the analysis; got %v", res.Diagnostics)
	}
	// The pass that would have hung is skipped, not merely survived: its table is absent
	// rather than half-built, so nothing downstream can read a partial answer as a whole one.
	if res.Ownership != nil {
		t.Errorf("ownership analysed a program that was already refused")
	}
}

// The same program with the cycle broken still gets its ownership table — the skip is
// conditioned on errors, not on the shape.
func TestAnalyze_AnAcceptedProgramStillGetsItsOwnershipTable(t *testing.T) {
	t.Parallel()
	const src = `data Opt<t> = Nothing | Just(t)
data Expr = Lit(i64) | Lam(Lambda)
struct Lambda { body: Opt<shared Expr> }
let main = () -> u8 => {
  let e = Lam(Lambda { body: Just(Lit(1)) })
  0
}`
	res := driver.Analyze([]byte(src))
	if res.HasErrors() {
		t.Fatalf("`shared` breaks the cycle and this must be accepted; got %v", res.Errors())
	}
	if res.Ownership == nil {
		t.Errorf("an accepted program must still be analysed for ownership")
	}
}
