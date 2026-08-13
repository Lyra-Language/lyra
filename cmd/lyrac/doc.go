package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Lyra-Language/lyra/pkg/docgen"
	"github.com/Lyra-Language/lyra/pkg/modules"
)

type docOptions struct {
	path           string
	outDir         string
	includePrivate bool
	includeDeps    bool
	includePrelude bool
	strict         bool
}

func parseDocArgs(args []string) (docOptions, bool) {
	o := docOptions{outDir: "docs"}
	for i := 0; i < len(args); i++ {
		switch a := args[i]; a {
		case "-o":
			if i+1 >= len(args) {
				return o, false
			}
			i++
			o.outDir = args[i]
		case "--private":
			o.includePrivate = true
		case "--deps":
			o.includeDeps = true
		case "--prelude":
			// The prelude is a dependency of everything, so asking for it is
			// asking for dependencies.
			o.includePrelude = true
			o.includeDeps = true
		case "--strict":
			o.strict = true
		default:
			if strings.HasPrefix(a, "-") || o.path != "" {
				return o, false
			}
			o.path = a
		}
	}
	return o, o.path != ""
}

// doc renders documentation for the entry file's module, one Markdown page per module.
//
// It runs the same analysis `check` does, and **requires it to be error-free**. That is
// not caution: a signature is rendered from resolved types, so documenting a program
// that does not type-check would print `?` where a type failed to resolve and publish
// the result as though it were the API.
func doc(o docOptions) int {
	res, ok := analyze(o.path)
	if !ok {
		return 2
	}
	printDiagnostics(o.path, res.Diagnostics)
	if res.HasErrors() {
		fmt.Fprintln(os.Stderr, "lyrac: not documenting a program that does not type-check")
		return 1
	}

	entry, ok := entryModule(res.SymbolTable.ModuleOfFile, o.path)
	if !ok {
		fmt.Fprintf(os.Stderr, "lyrac: %s is not part of the analyzed program\n", o.path)
		return 1
	}
	wanted := documentedModules(res.SymbolTable.ModuleOfFile, entry, o)
	mods := docgen.Collect(res, docgen.Options{
		Modules:        wanted,
		IncludePrivate: o.includePrivate,
	})
	if len(mods) == 0 {
		fmt.Fprintln(os.Stderr, "lyrac: nothing to document")
		return 1
	}

	if err := os.MkdirAll(o.outDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "lyrac: %v\n", err)
		return 1
	}
	for _, m := range mods {
		path := filepath.Join(o.outDir, docgen.FileName(m.Path))
		if err := os.WriteFile(path, docgen.RenderMarkdown(m), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "lyrac: %v\n", err)
			return 1
		}
		fmt.Printf("%s\n", path)
	}

	cov := docgen.Measure(mods)
	// Always reported, not only under --strict: a run that documented half its
	// surface and said nothing is the silent-incompleteness this command exists to
	// avoid. The pages list undocumented declarations too, with their signatures.
	fmt.Printf("documented %d/%d public declarations across %d module(s)\n",
		cov.Documented, cov.Total, len(mods))
	if len(cov.Undocumented) > 0 && o.strict {
		for _, name := range cov.Undocumented {
			fmt.Fprintf(os.Stderr, "lyrac: undocumented: %s\n", name)
		}
		return 1
	}
	return 0
}

// entryModule is the module path the file on the command line belongs to.
//
// It matches on the *cleaned absolute* path rather than on the string the user typed:
// the resolver records each unit under the path it read, so `./std/prelude/maybe.lyra`
// and `std/prelude/maybe.lyra` are the same file and only one of them is a key. An
// empty module path is a real answer — the entry file of a single-file program declares
// no module — so the second return says whether the file was found at all.
func entryModule(moduleOfFile map[string]string, path string) (string, bool) {
	if m, ok := moduleOfFile[path]; ok {
		return m, true
	}
	want, err := filepath.Abs(path)
	if err != nil {
		return "", false
	}
	for file, module := range moduleOfFile {
		abs, err := filepath.Abs(file)
		if err == nil && abs == want {
			return module, true
		}
	}
	return "", false
}

// documentedModules decides which modules a run covers: the entry module always, its
// dependencies under --deps, and the prelude only when asked for by name.
//
// The prelude needs its own opt-in even under --deps because it is implicitly imported
// by everything — so without this rule every project's docs would contain a copy of the
// standard library. It is still documented when it *is* the entry module, which is how
// the standard library's own pages get generated.
func documentedModules(moduleOfFile map[string]string, entry string, o docOptions) []string {
	if !o.includeDeps {
		return []string{entry}
	}
	seen := map[string]bool{}
	var out []string
	for _, m := range moduleOfFile {
		if seen[m] {
			continue
		}
		if m == modules.PreludeModule && !o.includePrelude && m != entry {
			continue
		}
		seen[m] = true
		out = append(out, m)
	}
	sort.Strings(out)
	return out
}
