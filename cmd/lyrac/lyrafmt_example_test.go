package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// `examples/lyrafmt/lyrafmt.lyra` is the self-hosting probe: a Lyra program that parses
// Lyra through the tree-sitter grammar over FFI and rebuilds each file from the tree's
// leaves. The round trip must reproduce the input byte for byte, on the formatter itself
// among others. Needs the tree-sitter runtime (`brew install tree-sitter`) and a C compiler
// for the grammar; skips without them, since the question is about the linker.
func TestExample_LyrafmtRoundTrips(t *testing.T) {
	root := repoRoot(t)
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Skip("clang not found on PATH")
	}
	libdir, err := exec.Command("pkg-config", "--variable=libdir", "tree-sitter").Output()
	if err != nil {
		t.Skip("the tree-sitter runtime is not installed (pkg-config tree-sitter); brew install tree-sitter")
	}
	grammar := filepath.Join(root, "..", "tree-sitter-lyra", "src")
	if _, err := os.Stat(filepath.Join(grammar, "parser.c")); err != nil {
		t.Skip("tree-sitter-lyra is not checked out beside lyra")
	}
	// The grammar archive, as examples/lyrafmt/libs.sh builds it, into a temp dir.
	lib := t.TempDir()
	for _, src := range []string{"parser.c", "scanner.c"} {
		obj := filepath.Join(lib, strings.TrimSuffix(src, ".c")+".o")
		if out, err := exec.Command(clang, "-O1", "-std=c11", "-c", filepath.Join(grammar, src), "-I", grammar, "-o", obj).CombinedOutput(); err != nil {
			t.Fatalf("compiling %s: %v\n%s", src, err, out)
		}
	}
	if out, err := exec.Command("ar", "rcs", filepath.Join(lib, "libtree-sitter-lyra.a"),
		filepath.Join(lib, "parser.o"), filepath.Join(lib, "scanner.o")).CombinedOutput(); err != nil {
		t.Fatalf("archiving the grammar: %v\n%s", err, out)
	}
	t.Setenv("LYRA_STD", root)
	t.Setenv("LIBRARY_PATH", lib+string(os.PathListSeparator)+strings.TrimSpace(string(libdir)))
	bin := filepath.Join(t.TempDir(), "lyrafmt")
	if _, stderr, code := captureRun(t, "build", "-o", bin, filepath.Join(root, "examples", "lyrafmt", "lyrafmt.lyra")); code != 0 {
		t.Fatalf("building the example exited %d\nstderr: %s", code, stderr)
	}
	files := []string{
		filepath.Join(root, "examples", "primes.lyra"),
		filepath.Join(root, "examples", "lyrafmt", "lyrafmt.lyra"),
		filepath.Join(root, "std", "prelude", "array.lyra"),
		filepath.Join(root, "bindings", "treesitter", "treesitter.lyra"),
	}
	out, err := exec.Command(bin, files...).CombinedOutput()
	if err != nil {
		t.Fatalf("the round trip failed: %v\n%s", err, out)
	}
	for _, f := range files {
		if !strings.Contains(string(out), "ok       "+f+"\n") {
			t.Errorf("no ok line for %s in:\n%s", f, out)
		}
	}
}
