package collector

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/ast/symbols"
)

// Snapshot is the collector's state after some prefix of a program's files, held so the
// rest can be collected onto a copy of it rather than onto a re-collection of everything.
//
// **The prefix is what does not change between keystrokes.** A language server re-analyzes
// a document's whole import graph on every edit; for a small file with the standard prelude
// that is 12 units of which 11 cannot have changed, and collection is 75% of the analysis.
//
// A snapshot is **immutable once taken**: Restore hands out a deep copy and the original is
// never written to again. That is the difference between this and undoing the edited file's
// contributions — a copy has to be right once, an undo has to be right every time, and the
// failure mode of getting an undo slightly wrong is analysis that drifts as a session runs.
type Snapshot struct {
	table      *symbols.SymbolTable
	scopeTable *symbols.ScopeTable
	statements []ast.AstNode
	errors     []error
}

// Snapshot captures the collector's current state. The receiver may go on being used —
// the capture is a copy, so what happens to it afterwards does not reach the snapshot.
func (c *Collector) Snapshot() *Snapshot {
	table, seen := c.table.Clone()
	return &Snapshot{
		table:      table,
		scopeTable: c.scopeTable.CloneWith(seen),
		statements: append([]ast.AstNode(nil), c.ast.Statements...),
		errors:     append([]error(nil), c.errors...),
	}
}

// Restore builds a collector holding a copy of the snapshot's state, ready for the
// remaining files to be added to it.
//
// The AST statements are shared rather than copied, which is the point: every side table
// downstream — ScopeTable, TypeTable, MethodTable — is keyed by AST *pointer*, so copying
// the nodes would invalidate all of them. What makes that safe is that re-running the
// analysis passes over one collected AST is idempotent, which pkg/driver's re-analysis test
// checks directly over the real prelude rather than assuming.
func (s *Snapshot) Restore(source []byte) *Collector {
	table, seen := s.table.Clone()
	c := &Collector{
		source:     source,
		table:      table,
		scopeTable: s.scopeTable.CloneWith(seen),
		ast:        &ast.Program{Statements: append([]ast.AstNode(nil), s.statements...)},
		errors:     append([]error(nil), s.errors...),
	}
	c.ctx = collector_ctx.NewCtx(source, c, &c.errors)
	c.ctx.ScopeTable = c.scopeTable
	// CurrentScope is where the *next* AddFile starts from, and AddFile re-parents itself
	// onto the file's own module scope anyway — but a restored collector whose current
	// scope pointed into the master's graph would write the edited file's bindings into a
	// scope nothing else in this copy can see.
	c.table.CurrentScope = c.table.GlobalScope
	return c
}

// StatementCount is how many top-level statements the snapshot covers — the boundary the
// typechecker resumes from. Read from the snapshot rather than recomputed from the units,
// because Finish may append a statement (a synthesized derive) and the count at capture time
// is the only thing that says where the prefix actually ends.
func (s *Snapshot) StatementCount() int {
	if s == nil {
		return 0
	}
	return len(s.statements)
}
