// Command lyrac is the Lyra compiler CLI.
//
// It runs the full front-end (parse → collect → check → typecheck) via
// pkg/driver and reports diagnostics; `lyrac build` then hands the typed
// program to the pkg/backend/llvm backend (see lowerAndEmit). Codegen is
// early — literals, arithmetic, and int-width conversions lower to real IR
// (see that package's doc comment for exactly what's covered) — so a
// non-trivial `main` may still hit an unimplemented form.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Lyra-Language/lyra/pkg/backend/llvm"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/driver"
	"github.com/Lyra-Language/lyra/pkg/modules"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) < 2 {
		usage()
		return 2
	}
	cmd, path := args[0], args[1]
	switch cmd {
	case "check":
		return check(path)
	case "build":
		return build(path)
	default:
		fmt.Fprintf(os.Stderr, "lyrac: unknown command %q\n", cmd)
		usage()
		return 2
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `usage: lyrac <command> <file.lyra>

commands:
  check   parse and type-check a source file, reporting diagnostics
  build   check, then (once implemented) generate code
`)
}

// check analyzes path and reports diagnostics. Exit status: 0 clean, 1 on any
// error-severity diagnostic, 2 on a usage/IO failure.
func check(path string) int {
	res, ok := analyze(path)
	if !ok {
		return 2
	}
	printDiagnostics(path, res.Diagnostics)
	if res.HasErrors() {
		return 1
	}
	return 0
}

// build runs the same analysis, resolves the program's entry point, and then
// hands the typed program to the backend. The backend does not exist yet, so a
// clean build stops at lowerAndEmit with a clear notice rather than silently
// producing nothing.
func build(path string) int {
	res, ok := analyze(path)
	if !ok {
		return 2
	}
	printDiagnostics(path, res.Diagnostics)
	if res.HasErrors() {
		return 1
	}
	entry, entryDiags := driver.ResolveEntryPoint(res)
	printDiagnostics(path, entryDiags)
	if entry == nil {
		return 1
	}
	return lowerAndEmit(path, res, entry)
}

// analyze reads path and runs the front-end pipeline. Returns ok=false (after
// printing to stderr) when the file cannot be read.
func analyze(path string) (*driver.Result, bool) {
	// Resolve the import graph before analyzing: every unit is collected into one
	// program, so they all have to be known up front (see driver.AnalyzeUnits).
	// Roots are the entry file's own directory first — a program's modules sit
	// alongside it — then the standard library.
	roots := []string{filepath.Dir(path)}
	if std := stdRoot(); std != "" {
		roots = append(roots, std)
	}
	// LYRA_NO_PRELUDE exists for bootstrapping and for tests: the prelude is ordinary
	// Lyra, so it has to be compilable by a compiler that is not yet handing it to
	// everything else.
	opts := modules.Options{Prelude: modules.PreludeModule}
	if os.Getenv("LYRA_NO_PRELUDE") != "" {
		opts.Prelude = ""
	}
	units, diags := modules.Resolve(path, roots, opts)
	if len(units) == 0 {
		for _, d := range diags {
			fmt.Fprintf(os.Stderr, "lyrac: %s\n", d.Message)
		}
		return nil, false
	}
	res := driver.AnalyzeUnits(units)
	// Resolver diagnostics come first: an unreadable import explains the errors that
	// follow from the names it failed to provide.
	res.Diagnostics = append(diags, res.Diagnostics...)
	return res, true
}

// stdRoot locates the standard library: the directory that *contains* `std/`, since a
// module path resolves beneath a root (`std.prelude` → `<root>/std/prelude.lyra`).
//
// LYRA_STD wins so a build can point at a working copy; otherwise the compiler's own
// directory is used, which is where ./build.sh puts `std` beside the binary — the same
// beside-the-executable convention Rust, Zig and Go use for a sysroot. An absent
// standard library is not an error: a program that uses nothing from it still builds.
func stdRoot() string {
	if root := os.Getenv("LYRA_STD"); root != "" {
		return root
	}
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	// Resolve symlinks before taking the directory. os.Executable does not do it
	// consistently — on Linux it reads /proc/self/exe, which is already resolved, but on
	// macOS it can hand back the symlink's own path. So a compiler symlinked onto PATH
	// (`ln -s .../build/lyrac /usr/local/bin/lyrac`) would look for the standard library
	// beside the *link* rather than beside the real binary, and find nothing — a
	// platform split that shows up as "the prelude works on my machine".
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	// The *root* is the directory holding `std/`, not `std/` itself: a module path is
	// resolved beneath a root, so `std.prelude` becomes `<root>/std/prelude.lyra`.
	// Returning the `std` directory here instead looked for `std/std/prelude.lyra` and
	// silently found no prelude.
	root := filepath.Dir(exe)
	if info, err := os.Stat(filepath.Join(root, "std")); err == nil && info.IsDir() {
		return root
	}
	return ""
}

// lowerAndEmit runs the backend over a fully-typed, error-free program and
// writes the emitted artifact. The LLVM backend is early (see pkg/backend/llvm's
// doc comment for what's covered); a form it doesn't lower yet returns an error
// here rather than silently emitting wrong code.
func lowerAndEmit(path string, res *driver.Result, entry *driver.EntryPoint) int {
	be := llvm.New()
	ir, err := be.Emit(res, entry)
	if err != nil {
		fmt.Fprintf(os.Stderr, "lyrac: %s backend: %v\n", be.Name(), err)
		return 1
	}
	out := replaceExt(path, ".ll")
	if err := os.WriteFile(out, ir, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "lyrac: %v\n", err)
		return 1
	}
	fmt.Printf("%s: wrote %s (%s backend)\n", path, out, be.Name())
	fmt.Printf("  compile with: clang %s -o %s\n", out, replaceExt(path, ""))
	return 0
}

// replaceExt returns path with a trailing ".lyra" replaced by ext (ext may be
// empty to drop the extension).
func replaceExt(path, ext string) string {
	return strings.TrimSuffix(path, ".lyra") + ext
}

// printDiagnostics writes each diagnostic as `path:line:col: severity[code]: message`,
// omitting the line:col when the diagnostic has no source location (a
// program-level error such as a missing entry point).
func printDiagnostics(path string, diags []diag.Diagnostic) {
	for _, d := range diags {
		loc := d.Location
		code := ""
		if d.Code != "" {
			code = fmt.Sprintf(" [%s]", d.Code)
		}
		// A diagnostic from an imported module names its own file; one with no File
		// set came from the entry unit (or is program-level) and uses the path given.
		file := path
		if d.Location.File != "" {
			file = d.Location.File // set on every node, so it covers all analysis passes
		}
		if d.File != "" {
			file = d.File // a resolver diagnostic names its own file explicitly
		}
		where := file
		if loc.StartLine > 0 {
			where = fmt.Sprintf("%s:%d:%d", file, loc.StartLine, loc.StartCol)
		}
		fmt.Fprintf(os.Stderr, "%s: %s%s: %s\n",
			where, severityLabel(d.Severity), code, d.Message)
	}
}

func severityLabel(s diag.Severity) string {
	switch s {
	case diag.SeverityWarning:
		return "warning"
	case diag.SeverityInfo:
		return "info"
	default:
		return "error"
	}
}
