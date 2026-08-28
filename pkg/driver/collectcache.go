package driver

import (
	"crypto/sha256"
	"encoding/binary"
	"sync"

	"github.com/Lyra-Language/lyra/pkg/analyzer/collector"
	"github.com/Lyra-Language/lyra/pkg/analyzer/typechecker"
	"github.com/Lyra-Language/lyra/pkg/modules"
)

// CollectCache reuses the collection of a program's unchanged prefix.
//
// **Collection is ~75% of analysis, and for an editor almost all of it is wasted.** A
// language server re-analyzes the document's whole import graph on every keystroke; for a
// small file with the standard prelude that is 12 units, of which only the one being typed
// in can have changed. Measured: 0.09 ms to analyze the user's file alone, 11.7 ms with the
// prelude.
//
// The cache holds one snapshot, taken after every unit **except the last** — units arrive in
// dependency order with the entry file last, so that boundary is exactly "everything the
// user is not editing". Editing a file that is not the entry invalidates the snapshot and
// costs a full collection, which is correct and rare.
//
// Nil is the default and means no reuse, which is what a one-shot `lyrac` wants.
type CollectCache struct {
	mu   sync.Mutex
	key  string
	snap *collector.Snapshot
	// types is the typechecking of that same prefix. It rides here rather than in a cache
	// of its own because it is valid under exactly the same condition — the prefix's bytes,
	// the prelude path and the import graph — and two caches keyed identically are two
	// chances to invalidate one and not the other.
	types *typechecker.Snapshot
}

// NewCollectCache returns an empty cache. Safe for concurrent use.
func NewCollectCache() *CollectCache { return &CollectCache{} }

// prefixKey identifies the state a snapshot is valid for.
//
// It covers more than the prefix's own bytes, because collection is configured from the
// *whole* unit set before the first file is walked: `SetPreludeModule` and `SetImports` both
// affect how a name resolves (`symbols.declKeyIn`) and which declarations warn as shadows,
// so a prelude module or import graph that differs makes a snapshot of the same files mean
// something different. Editing an
// `import` line therefore invalidates, which is the intent — it is a change to how every
// name in the program resolves, not just to one file.
func prefixKey(units []modules.Unit, n int, prelude string, graph map[string][]string) string {
	h := sha256.New()
	h.Write([]byte(prelude))
	h.Write([]byte{0})
	var scratch [8]byte
	for i := 0; i < n; i++ {
		h.Write([]byte(units[i].Path))
		h.Write([]byte{0})
		h.Write([]byte(units[i].File))
		h.Write([]byte{0})
		binary.LittleEndian.PutUint64(scratch[:], uint64(len(units[i].Source)))
		h.Write(scratch[:])
		h.Write(units[i].Source)
	}
	// The import graph is a map, so it is folded in through the units' own order rather
	// than by ranging over it — map iteration order is randomized, and a key that varied
	// run to run would never hit.
	for i := range units {
		h.Write([]byte(units[i].Path))
		for _, dep := range graph[units[i].Path] {
			h.Write([]byte{1})
			h.Write([]byte(dep))
		}
	}
	return string(h.Sum(nil))
}

func (c *CollectCache) get(key string) *collector.Snapshot {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.key != key {
		return nil
	}
	return c.snap
}

func (c *CollectCache) put(key string, snap *collector.Snapshot) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	// A new prefix invalidates the typechecking of the old one.
	c.key, c.snap, c.types = key, snap, nil
}

func (c *CollectCache) getTypes(key string) *typechecker.Snapshot {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.key != key {
		return nil
	}
	return c.types
}

func (c *CollectCache) putTypes(key string, snap *typechecker.Snapshot) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.key != key {
		return
	}
	c.types = snap
}
