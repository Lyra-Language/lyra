// Package modules resolves a program's import graph: it turns an entry file into
// the ordered list of source units a compile needs, following `import` statements
// transitively.
//
// The mapping is by directory convention — `std.io` is `std/io.lyra` beneath one of
// the search roots — so a module's name and its location agree by construction and
// there is no manifest to keep in sync. Roots are searched in order, which is what
// lets a program's own files shadow nothing and the standard library live outside the
// project tree.
//
// Deliberately out of scope, since none of it changes what a module's source looks
// like: package management, versioning, and separate or incremental compilation. A
// compile reads every unit it needs in one process and hands them to the collector as
// one program.
package modules

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"

	"github.com/Lyra-Language/lyra/pkg/ast"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/parser"
)

// Extension is the source-file suffix a module path maps onto.
const Extension = ".lyra"

// PreludeModule is the module implicitly available to every file. It is an ordinary
// module — `pub` exports, resolved through the same roots — that the compiler happens
// to import for you, rather than a set of names baked into the compiler. That is what
// lets it be read, tested and replaced like any other code.
const PreludeModule = "std.prelude"

// Unit is one resolved source file: its module path (empty for the entry file when it
// declares no module), where it came from, its bytes, and its parsed tree.
type Unit struct {
	Path   string // dotted module path, e.g. "std.prelude"
	File   string // filesystem path it was read from
	Source []byte
	Tree   *sitter.Tree
	Root   *sitter.Node
}

// Resolve reads the entry file and everything it imports, transitively, and returns
// the units in **dependency order** — every module before the ones importing it —
// with the entry file last.
//
// Order matters less than it appears: the collector's whole-program passes run after
// every unit is walked, precisely so a forward reference across modules resolves. It
// is dependency-ordered anyway because diagnostics read better when a module's own
// errors precede those of its dependents.
//
// An import that cannot be found, and an import cycle, are both reported as
// diagnostics rather than returned as errors: they are the user's mistakes, and
// reporting them alongside the rest means a compile shows all of them at once.
func Resolve(entryFile string, roots []string, opts Options) ([]Unit, []diag.Diagnostic) {
	r := &resolver{
		roots:   roots,
		overlay: cleanOverlay(opts.Overlay),
		byPath:  map[string]bool{},
		onStack: map[string]bool{},
	}
	entry, ok := r.load(entryFile, "", ast.Location{}, entryFile)
	if !ok {
		return nil, r.diags
	}
	r.includePrelude(opts, entry)
	r.visit(entry)
	return r.units, r.diags
}

// Options configures resolution. The zero value pulls in no prelude, which is what a
// caller analyzing a single snippet wants.
type Options struct {
	// Prelude is the module implicitly available to every file, e.g. "std.prelude".
	// Empty disables it.
	Prelude string

	// Overlay supplies in-memory source for files, keyed by filesystem path, and wins
	// over what is on disk. It exists for an editor: a language server analyzes the
	// buffer the user is typing into, which is by definition not what is saved — and
	// a file that has never been saved has nothing on disk at all. Resolution
	// therefore treats an overlaid path as existing, so an import of an unsaved file
	// resolves too.
	//
	// Keys are compared after filepath.Clean, so a caller need not pre-normalize
	// them; they do have to be absolute if the roots are (they are, since the entry
	// file's own directory is the first root).
	Overlay map[string][]byte
}

// cleanOverlay normalizes an overlay's keys once, at entry, so every later lookup is a
// plain map hit rather than a path comparison that each call site could get subtly
// different.
func cleanOverlay(in map[string][]byte) map[string][]byte {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string][]byte, len(in))
	for file, src := range in {
		out[filepath.Clean(file)] = src
	}
	return out
}

// includePrelude resolves the prelude ahead of the entry file's own imports, so it is
// collected first and its names are in place before anything can shadow them.
//
// A missing prelude is **not an error**. The standard library is found by searching the
// roots, and a program that has none — a single file compiled straight out of a
// directory, which is most of the test suite — must still build. Requiring it would
// turn "no std/ on this machine" into a compile failure for every program.
//
// The prelude does not import itself: when the entry file *is* the prelude, this is
// skipped, which is what lets the prelude be compiled and tested like any other module.
func (r *resolver) includePrelude(opts Options, entry Unit) {
	if opts.Prelude == "" || entry.Path == opts.Prelude {
		return
	}
	rel := filepath.Join(strings.Split(opts.Prelude, ".")...) + Extension
	for _, root := range r.roots {
		candidate := filepath.Join(root, rel)
		if !r.exists(candidate) {
			continue
		}
		if unit, ok := r.load(candidate, opts.Prelude, ast.Location{}, entry.File); ok {
			r.visit(unit)
		}
		return
	}
}

