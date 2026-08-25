package main

import (
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/owenrumney/go-lsp/lsp"

	"github.com/Lyra-Language/lyra/pkg/driver"
	"github.com/Lyra-Language/lyra/pkg/modules"
)

// Finding a module's **importers**, which is the one direction resolution does not go.
//
// `modules.Resolve` follows a file's imports downward: what it needs, and its own module's
// sibling files. Nothing follows them upward, so the modules that depend on the open
// document are never loaded — and "find every use of this exported type" and "rename it
// everywhere" are both upward questions. Answering them by resolving harder is impossible;
// the graph simply does not run that way. It has to be answered by *searching*.
//
// **A walk, not an index, and deliberately so for now.** The obvious optimisation is a
// persistent map rebuilt on change, and the obvious hazard is that its invalidation is
// wrong in a way nobody notices until a rename misses a file. This runs on an explicit user
// action — a rename, or references on an exported name — never per keystroke, and it reads
// each file once through the shared parse cache. Measure before making it clever.

// workspaceRoot is where that search starts. The client's root is preferred, since it is
// what the user opened; a document outside it (or a client that sent none) falls back to
// the document's own directory, which is also what the module resolver treats as a root.
func (h *Handler) workspaceRoot(docPath string) string {
	h.mu.Lock()
	root := h.rootPath
	h.mu.Unlock()
	if root != "" && docPath != "" {
		if rel, err := filepath.Rel(root, docPath); err == nil && !strings.HasPrefix(rel, "..") {
			return root
		}
	}
	if root != "" && docPath == "" {
		return root
	}
	return filepath.Dir(docPath)
}

// importerFiles returns every file under the workspace root whose `import` statements name
// modulePath, excluding files of that module itself — a sibling is already in the unit set
// and is not an importer.
//
// The standard library is skipped: it is on the search path of every program and cannot
// import a user's module, so scanning it is pure cost. A directory that cannot be read is
// skipped rather than failing the walk, since a workspace routinely contains one.
func (h *Handler) importerFiles(root, modulePath string) []string {
	if root == "" || modulePath == "" {
		return nil
	}
	var out []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil //nolint:nilerr // an unreadable directory is skipped, not fatal
		}
		if d.IsDir() {
			if skipDir(path, d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".lyra" {
			return nil
		}
		source, ok := h.sourceFor(path)
		if !ok {
			return nil
		}
		header, ok := modules.ScanFile(path, source, h.parseCache)
		if !ok || header.Module == modulePath {
			return nil
		}
		for _, imported := range header.Imports {
			if imported == modulePath {
				out = append(out, path)
				return nil
			}
		}
		return nil
	})
	if err != nil {
		log.Printf("importers: walk of %s failed: %v", root, err)
	}
	return out
}

// sourceFor is the file's text as the *editor* has it — an open buffer's unsaved content
// when there is one, and the file on disk otherwise. A rename that consulted only disk
// would miss an import the user added a moment ago and has not saved.
func (h *Handler) sourceFor(path string) ([]byte, bool) {
	h.mu.Lock()
	source, open := h.docStore[pathToURI(path)]
	h.mu.Unlock()
	if open {
		return []byte(source), true
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	return data, true
}

// skipDir keeps the walk out of places a Lyra source file cannot usefully be: version
// control, dependency and build directories, and anything hidden.
//
// **The standard library is not skipped**, though an earlier version did skip it and found
// nothing at all as a result. `StdRoot` is the directory *containing* `std/` — that is what
// makes `std.prelude` resolve to `<root>/std/prelude` — so in a checkout where the library
// lives inside the project, the std root *is* the project root and skipping it skips the
// whole walk. Scanning it is also right on its own terms: `std.tui` imports `std.prelude`,
// so a prelude type has importers inside the library.
func skipDir(_, name string) bool {
	switch name {
	case ".git", "node_modules", "target", "vendor":
		return true
	}
	return strings.HasPrefix(name, ".") && name != "."
}

// importerAnalysis analyses the open document together with every module that imports it,
// so the result's type index covers the uses resolution alone cannot reach.
//
// Kept apart from analyzeDocument, whose result is *published*: this program is larger than
// the one the user is editing, so its diagnostics belong to files they did not ask about
// and two modules that each export a name would be reported as colliding. Only the index is
// taken from it.
// exported says whether the name being asked about can be seen outside its module. A
// private one cannot, so it has no importers and the walk is pure cost — which matters,
// since the walk is tens of milliseconds against the microseconds the rest of a lookup
// takes.
func (h *Handler) importerAnalysis(analysis *docAnalysis, source string, exported bool) *docAnalysis {
	if !exported || analysis.file == "" || analysis.symTable == nil {
		return analysis
	}
	module := analysis.symTable.ModuleOfFile[analysis.file]
	if module == "" {
		return analysis
	}
	root := h.workspaceRoot(analysis.file)
	importers := h.importerFiles(root, module)
	if len(importers) == 0 {
		return analysis
	}

	opts := modules.DefaultOptions()
	opts.Overlay = h.overlay(lsp.DocumentURI(pathToURI(analysis.file)), source)
	opts.ParseCache = h.parseCache

	seen := map[string]bool{}
	var units []modules.Unit
	for _, entry := range append([]string{analysis.file}, importers...) {
		found, _ := modules.Resolve(entry, modules.DefaultRoots(entry), opts)
		for _, u := range found {
			if seen[u.File] {
				continue
			}
			seen[u.File] = true
			units = append(units, u)
		}
	}
	if len(units) == 0 {
		return analysis
	}
	res := driver.AnalyzeUnitsCached(units, h.collectCache)
	if res == nil || res.SymbolTable == nil {
		return analysis
	}
	log.Printf("importers: %s is imported by %d file(s); index spans %d unit(s)", module, len(importers), len(units))
	widened := *analysis
	widened.symTable = res.SymbolTable
	return &widened
}
