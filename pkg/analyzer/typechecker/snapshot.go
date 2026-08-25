package typechecker

import (
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/ast/symbols"
	"github.com/Lyra-Language/lyra/pkg/typetable"
)

// Snapshot is what a typechecking run produced for some prefix of a program's statements,
// held so the next run can start from it instead of re-deriving it.
//
// **`Check` is a handful of whole-program setup passes and then a per-statement loop**, and
// the loop is where the time goes: measured over the real prelude it is ~1.0 ms of a 2.7 ms
// analysis, while impl collection, coherence, newtype cycles and trait defaults together are
// under 1%. That split is what makes this tractable — the setup passes simply re-run over
// the whole program, so a snapshot holds only what the *loop* accumulates, and the
// TypeChecker's other thirty-odd fields are either rebuilt by those passes (declOfFunc,
// circularNewtypes, traitImpls) or are caches that cost nothing to lose (resolvedTypes).
//
// The reason the loop can be skipped for the prefix at all is that **the prelude cannot see
// user code**: a module resolves through its own scope, then its imports, then the prelude,
// and never downward. So checking a prelude statement gives the same answer whatever the
// user is typing. Where a user's type *does* reach prelude code — a generic instantiated at
// it, an impl of a prelude trait — it is discovered while checking the user's own statements,
// or published by the setup passes that re-run.
type Snapshot struct {
	typeTable      *typetable.TypeTable
	methodTable    *typetable.MethodTable
	instantiations *typetable.InstantiationTable
	constraints    *typetable.ConstraintTable
	errors         []TypeError
	// Statements is how many of the program's statements this snapshot covers. The caller
	// passes it back to CheckFrom, which starts there.
	Statements int
}

// Snapshot captures what has been checked so far. The receiver may go on being used: the
// capture is a copy.
func (tc *TypeChecker) Snapshot(statements int) *Snapshot {
	return &Snapshot{
		typeTable:      tc.typeTable.Clone(),
		methodTable:    tc.methodTable.Clone(),
		instantiations: tc.instantiations.Clone(),
		constraints:    tc.constraintChecks.Clone(),
		errors:         append([]TypeError(nil), tc.errors...),
		Statements:     statements,
	}
}

// NewFrom builds a checker primed with a snapshot's results, ready to check the statements
// the snapshot does not cover.
//
// The type table is *filled from* the snapshot rather than replaced, because the caller owns
// it and passes the same one to every pass downstream.
func NewFrom(symTable *symbols.SymbolTable, scopeTable *symbols.ScopeTable,
	typeTable *typetable.TypeTable, snap *Snapshot) *TypeChecker {
	tc := New(symTable, scopeTable, typeTable)
	if snap == nil {
		return tc
	}
	typeTable.Absorb(snap.typeTable)
	tc.methodTable = snap.methodTable.Clone()
	tc.instantiations = snap.instantiations.Clone()
	tc.constraintChecks = snap.constraints.Clone()
	tc.errors = append([]TypeError(nil), snap.errors...)
	return tc
}

// CheckFrom is Check, skipping the first `start` statements — the ones a snapshot already
// covers. CheckFrom(program, 0) is exactly Check.
//
// The setup passes above the loop still run over the **whole** program, which is both cheap
// and necessary: impl coherence and the trait-default candidate sets are whole-program
// questions, and a user's `impl` of a prelude trait has to reach them.
func (tc *TypeChecker) CheckFrom(program *ast.Program, start int) []TypeError {
	tc.prepare(program)
	tc.checkRange(program, start, len(program.Statements))
	return tc.errors
}

// CheckSplit checks [0, at) — the statements a snapshot would cover — hands the caller a
// snapshot of that, and then checks the rest. It is how a snapshot gets taken in the first
// place, on the run that has no cache to start from.
//
// **The split changes the order consts are checked in, and that is safe for one reason.**
// Check does every const in the program before any other statement, so a body referencing a
// const sees its resolved type. Splitting means the prefix's non-consts are checked before
// the *suffix's* consts — which would matter if a prelude function body could reference a
// user const, and it cannot: a module resolves through its own scope, its imports and the
// prelude, never downward into a module that imports it. Within each half the consts-first
// order is preserved, which is the ordering a program can actually observe.
func (tc *TypeChecker) CheckSplit(program *ast.Program, at int, onPrefix func(*Snapshot)) []TypeError {
	tc.prepare(program)
	tc.checkRange(program, 0, at)
	if onPrefix != nil {
		onPrefix(tc.Snapshot(at))
	}
	tc.checkRange(program, at, len(program.Statements))
	return tc.errors
}

// checkRange is the per-statement loop over [start, end), consts first.
func (tc *TypeChecker) checkRange(program *ast.Program, start, end int) {
	if start < 0 {
		start = 0
	}
	if end > len(program.Statements) {
		end = len(program.Statements)
	}
	for i := start; i < end; i++ {
		if isTopLevelConst(program.Statements[i]) {
			tc.checkInModule(program.Statements[i])
		}
	}
	for i := start; i < end; i++ {
		if !isTopLevelConst(program.Statements[i]) {
			tc.checkInModule(program.Statements[i])
		}
	}
}