type resolver struct {
	roots   []string
	overlay map[string][]byte // filesystem path → in-memory source (see Options.Overlay)
	units   []Unit
	byPath  map[string]bool // module path → already emitted
	onStack map[string]bool // module path → currently being visited (cycle detection)
	diags   []diag.Diagnostic
}

// exists reports whether a candidate file is available to be loaded — on disk, or as an
// overlay. Both the prelude search and import resolution ask this before load, so an
// unsaved buffer has to answer yes here or it would be reported missing despite being
// readable.
func (r *resolver) exists(file string) bool {
	if _, ok := r.overlay[filepath.Clean(file)]; ok {
		return true
	}
	_, err := os.Stat(file)
	return err == nil
}

// read returns a file's source, preferring the overlay.
func (r *resolver) read(file string) ([]byte, error) {
	if src, ok := r.overlay[filepath.Clean(file)]; ok {
		return src, nil
	}
	return os.ReadFile(file)
}

// visit walks u's imports depth-first, emitting each dependency before u itself.
func (r *resolver) visit(u Unit) {
	key := u.Path
	if key == "" {
		key = u.File
	}
	if r.byPath[key] {
		return
	}
	// A cycle is rejected rather than broken arbitrarily: with no lazy or partial
	// initialization semantics defined, "which half of the cycle sees the other" has
	// no answer a user could predict.
	if r.onStack[key] {
		r.errorf(u.File, ast.Location{}, diag.CodeImportCycle,
			"import cycle: %s imports itself, directly or indirectly", key)
		return
	}
	r.onStack[key] = true
	for _, imp := range importsOf(u) {
		path := joinPath(imp.Path)
		dep, ok := r.resolveImport(path, u.File, imp.GetLocation())
		if !ok {
			continue
		}
		r.visit(dep)
	}
	delete(r.onStack, key)
	r.byPath[key] = true
	r.units = append(r.units, u)
}

// resolveImport finds the file a module path names, searching each root in order.
func (r *resolver) resolveImport(path, fromFile string, loc ast.Location) (Unit, bool) {
	rel := filepath.Join(strings.Split(path, ".")...) + Extension
	var tried []string
	for _, root := range r.roots {
		candidate := filepath.Join(root, rel)
		if r.exists(candidate) {
			return r.load(candidate, path, loc, fromFile)
		}
		tried = append(tried, candidate)
	}
	r.errorf(fromFile, loc, diag.CodeUnresolvedImport,
		"cannot find module %q — looked for %s", path, strings.Join(tried, ", "))
	return Unit{}, false
}

// load reads and parses one file.
func (r *resolver) load(file, path string, loc ast.Location, fromFile string) (Unit, bool) {
	source, err := r.read(file)
	if err != nil {
		r.errorf(fromFile, loc, diag.CodeUnresolvedImport, "cannot read %s: %v", file, err)
		return Unit{}, false
	}
	tree, err := parser.Parse(string(source))
	if err != nil || tree == nil {
		r.errorf(file, ast.Location{}, "", "parse error: %v", err)
		return Unit{}, false
	}
	u := Unit{Path: path, File: file, Source: source, Tree: tree, Root: tree.RootNode()}
	if u.Path == "" {
		u.Path = declaredModulePath(u)
	}
	return u, true
}

func (r *resolver) errorf(file string, loc ast.Location, code, format string, args ...any) {
	r.diags = append(r.diags, diag.Diagnostic{
		File:     file,
		Location: loc,
		Severity: diag.SeverityError,
		Code:     code,
		Message:  fmt.Sprintf(format, args...),
	})
}

func joinPath(parts []ast.ModuleName) string {
	names := make([]string, len(parts))
	for i, p := range parts {
		names[i] = p.Name
	}
	return strings.Join(names, ".")
}
