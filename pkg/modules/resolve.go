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
// A module may also be a **directory** of files: `std.prelude` is `std/prelude.lyra`
// *or* every `*.lyra` directly inside `std/prelude/`. The two forms are the same module
// — one namespace, one set of keys, one scope — so a module that outgrows a file is
// split without any of its declarations changing meaning. That is not a convenience:
// receiver-keyed overloading and prelude shadowing are both per-module, so splitting a
// grown module into *separate* modules instead would silently change what its names
// mean. See README.md.
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
	"sort"
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
	group := r.entryGroup(entry)
	r.includePrelude(opts, entry)
	r.visit(group)
	return r.units, r.diags
}

// entryGroup returns every unit of the entry file's own module — the entry file alone
// unless it is one file of a multi-file module, in which case its siblings come with it.
//
// Without this, compiling `std/prelude/strings.lyra` directly would analyze a *fragment*
// of the prelude: `Maybe`, `slice` and everything else declared in a sibling file would
// be undefined, and the "the prelude compiles standalone" property — the whole reason it
// is an ordinary module — would hold only while it fitted in one file.
//
// The test is that the file sits in a directory *named by its own module path*. A file
// declaring `module app.util` in a directory called `src` is a single-file module that
// happens to have neighbours, and its neighbours are not its business.
func (r *resolver) entryGroup(entry Unit) []Unit {
	dir := filepath.Dir(entry.File)
	if entry.Path == "" || filepath.Base(dir) != lastSegment(entry.Path) {
		return []Unit{entry}
	}
	units, ok := r.loadDir(dir, entry.Path, ast.Location{}, entry.File)
	if !ok {
		return []Unit{entry}
	}
	// The entry file is re-read by loadDir; keep that copy rather than both, so the
	// unit set holds one of each file.
	return units
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
// The prelude does not import itself: when the entry file *is* the prelude — or, once
// the prelude is a directory, is *one file of* it — this is skipped, which is what lets
// the prelude be compiled and tested like any other module. Comparing module paths
// rather than file paths is what makes the multi-file case fall out for free.
func (r *resolver) includePrelude(opts Options, entry Unit) {
	if opts.Prelude == "" || entry.Path == opts.Prelude {
		return
	}
	if units, _, ok := r.findModule(opts.Prelude, entry.File, ast.Location{}); ok {
		r.visit(units)
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

// visit walks a module's imports depth-first, emitting each dependency before the
// module itself.
//
// It takes the module's *whole* unit set rather than one file, because dedup, cycle
// detection and emission are all per-module: a module's files share a namespace, so
// admitting one of them and not the others would collect a fragment of a module and
// report its own declarations undefined. The imports of every file are followed — an
// import binds for the module (`SymbolTable.Imports` is keyed by module path), so which
// file of a module wrote it does not change what it reaches.
func (r *resolver) visit(units []Unit) {
	if len(units) == 0 {
		return
	}
	key := units[0].Path
	if key == "" {
		key = units[0].File
	}
	if r.byPath[key] {
		return
	}
	// A cycle is rejected rather than broken arbitrarily: with no lazy or partial
	// initialization semantics defined, "which half of the cycle sees the other" has
	// no answer a user could predict.
	if r.onStack[key] {
		r.errorf(units[0].File, ast.Location{}, diag.CodeImportCycle,
			"import cycle: %s imports itself, directly or indirectly", key)
		return
	}
	r.onStack[key] = true
	for _, u := range units {
		for _, imp := range importsOf(u) {
			path := joinPath(imp.Path)
			dep, ok := r.resolveImport(path, u.File, imp.GetLocation())
			if !ok {
				continue
			}
			r.visit(dep)
		}
	}
	delete(r.onStack, key)
	r.byPath[key] = true
	r.units = append(r.units, units...)
}

// resolveImport finds the units a module path names, reporting an unresolvable import.
func (r *resolver) resolveImport(path, fromFile string, loc ast.Location) ([]Unit, bool) {
	units, tried, ok := r.findModule(path, fromFile, loc)
	if ok {
		return units, true
	}
	r.errorf(fromFile, loc, diag.CodeUnresolvedImport,
		"cannot find module %q — looked for %s", path, strings.Join(tried, ", "))
	return nil, false
}

// findModule searches the roots in order for a module, in either of its two forms: the
// file `<root>/std/prelude.lyra`, or the directory `<root>/std/prelude/`. It reports
// nothing when the module is simply absent — the prelude search depends on that, since a
// missing standard library must not be an error — and returns the candidates it tried so
// its caller can say where it looked.
//
// **A root offering both forms is an error rather than a silent preference.** Which one
// wins would decide what half the program's names mean, and a reader looking at
// `std/prelude/strings.lyra` has no way to tell that `std/prelude.lyra` beside it is
// quietly the real module. Across *different* roots there is no ambiguity: the earlier
// root wins, which is the ordinary shadowing every other lookup here does.
func (r *resolver) findModule(path, fromFile string, loc ast.Location) (units []Unit, tried []string, ok bool) {
	rel := filepath.Join(strings.Split(path, ".")...)
	for _, root := range r.roots {
		file, dir := filepath.Join(root, rel+Extension), filepath.Join(root, rel)
		hasFile, hasDir := r.exists(file), r.isModuleDir(dir)
		switch {
		case hasFile && hasDir:
			r.errorf(fromFile, loc, diag.CodeUnresolvedImport,
				"module %q is both %s and %s — a module is one or the other, so delete "+
					"whichever is not the module (move its declarations into the directory "+
					"to keep the split)", path, file, dir)
			return nil, nil, false
		case hasFile:
			u, loaded := r.load(file, path, loc, fromFile)
			// No directory: in the single-file form the header is optional, since the
			// file's location already says which module it is.
			if loaded {
				r.checkHeader(u, path, "")
			}
			return []Unit{u}, nil, loaded
		case hasDir:
			units, loaded := r.loadDir(dir, path, loc, fromFile)
			return units, nil, loaded
		}
		tried = append(tried, file, dir+string(filepath.Separator))
	}
	return nil, tried, false
}

// loadDir loads every `*.lyra` directly inside a module's directory, in name order.
//
// Not recursive, and deliberately: a subdirectory is the *next* module path down
// (`std/prelude/text/` is `std.prelude.text`), so recursing would swallow a module into
// its parent and make the two spellings of a name mean the same thing.
//
// Every file must declare the module it is in. Membership by location alone would be
// less to type, but it would also mean a file's own text no longer says what namespace
// its declarations land in — and this is a namespace where a name may be a receiver
// overload of one three files away. The header is the thing a reader has, so it is
// required and checked rather than inferred.
func (r *resolver) loadDir(dir, path string, loc ast.Location, fromFile string) ([]Unit, bool) {
	files, err := r.moduleFiles(dir)
	if err != nil {
		r.errorf(fromFile, loc, diag.CodeUnresolvedImport, "cannot read %s: %v", dir, err)
		return nil, false
	}
	if len(files) == 0 {
		r.errorf(fromFile, loc, diag.CodeUnresolvedImport,
			"module %q is the directory %s, which holds no %s files", path, dir, Extension)
		return nil, false
	}
	var units []Unit
	ok := true
	for _, file := range files {
		u, loaded := r.load(file, path, loc, fromFile)
		if !loaded {
			ok = false
			continue
		}
		if !r.checkHeader(u, path, dir) {
			ok = false
			continue
		}
		units = append(units, u)
	}
	return units, ok
}

// checkHeader reports a file whose `module` declaration disagrees with where it is.
//
// The two forms differ in one way, and only one: inside a module *directory* the header
// is required, because nothing else in the file says which namespace its declarations
// join — a directory is a set, and silence about membership in a set is ambiguous. A
// module that is a single file (dir == "") needs no header, since its path is its own
// location; a header that contradicts that location is still an error, because one of
// the two is wrong and guessing which is not the compiler's to do.
func (r *resolver) checkHeader(u Unit, path, dir string) bool {
	declared := declaredModulePath(u)
	if declared == path || (dir == "" && declared == "") {
		return true
	}
	// Reported against the offending file, at its top, rather than against the import
	// that pulled it in: the mistake is in this file, and the importer may be several
	// modules away with nothing to fix.
	top := ast.Location{StartLine: 1, StartCol: 1, EndLine: 1, EndCol: 1}
	if dir == "" {
		r.errorf(u.File, top, diag.CodeUnresolvedImport,
			"%s is module %q by its location, but declares module %q",
			filepath.Base(u.File), path, declared)
		return false
	}
	r.errorf(u.File, top, diag.CodeUnresolvedImport,
		"%s is part of module %q (it is in %s), so it must begin with `module %s`%s",
		filepath.Base(u.File), path, dir, path, declaredSuffix(declared))
	return false
}

// isModuleDir reports whether a path is a module's directory. An overlaid file counts
// even when nothing is on disk: an editor may hold a whole module that has never been
// saved, and reporting it missing is exactly what the overlay exists to prevent.
func (r *resolver) isModuleDir(dir string) bool {
	if info, err := os.Stat(dir); err == nil && info.IsDir() {
		return true
	}
	clean := filepath.Clean(dir)
	for file := range r.overlay {
		if filepath.Dir(file) == clean && strings.HasSuffix(file, Extension) {
			return true
		}
	}
	return false
}

// moduleFiles lists a module directory's own source files, in name order so a compile is
// reproducible — the emitted unit order feeds diagnostic order, and a directory listing
// is not ordered on every filesystem. The overlay is merged in, so an editor's unsaved
// (or never-saved) file counts as a member of the module it declares.
func (r *resolver) moduleFiles(dir string) ([]string, error) {
	seen := map[string]bool{}
	entries, err := os.ReadDir(dir)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), Extension) {
			seen[filepath.Join(dir, e.Name())] = true
		}
	}
	for file := range r.overlay {
		if filepath.Dir(file) == filepath.Clean(dir) && strings.HasSuffix(file, Extension) {
			seen[file] = true
		}
	}
	files := make([]string, 0, len(seen))
	for file := range seen {
		files = append(files, file)
	}
	sort.Strings(files)
	return files, nil
}

// declaredSuffix names what a misplaced file says instead, when it says anything. A file
// with no header at all is the commoner mistake and reads better without the clause.
func declaredSuffix(declared string) string {
	if declared == "" {
		return ""
	}
	return fmt.Sprintf(" (it declares module %q)", declared)
}

// lastSegment is the final component of a dotted module path — the directory name a
// multi-file module lives in.
func lastSegment(path string) string {
	if i := strings.LastIndex(path, "."); i >= 0 {
		return path[i+1:]
	}
	return path
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
