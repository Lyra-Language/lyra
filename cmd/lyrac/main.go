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
	"os/exec"
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
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "check":
		if len(rest) != 1 {
			usage()
			return 2
		}
		return check(rest[0])
	case "build":
		opts, ok := parseBuildArgs(rest)
		if !ok {
			usage()
			return 2
		}
		return build(opts)
	default:
		fmt.Fprintf(os.Stderr, "lyrac: unknown command %q\n", cmd)
		usage()
		return 2
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `usage: lyrac <command> [flags] <file.lyra>

commands:
  check   parse and type-check a source file, reporting diagnostics
  build   check, then compile to a native executable

build flags:
  -o <path>     write the executable here (default: the source path without .lyra)
  --emit-llvm   stop after emitting <name>.ll; do not link an executable
  --keep-ll     keep the emitted <name>.ll beside the executable
  --cc <path>   C compiler used to assemble and link (default: $LYRA_CC, else clang)
`)
}

// buildOptions is what `lyrac build` was asked for. The zero value (plus a path)
// is the default build: link an executable next to the source and keep no IR.
type buildOptions struct {
	path     string // the .lyra source
	out      string // executable path; empty means derive it from path
	emitOnly bool   // --emit-llvm: write the .ll and stop
	keepLL   bool   // --keep-ll: keep the .ll as well as the executable
	cc       string // C compiler override
}

// parseBuildArgs accepts flags before or after the source path, since a build
// command is as often edited by appending a flag as by inserting one.
func parseBuildArgs(args []string) (buildOptions, bool) {
	var o buildOptions
	for i := 0; i < len(args); i++ {
		arg := args[i]
		// A flag taking a value consumes the next argument; running off the end
		// is a usage error rather than an empty value silently taking effect.
		takesValue := arg == "-o" || arg == "--cc"
		if takesValue {
			if i+1 >= len(args) {
				fmt.Fprintf(os.Stderr, "lyrac: %s needs a value\n", arg)
				return o, false
			}
			i++
		}
		switch {
		case arg == "-o":
			o.out = args[i]
		case arg == "--cc":
			o.cc = args[i]
		case arg == "--emit-llvm":
			o.emitOnly = true
		case arg == "--keep-ll":
			o.keepLL = true
		case strings.HasPrefix(arg, "-"):
			fmt.Fprintf(os.Stderr, "lyrac: unknown flag %q\n", arg)
			return o, false
		case o.path != "":
			fmt.Fprintf(os.Stderr, "lyrac: build takes one source file (got %q and %q)\n", o.path, arg)
			return o, false
		default:
			o.path = arg
		}
	}
	if o.path == "" {
		return o, false
	}
	return o, true
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

// build runs the same analysis, resolves the program's entry point, hands the
// typed program to the backend, and links the emitted IR into an executable.
func build(o buildOptions) int {
	res, ok := analyze(o.path)
	if !ok {
		return 2
	}
	printDiagnostics(o.path, res.Diagnostics)
	if res.HasErrors() {
		return 1
	}
	entry, entryDiags := driver.ResolveEntryPoint(res)
	printDiagnostics(o.path, entryDiags)
	if entry == nil {
		return 1
	}
	return lowerAndEmit(o, res, entry)
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

// lowerAndEmit runs the backend over a fully-typed, error-free program, then
// links the emitted IR into an executable (unless --emit-llvm asked to stop at
// the IR). The LLVM backend is early (see pkg/backend/llvm's doc comment for
// what's covered); a form it doesn't lower yet returns an error here rather
// than silently emitting wrong code.
func lowerAndEmit(o buildOptions, res *driver.Result, entry *driver.EntryPoint) int {
	be := llvm.New()
	ir, err := be.Emit(res, entry)
	if err != nil {
		fmt.Fprintf(os.Stderr, "lyrac: %s backend: %v\n", be.Name(), err)
		return 1
	}

	llPath := replaceExt(o.path, ".ll")
	keepLL := o.emitOnly || o.keepLL
	if !keepLL {
		// A throwaway .ll: the executable is the artifact, so the IR does not
		// belong in the user's source tree. It still has to reach the compiler
		// as a file, since clang reads IR from a path, not from stdin.
		dir, err := os.MkdirTemp("", "lyrac-")
		if err != nil {
			fmt.Fprintf(os.Stderr, "lyrac: %v\n", err)
			return 1
		}
		defer os.RemoveAll(dir)
		llPath = filepath.Join(dir, filepath.Base(replaceExt(o.path, ".ll")))
	}
	if err := os.WriteFile(llPath, ir, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "lyrac: %v\n", err)
		return 1
	}

	if o.emitOnly {
		fmt.Printf("%s: wrote %s (%s backend)\n", o.path, llPath, be.Name())
		fmt.Printf("  compile with: clang %s -lm -o %s\n", llPath, exePath(o))
		return 0
	}

	cc, err := findCC(o.cc)
	if err != nil {
		// Keep the IR on this path whatever the flags said: without it the user
		// has nothing to hand to a compiler they install later.
		fallback := replaceExt(o.path, ".ll")
		if fallback != llPath {
			if werr := os.WriteFile(fallback, ir, 0o644); werr == nil {
				llPath = fallback
			}
		}
		fmt.Fprintf(os.Stderr, "lyrac: %v\n", err)
		fmt.Fprintf(os.Stderr, "  wrote %s; compile it with: clang %s -lm -o %s\n", llPath, llPath, exePath(o))
		return 1
	}

	exe := exePath(o)
	// -lm links libm for the float intrinsics (floor/ceil/round, fmod). It is
	// passed unconditionally: harmless for a program that needs none of them,
	// and matching what the backend's behavioural tests compile with.
	cmd := exec.Command(cc, llPath, "-lm", "-o", exe)
	if out, err := cmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "lyrac: %s failed to compile the emitted IR: %v\n%s", cc, err, out)
		return 1
	}

	fmt.Printf("%s: wrote %s (%s backend)\n", o.path, exe, be.Name())
	if keepLL {
		fmt.Printf("  kept %s\n", llPath)
	}
	return 0
}

// exePath is where the executable goes: -o if given, else the source path with
// its .lyra extension dropped.
func exePath(o buildOptions) string {
	if o.out != "" {
		return o.out
	}
	return replaceExt(o.path, "")
}

// findCC resolves the C compiler that assembles and links the emitted IR:
// --cc, else $LYRA_CC, else clang on PATH. It must understand LLVM IR as an
// input file, so `cc` is not a fallback — gcc would reject the .ll with a
// confusing error rather than a clear one.
func findCC(override string) (string, error) {
	cc := "clang"
	for _, candidate := range []string{override, os.Getenv("LYRA_CC")} {
		if candidate != "" {
			cc = candidate
			break
		}
	}
	path, err := exec.LookPath(cc)
	if err != nil {
		return "", fmt.Errorf("cannot run %q: %v", cc, err)
	}
	return path, nil
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
