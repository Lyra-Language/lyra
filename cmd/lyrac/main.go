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
	"errors"
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
		opts, ok := parseBuildArgs(cmd, rest)
		if !ok {
			usage()
			return 2
		}
		return build(opts)
	case "run":
		opts, ok := parseBuildArgs(cmd, rest)
		if !ok {
			usage()
			return 2
		}
		return runProgram(opts)
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
  run     build to a temporary location and execute it

build flags:
  -o <path>     write the executable here (default: the source path without .lyra)
  --emit-llvm   stop after emitting <name>.ll; do not link an executable
  --keep-ll     keep the emitted <name>.ll beside the executable
  -O<level>     optimization level passed to the C compiler (default: -O2).
                -O0 for the fastest build; -Os for size. No debug info is emitted
                at any level, so -O0 buys build time rather than debuggability.
  --cc <path>   C compiler used to assemble and link (default: $LYRA_CC, else clang)

run flags:
  --cc <path>   as above; run leaves no executable or IR behind
`)
}

// buildOptions is what `lyrac build` (or `run`) was asked for. The zero value
// (plus a path) is the default build: link an executable next to the source and
// keep no IR.
type buildOptions struct {
	path     string // the .lyra source
	out      string // executable path; empty means derive it from path
	emitOnly bool   // --emit-llvm: write the .ll and stop
	keepLL   bool   // --keep-ll: keep the .ll as well as the executable
	cc       string // C compiler override

	// opt is the optimization level handed to the C compiler, as clang spells it
	// ("-O2"). It defaults to -O2 rather than to clang's own -O0 default: this
	// compiler emits **no debug info**, so the usual reason to ship unoptimized —
	// being able to step through the source — buys nothing here, while -O0 costs
	// around 3x on ordinary code. Measured on a string-scan workload: 15925us at
	// -O0 against 5087us at -O2, and an arithmetic loop the optimizer can close
	// disappears entirely. The whole backend behavioural suite passes at -O1, -O2,
	// -O3 and -Os, so this is not resting on -O2 happening to be gentle.
	opt string

	// ephemeral is `run`: every artifact is a temp file, so nothing may be
	// written into the source tree — not even the IR that a failed link
	// otherwise leaves behind for the user to compile by hand.
	ephemeral bool
}

// parseBuildArgs accepts flags before or after the source path, since a build
// command is as often edited by appending a flag as by inserting one. cmd names
// the subcommand: `run` produces a throwaway executable, so the flags choosing
// where artifacts land are rejected rather than quietly ignored.
func parseBuildArgs(cmd string, args []string) (buildOptions, bool) {
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
		if cmd == "run" && (arg == "-o" || arg == "--emit-llvm" || arg == "--keep-ll") {
			fmt.Fprintf(os.Stderr, "lyrac: %s is a build flag; run keeps no artifact (use `lyrac build`)\n", arg)
			return o, false
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
		case isOptFlag(arg):
			// Spelled the way clang spells it, and passed through unexamined
			// beyond that: the C compiler is the authority on which levels it
			// has, so a level this does not know about is its error to report,
			// with its own wording, rather than one to duplicate here.
			o.opt = arg
		case strings.HasPrefix(arg, "-"):
			fmt.Fprintf(os.Stderr, "lyrac: unknown flag %q\n", arg)
			return o, false
		case o.path != "":
			fmt.Fprintf(os.Stderr, "lyrac: %s takes one source file (got %q and %q)\n", cmd, o.path, arg)
			return o, false
		default:
			o.path = arg
		}
	}
	if o.opt == "" {
		o.opt = "-O2"
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
	res, entry, code := typedProgram(o.path)
	if entry == nil {
		return code
	}
	exe, code := lowerAndEmit(o, res, entry)
	if code != 0 {
		return code
	}
	if o.emitOnly {
		return 0
	}
	fmt.Printf("%s: wrote %s (llvm backend)\n", o.path, exe)
	if o.keepLL {
		fmt.Printf("  kept %s\n", replaceExt(o.path, ".ll"))
	}
	return 0
}

// runProgram builds into a temp directory and executes the result, leaving no
// executable and no IR behind. The program's own exit status is passed through,
// which is why nothing here prints a build summary on success: `lyrac run`'s
// output should be the program's, so that piping it works.
//
// The cost of passing the status through is that a program exiting 1 or 2 is
// indistinguishable from a compile error — the same trade `go run` makes, and
// the compiler's own failures are the ones that also print a diagnostic.
func runProgram(o buildOptions) int {
	res, entry, code := typedProgram(o.path)
	if entry == nil {
		return code
	}

	dir, err := os.MkdirTemp("", "lyrac-run-")
	if err != nil {
		fmt.Fprintf(os.Stderr, "lyrac: %v\n", err)
		return 1
	}
	defer os.RemoveAll(dir)

	o.ephemeral = true
	o.out = filepath.Join(dir, filepath.Base(replaceExt(o.path, "")))
	exe, code := lowerAndEmit(o, res, entry)
	if code != 0 {
		return code
	}

	cmd := exec.Command(exe)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			// The program ran and chose this status (or was killed by a signal,
			// which ExitCode reports as -1). Either way it is the program's
			// outcome, not the compiler's, so it is reported without comment.
			if status := exit.ExitCode(); status >= 0 {
				return status
			}
			fmt.Fprintf(os.Stderr, "lyrac: %s: %v\n", filepath.Base(exe), exit)
			return 1
		}
		fmt.Fprintf(os.Stderr, "lyrac: cannot run %s: %v\n", exe, err)
		return 1
	}
	return 0
}

// typedProgram is the front half both build and run need: analyze, report
// diagnostics, and resolve the entry point. A nil entry means it failed and the
// returned code is what the process should exit with.
func typedProgram(path string) (*driver.Result, *driver.EntryPoint, int) {
	res, ok := analyze(path)
	if !ok {
		return nil, nil, 2
	}
	printDiagnostics(path, res.Diagnostics)
	if res.HasErrors() {
		return nil, nil, 1
	}
	entry, entryDiags := driver.ResolveEntryPoint(res)
	printDiagnostics(path, entryDiags)
	if entry == nil {
		return nil, nil, 1
	}
	return res, entry, 0
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
// the IR) and returns its path. The LLVM backend is early (see pkg/backend/llvm's
// doc comment for what's covered); a form it doesn't lower yet returns an error
// here rather than silently emitting wrong code.
//
// It prints nothing on success — the caller knows whether its command has a
// summary to report — so `lyrac run`'s output is the program's alone.
func lowerAndEmit(o buildOptions, res *driver.Result, entry *driver.EntryPoint) (string, int) {
	be := llvm.New()
	ir, err := be.Emit(res, entry)
	if err != nil {
		fmt.Fprintf(os.Stderr, "lyrac: %s backend: %v\n", be.Name(), err)
		return "", 1
	}

	llPath := replaceExt(o.path, ".ll")
	if !o.emitOnly && !o.keepLL {
		// A throwaway .ll: the executable is the artifact, so the IR does not
		// belong in the user's source tree. It still has to reach the compiler
		// as a file, since clang reads IR from a path, not from stdin.
		dir, err := os.MkdirTemp("", "lyrac-")
		if err != nil {
			fmt.Fprintf(os.Stderr, "lyrac: %v\n", err)
			return "", 1
		}
		defer os.RemoveAll(dir)
		llPath = filepath.Join(dir, filepath.Base(replaceExt(o.path, ".ll")))
	}
	if err := os.WriteFile(llPath, ir, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "lyrac: %v\n", err)
		return "", 1
	}

	if o.emitOnly {
		fmt.Printf("%s: wrote %s (%s backend)\n", o.path, llPath, be.Name())
		fmt.Printf("  compile with: clang %s %s -lm -o %s\n", o.opt, llPath, exePath(o))
		return llPath, 0
	}

	cc, err := findCC(o.cc)
	if err != nil {
		// Keep the IR on this path whatever the flags said: without it the user
		// has nothing to hand to a compiler they install later. `run` is the
		// exception — it promised to leave nothing behind, and a temp path in
		// the message would name a file already deleted.
		if !o.ephemeral {
			fallback := replaceExt(o.path, ".ll")
			if fallback != llPath {
				if werr := os.WriteFile(fallback, ir, 0o644); werr == nil {
					llPath = fallback
				}
			}
			fmt.Fprintf(os.Stderr, "lyrac: %v\n", err)
			fmt.Fprintf(os.Stderr, "  wrote %s; compile it with: clang %s %s -lm -o %s\n", llPath, o.opt, llPath, exePath(o))
			return "", 1
		}
		fmt.Fprintf(os.Stderr, "lyrac: %v\n", err)
		return "", 1
	}

	exe := exePath(o)
	// -lm links libm for the float intrinsics (floor/ceil/round, fmod). It is
	// passed unconditionally: harmless for a program that needs none of them,
	// and matching what the backend's behavioural tests compile with.
	cmd := exec.Command(cc, o.opt, llPath, "-lm", "-o", exe)
	if out, err := cmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "lyrac: %s failed to compile the emitted IR: %v\n%s", cc, err, out)
		return "", 1
	}
	return exe, 0
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

// isOptFlag reports whether arg is a clang optimization-level flag: -O followed by
// anything, which covers -O0..-O3 plus -Os, -Oz and -Ofast without this having to
// track which of them a given clang accepts.
//
// The permissive match is deliberate. Enumerating the levels here would mean a
// second, staler copy of the C compiler's own list — and the failure mode of
// getting it wrong is refusing a level that works, which is worse than passing one
// through and letting clang say what it thinks.
func isOptFlag(arg string) bool {
	return strings.HasPrefix(arg, "-O") && len(arg) > 2
}
