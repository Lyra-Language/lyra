package typetable

import (
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// ConstraintTable records the expressions that must have their newtype's
// constraints checked **at run time**, and against which newtype.
//
// A `where` constraint was a compile-time assertion and nothing else until 08/13:
// it caught a literal, and anything the value-range pass could pin to an interval,
// and silently accepted everything else. So
//
//	let mk = (n: u8) -> Percent => Percent(n)
//	mk(200)     // built, ran, printed 200
//
// on `newtype Percent = u8 where range(0..<=100)`. That left the language's own
// ladder — provable → compile error, otherwise → trap — with a first rung and no
// second, in the one construct whose entire purpose is to be checked. The values
// that reach a constrained newtype at run time are exactly the ones from outside
// the program (parsed input, computed results), which is where a range or unit
// mistake actually lives.
//
// **The typechecker decides, the backend emits.** Only the typechecker knows which
// values it managed to verify statically, so it publishes the sites that still need
// a check rather than having codegen re-derive "is this a construction, and is it
// provable" — the same rule the method, callee and instantiation tables follow (see
// lyra/CLAUDE.md's hazard 9). A site verified at compile time is *not* recorded, so
// no check is emitted for it.
//
// The key is the value expression, not the constructor call: most positions a
// constrained value arrives in have no constructor node at all (an annotated
// binding, an argument, a return, an array element), and the value is the one node
// every position shares.
type ConstraintTable struct {
	checks map[ast.Expression]*types.ConstrainedType
}

func NewConstraintTable() *ConstraintTable {
	return &ConstraintTable{checks: map[ast.Expression]*types.ConstrainedType{}}
}

// Require records that expr must satisfy ct's constraints at run time.
func (t *ConstraintTable) Require(expr ast.Expression, ct *types.ConstrainedType) {
	if t == nil || expr == nil || ct == nil {
		return
	}
	t.checks[expr] = ct
}

// Get returns the newtype whose constraints expr must satisfy at run time, if any.
// Nil-receiver-safe, so a backend running without a typechecker pass (a test that
// lowers a hand-built program) simply emits no checks.
func (t *ConstraintTable) Get(expr ast.Expression) (*types.ConstrainedType, bool) {
	if t == nil {
		return nil, false
	}
	ct, ok := t.checks[expr]
	return ct, ok
}

// Len is the number of recorded sites, for tests that assert a check was or was not
// scheduled without reaching into the map.
func (t *ConstraintTable) Len() int {
	if t == nil {
		return 0
	}
	return len(t.checks)
}
