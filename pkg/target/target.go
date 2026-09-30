// Package target is what a program is compiled *for*: the host (the machine the compiler
// runs on, with an operating system, a heap and 64-bit arithmetic), or a console such as
// the Genesis, whose 68000 has none of those. A project says which in `lyra.toml`:
//
//	target = "genesis"
//
// found by looking up from the entry file's directory, so `lyrac` and `lyra-lsp` agree —
// the target is a property of the code, not of an editor's settings.
//
// The front end reads the answer to check what cannot work there (checker.CheckTarget):
// code that reaches outside Lyra (an `extern`, or a host builtin like `println`), heap
// allocation where there is no heap, and arithmetic the CPU can only emulate.
package target

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

// Target is one thing a program can be compiled for.
type Target struct {
	// Name is how `lyra.toml` spells it.
	Name string
	// Display is how a diagnostic names it: "the Genesis".
	Display string
	// Host is the machine the compiler runs on: an operating system, libc, files, a
	// terminal. Anything reaching outside Lyra works only here.
	Host bool
	// Heap is whether there is an allocator — dynamic arrays, built strings, `shared`
	// values and capturing closures all need one.
	Heap bool
	// CPU names the processor, for a diagnostic about emulated arithmetic.
	CPU string
	// Emulated are the types the CPU has no instructions for, so each operation on them
	// is a sequence of instructions or a call: 64-bit integers and floats on a 68000.
	Emulated []string
}

// HostTarget is the default: no `lyra.toml`, or one naming no target.
var HostTarget = Target{Name: "host", Display: "the host", Host: true, Heap: true}

// targets are the known ones, by the name `lyra.toml` uses.
var targets = map[string]Target{
	"host": HostTarget,
	// The Sega Genesis / Mega Drive: a 7.67 MHz 68000 with 64 KB of RAM. No operating
	// system, and no allocator until Vega's runtime has one. The 68000 multiplies and
	// divides 16-bit values, adds 32-bit ones, and has no floating point.
	"genesis": {
		Name:     "genesis",
		Display:  "the Genesis",
		Host:     false,
		Heap:     false,
		CPU:      "68000",
		Emulated: []string{"i64", "u64", "f32", "f64"},
	},
}

// Names lists the known targets, sorted, for a diagnostic.
func Names() []string {
	names := make([]string, 0, len(targets))
	for name := range targets {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Lookup returns the target `lyra.toml` names `name`.
func Lookup(name string) (Target, bool) {
	t, ok := targets[name]
	return t, ok
}

// ConfigName is the project file's name.
const ConfigName = "lyra.toml"

// Problem is what is wrong with a `lyra.toml`: its path, the 1-based line (0 for the
// file as a whole) and a message.
type Problem struct {
	Path    string
	Line    int
	Message string
}

// ForFile finds the `lyra.toml` governing the source file at path — in its directory or
// the nearest above it — and returns its target: the host when there is none, or when
// path is empty (a buffer with no file). A file that cannot be read or understood is a
// Problem, with the host as the target.
func ForFile(path string) (Target, *Problem) {
	if path == "" {
		return HostTarget, nil
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return HostTarget, nil
	}
	for dir := filepath.Dir(abs); ; {
		config := filepath.Join(dir, ConfigName)
		if data, err := os.ReadFile(config); err == nil {
			return parse(config, data)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return HostTarget, nil
		}
		dir = parent
	}
}

// parse reads a `lyra.toml`. Only what Lyra uses is understood — top-level `key =
// "string"` lines, `#` comments and blank lines — and anything else is a Problem rather
// than silently ignored, since an ignored setting is a program built for the wrong
// machine.
func parse(path string, data []byte) (Target, *Problem) {
	result := HostTarget
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for line := 1; scanner.Scan(); line++ {
		text := strings.TrimSpace(stripComment(scanner.Text()))
		if text == "" {
			continue
		}
		key, value, ok := strings.Cut(text, "=")
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if !ok || key == "" {
			return HostTarget, &Problem{path, line, fmt.Sprintf("expected `key = \"value\"`, found %q", text)}
		}
		str, isString := unquote(value)
		if !isString {
			return HostTarget, &Problem{path, line, fmt.Sprintf("%s: expected a quoted string, found %s", key, value)}
		}
		switch key {
		case "target":
			t, known := Lookup(str)
			if !known {
				return HostTarget, &Problem{path, line,
					fmt.Sprintf("unknown target %q; the targets are %s", str, strings.Join(Names(), ", "))}
			}
			result = t
		default:
			return HostTarget, &Problem{path, line, fmt.Sprintf("unknown setting %q; the one there is is `target`", key)}
		}
	}
	return result, nil
}

// stripComment removes a `#` comment, leaving a `#` inside a quoted string alone.
func stripComment(line string) string {
	inString := false
	for i, r := range line {
		switch {
		case r == '"':
			inString = !inString
		case r == '#' && !inString:
			return line[:i]
		}
	}
	return line
}

// unquote reads a basic TOML string: double quotes, with `\"` and `\\` escapes.
func unquote(value string) (string, bool) {
	if len(value) < 2 || value[0] != '"' || value[len(value)-1] != '"' {
		return "", false
	}
	inner := value[1 : len(value)-1]
	var out strings.Builder
	for i := 0; i < len(inner); i++ {
		c := inner[i]
		if c == '\\' && i+1 < len(inner) {
			i++
			out.WriteByte(inner[i])
			continue
		}
		if c == '"' {
			return "", false
		}
		out.WriteByte(c)
	}
	return out.String(), true
}

// Emulates reports whether arithmetic on the type named `name` is emulated on t.
func (t Target) Emulates(name string) bool { return slices.Contains(t.Emulated, name) }
