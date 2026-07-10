// Command lyrac is the Lyra compiler CLI.
//
// Today it runs the full front-end (parse → collect → check → typecheck) via
// pkg/driver and reports diagnostics. Code generation is not implemented yet:
// `lyrac build` stops after a clean analysis at the seam where a backend will
// be wired in (see lowerAndEmit).
package main

import (
	"fmt"
	"os"

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

// lowerAndEmit is the backend seam: given a fully-typed, error-free program and
// its resolved entry point, lower it to a target and emit an artifact. Not
// implemented yet.
func lowerAndEmit(path string, res *driver.Result, entry *driver.EntryPoint) int {
	fmt.Printf("%s: analysis passed (%d statement(s)); entry point %q returns %s; code generation is not implemented yet\n",
		path, len(res.Program.Statements), entry.Name, entry.Returns)
	return 0
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
