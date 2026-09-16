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
// among others, and the indentation rule must fix a badly indented file once and then
// leave it alone. Needs the tree-sitter runtime (`brew install tree-sitter`) and a C
// compiler for the grammar; skips without them, since the question is about the linker.
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
	// With no rule in force the output is the input: `--check` finds nothing to change.
	if out, err := exec.Command(bin, append([]string{"--roundtrip", "--check"}, files...)...).CombinedOutput(); err != nil {
		t.Fatalf("the round trip changed a file: %v\n%s", err, out)
	}
	// Every rule at once, on a file that breaks each of them: indentation (a wrapped
	// array, match arms at odd depths, a block body, a `data` type's constructors after
	// `=`, an `else` on its own line, a wrapped signature with its body, a doc comment
	// between `=` and a constructor), a run of blank lines, spacing that is missing or
	// forbidden around `,` `:` `=>`, and a trailing newline. Two things must survive
	// untouched: a range's tight step (`0..<10:2`), and the aligned `=>` of a match, which
	// is a choice the author made and not a gap to collapse. What comes out is fixed once
	// — the second run changes nothing.
	ugly := filepath.Join(t.TempDir(), "ugly.lyra")
	if err := os.WriteFile(ugly, []byte(`let f = (n: i64) -> i64 => {
      let xs = [
1,
        2,
]
  match n {
   0 => 1,
_ => {
  xs.len()
  },
 }
}
data E =
      /// doc
  A(i64)
      | B
let g = (n: i64) -> i64 =>
if n > 0 { 1 }
      else { 0 }
let h = (a: i64,
                b: i64) -> i64 => {
      a + b
   }



let spaced = (a: i64,b :i64) -> i64=>{
  let xs = [1 ,2]
  let r = 0..<10:2
  match a { 1=>2, _ =>3 }
}
let aligned = (n: i64) -> i64 => match n {
  1   => 1,
  100 => 2,
  _   => 3,
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	const want = `let f = (n: i64) -> i64 => {
  let xs = [
    1,
    2,
  ]
  match n {
    0 => 1,
    _ => {
      xs.len()
    },
  }
}
data E =
  /// doc
  A(i64)
  | B
let g = (n: i64) -> i64 =>
  if n > 0 { 1 }
  else { 0 }
let h = (a: i64,
  b: i64) -> i64 => {
  a + b
}

let spaced = (a: i64, b: i64) -> i64 => {
  let xs = [1, 2]
  let r = 0..<10:2
  match a { 1 => 2, _ => 3 }
}
let aligned = (n: i64) -> i64 => match n {
  1   => 1,
  100 => 2,
  _   => 3,
}
`
	got, err := exec.Command(bin, ugly).Output()
	if err != nil {
		t.Fatalf("formatting failed: %v", err)
	}
	if string(got) != want {
		t.Errorf("formatted:\n%s\nwant:\n%s", got, want)
	}
	formatted := filepath.Join(t.TempDir(), "formatted.lyra")
	if err := os.WriteFile(formatted, got, 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(bin, "--check", formatted).CombinedOutput(); err != nil {
		t.Errorf("formatting is not a fixed point: %v\n%s", err, out)
	}
	// Every file above is formatted already, so `--check` passes on all of them.
	if out, err := exec.Command(bin, append([]string{"--check"}, files...)...).CombinedOutput(); err != nil {
		t.Errorf("--check on formatted files: %v\n%s", err, out)
	}
}
