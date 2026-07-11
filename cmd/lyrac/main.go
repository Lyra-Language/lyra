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
	source, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "lyrac: %v\n", err)
		return nil, false
	}
	return driver.Analyze(source), true
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
		where := path
		if loc.StartLine > 0 {
			where = fmt.Sprintf("%s:%d:%d", path, loc.StartLine, loc.StartCol)
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
