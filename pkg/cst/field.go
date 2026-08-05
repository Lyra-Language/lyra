// Package cst holds the small accessors every CST walk needs, so the cost and the
// hazards of reaching into tree-sitter live in one place rather than at hundreds of
// call sites.
package cst

import (
	"sync"

	sitter "github.com/tree-sitter/go-tree-sitter"
)

// fieldIDs memoizes a grammar field's name → id. The ids are a property of the
// language, and this compiler parses exactly one, so a single map answers for the
// whole process; a second grammar would need this keyed by language.
//
// Read-mostly by construction: a field name is looked up once and then answered from
// the map for the rest of the run, so the read lock is uncontended in practice even
// with the parallel per-test analyses this package sees.
var (
	fieldIDMu sync.RWMutex
	fieldIDs  = map[string]uint16{}
)

// Field returns n's child under the named grammar field, or **nil when the field is
// absent** — a genuine nil `*sitter.Node`, exactly as ChildByFieldName returns.
//
// That nil is the reason to be careful rather than convenient: calling an accessor
// (`ChildCount`, `Child`, `Kind`) on it does not panic, it **hangs inside the CGO
// binding**, which once silently froze the whole collector. Check the result before
// touching it — see the collector's README and hazard 2 in lyra/CLAUDE.md.
//
// It resolves the field to an id once and then calls ts_node_child_by_field_id.
// ChildByFieldName, which this replaces, instead allocates a C string from the Go
// name, calls into C, and frees it — on *every* lookup, at nearly every node. That
// made it about a quarter of all samples in an analysis run and roughly half the
// front end's time; going through the cached id measured ~3.7x faster on the same
// walk (pkg/parser's field benchmark), because the malloc/copy/free disappears and
// only the CGO call remains.
func Field(n *sitter.Node, name string) *sitter.Node {
	return n.ChildByFieldId(fieldID(n, name))
}

// fieldID is Field's memo. An unknown name resolves to 0, the invalid field id,
// which ChildByFieldId answers with nil — the same answer ChildByFieldName gives for
// a name the grammar does not define, so a typo'd field stays as quiet as it was.
func fieldID(n *sitter.Node, name string) uint16 {
	fieldIDMu.RLock()
	id, ok := fieldIDs[name]
	fieldIDMu.RUnlock()
	if ok {
		return id
	}
	id = n.Language().FieldIdForName(name)
	fieldIDMu.Lock()
	fieldIDs[name] = id
	fieldIDMu.Unlock()
	return id
}
