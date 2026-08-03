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
	// program, so they all have to be known up front (see driver.AnalyzeUnits). The
	// roots and the prelude setting are modules' to define, so the compiler and the
	// language server cannot disagree about where the standard library is.
	units, diags := modules.Resolve(path, modules.DefaultRoots(path), modules.DefaultOptions())
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
