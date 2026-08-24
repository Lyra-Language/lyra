package modules

import (
	"bytes"
	"sync"

	sitter "github.com/tree-sitter/go-tree-sitter"
)

// ParseCache holds the syntax tree of each file whose bytes have not changed.
//
// It exists for a language server, which re-resolves a document's whole import graph on
// every keystroke and so re-parses every file in it — including a standard prelude that
// cannot have changed. See Options.ParseCache for the measurement.
//
// **A hit requires the bytes to match, not the path.** The file is read either way; only
// the parse is skipped. That makes a stale tree unreachable rather than unlikely: a file
// changed on disk behind the editor's back (a git checkout under a running server) simply
// misses, where an mtime or path key would serve the old tree at exactly the moment being
// wrong is most confusing.
//
// A tree outlives the cache entry it came from — nothing closes one, and the units handed
// to the driver keep referring to it — so eviction is safe at any time.
type ParseCache struct {
	mu      sync.Mutex
	entries map[string]parseEntry
}

type parseEntry struct {
	source []byte
	tree   *sitter.Tree
}

// NewParseCache returns an empty cache. Safe for concurrent use.
func NewParseCache() *ParseCache {
	return &ParseCache{entries: map[string]parseEntry{}}
}

func (c *ParseCache) get(file string, source []byte) *sitter.Tree {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[file]
	if !ok || !bytes.Equal(e.source, source) {
		return nil
	}
	return e.tree
}

func (c *ParseCache) put(file string, source []byte, tree *sitter.Tree) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	// The source is copied because the caller's slice is the buffer it read into, and an
	// overlay's bytes belong to the editor's document store — either may be reused or
	// mutated after this returns, which would silently turn a later comparison into a
	// false hit.
	c.entries[file] = parseEntry{source: bytes.Clone(source), tree: tree}
}
