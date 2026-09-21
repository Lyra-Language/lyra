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
// buildLyrafmt compiles the formatter and answers the binary's path, skipping the test
// when the pieces a linker needs are absent — a C compiler, the tree-sitter runtime, and
// the grammar checked out beside this repo. The skip is about the linker, not the rule
// under test, which is why it is a skip and not a failure; CI installs all three, and did
// not until 09/20, which is how an x86-64 crash lived here unseen.
func buildLyrafmt(t *testing.T) string {
	t.Helper()
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
	return bin
}

func TestExample_LyrafmtRoundTrips(t *testing.T) {
	root := repoRoot(t)
	bin := buildLyrafmt(t)
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
	// between `=` and a constructor), a run of blank lines, spacing missing, forbidden and
	// doubled around `,` `:` `=>`, and a trailing newline.
	//
	// A record update written across lines puts every field at one level, the first
	// included: the `|` after its base is a separator, as a comma is (09/18). Before, the
	// first field read as continuing the base and sat a level deeper than the rest.
	//
	// The inline-block rule is here too: braces that share a line hold their contents one
	// space away (`{n + 1}`, an arm list closing tight), empty braces close up to `{}`, and
	// an interpolation's `${…}` is not a block — those braces stay against the expression,
	// which is a leaf's business and not a block's.
	//
	// Three things must survive untouched, and each is a **hidden token** the grammar gives
	// no node — bytes that sit in the gap between two leaves with nothing naming them, so a
	// formatter that assumes a gap is whitespace destroys them: a raw string's own
	// backticks, a float's mantissa before its exponent, and (a node, but tight by rule) a
	// range's step. What comes out is fixed once — the second run changes nothing.
	// A Lyra raw string is backtick-delimited, and a backtick cannot appear inside a Go
	// raw string — so the lines exercising them are concatenated in.
	const bt = "\x60"
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
  let wide   =   a   +   1
  match a { 1=>2, _ =>3 }
}
let tight = (n: i64) -> i64 => {n + 1}
let arm = (n: i64) -> i64 => match n { 1 => 2, _ => 3}
let empty = () -> void => { }
let interp = (n: i64) -> string => {"x${n}y"}
struct Pt { x: i64, y: i64 }
let updated = (p: Pt) -> Pt => Pt { p |
        x: 1,
    y: 2,
  }
let hidden = () -> f64 => 1.0e30 + 2.5e-3
let raw_line = () -> string =>
  `+bt+`starts a line`+bt+`
let raw_tail = () -> string => #`+bt+`holds a `+bt+` backtick`+bt+`#
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
  let wide = a + 1
  match a { 1 => 2, _ => 3 }
}
let tight = (n: i64) -> i64 => { n + 1 }
let arm = (n: i64) -> i64 => match n { 1 => 2, _ => 3 }
let empty = () -> void => {}
let interp = (n: i64) -> string => { "x${n}y" }
struct Pt { x: i64, y: i64 }
let updated = (p: Pt) -> Pt => Pt { p |
  x: 1,
  y: 2,
}
let hidden = () -> f64 => 1.0e30 + 2.5e-3
let raw_line = () -> string =>
  ` + bt + `starts a line` + bt + `
let raw_tail = () -> string => #` + bt + `holds a ` + bt + ` backtick` + bt + `#
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
	// **The output must still be a program.** A formatter that rewrites a gap holding a
	// hidden token produces something that reads almost right and no longer parses, which
	// is how `1.0e30` became `e30` and a raw string lost its opening backtick (09/15).
	if _, stderr, code := captureRun(t, "check", formatted); code != 0 {
		t.Errorf("the formatted file no longer checks (exit %d):\n%s", code, stderr)
	}
	// Every file above is formatted already, so `--check` passes on all of them.
	if out, err := exec.Command(bin, append([]string{"--check"}, files...)...).CombinedOutput(); err != nil {
		t.Errorf("--check on formatted files: %v\n%s", err, out)
	}

	// **A directory is every `.lyra` file beneath it**, which is what makes `lyrafmt
	// --check .` a CI step. Four rules at once, and three of them are about what the walk
	// does *not* visit:
	//
	//   - it recurses, and the order is `read_dir`'s (sorted), so the report is the same
	//     on every machine — the reason the output below can be compared literally;
	//   - a name beginning with `.` is skipped while walking, so `.git` costs nothing;
	//   - a file that is not `.lyra` is not a candidate;
	//   - **a symlink is not descended.** This repo's own `build/std` points back at
	//     `std/`, so a walk that follows links formats the prelude twice under two names,
	//     and a link into an ancestor never terminates. The link here points at a sibling
	//     directory, so following it would report `sub/b.lyra` a second time as
	//     `link/b.lyra`.
	tree := t.TempDir()
	for _, dir := range []string{"sub", filepath.Join("sub", "deep"), ".hidden"} {
		if err := os.MkdirAll(filepath.Join(tree, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{"a.lyra", ".dotfile.lyra", "readme.txt",
		filepath.Join("sub", "b.lyra"), filepath.Join("sub", "deep", "c.lyra"),
		filepath.Join(".hidden", "h.lyra")} {
		if err := os.WriteFile(filepath.Join(tree, f), []byte("let f = () -> i64 => {1}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(tree, "sub"), filepath.Join(tree, "link")); err != nil {
		t.Fatal(err)
	}
	walked, err := exec.Command(bin, "--check", tree).Output()
	if err == nil {
		t.Errorf("--check over a tree needing changes exited 0")
	}
	wantWalk := "would reformat " + filepath.Join(tree, "a.lyra") + "\n" +
		"would reformat " + filepath.Join(tree, "sub", "b.lyra") + "\n" +
		"would reformat " + filepath.Join(tree, "sub", "deep", "c.lyra") + "\n"
	if string(walked) != wantWalk {
		t.Errorf("walking a directory reported:\n%s\nwant:\n%s", walked, wantWalk)
	}
	// **A path named on the command line is never filtered** — the convention every tool
	// that skips dotfiles keeps. The skip is a rule about walking, not about the file.
	named, err := exec.Command(bin, filepath.Join(tree, ".dotfile.lyra")).Output()
	if err != nil {
		t.Fatalf("formatting a named dotfile: %v", err)
	}
	if got, want := string(named), "let f = () -> i64 => { 1 }\n"; got != want {
		t.Errorf("a named dotfile formatted to %q, want %q", got, want)
	}

	// **`.lyrafmtignore` states what the walk skips**, which is what lets `--check .` be a
	// CI step in a repo that keeps a file meant not to parse (`cmd/lyrac/testdata`). A
	// directory prefix takes everything under it, a comment and a blank line say nothing,
	// and — the rule that matters — a path named on the command line is formatted anyway,
	// exactly as a dotfile is.
	ignore := "# a comment\n\nsub/deep/\na.lyra\n"
	if err := os.WriteFile(filepath.Join(tree, ".lyrafmtignore"), []byte(ignore), 0o644); err != nil {
		t.Fatal(err)
	}
	ignored, err := exec.Command(bin, "--check", tree).Output()
	if err == nil {
		t.Errorf("--check over a tree still needing changes exited 0")
	}
	if got, want := string(ignored), "would reformat "+filepath.Join(tree, "sub", "b.lyra")+"\n"; got != want {
		t.Errorf("with .lyrafmtignore the walk reported:\n%s\nwant:\n%s", got, want)
	}
	// The ignored file, asked for by name.
	askedFor, err := exec.Command(bin, filepath.Join(tree, "a.lyra")).Output()
	if err != nil {
		t.Fatalf("formatting an ignored file by name: %v", err)
	}
	if got, want := string(askedFor), "let f = () -> i64 => { 1 }\n"; got != want {
		t.Errorf("an ignored file named on the command line formatted to %q, want %q", got, want)
	}
}

// **A chain breaks a line per call; a line that merely holds several calls does not**
// (09/21). The rule is what separates the two, and both halves are the test: each case
// here is one line over the 90-column budget, and half of them must come back untouched.
//
// What is a chain is the whole question. Its links apply to one another — `a.b().c()` —
// so a break before each `.` puts one step of a pipeline on each line, with the receiver
// keeping the first. What looks like a chain and is not: `c.f() || c.g()`, whose second
// call applies to `c` again, and `p.a.b.c`, a walk through fields rather than calls. The
// first version of this rule broke both, and the output — a `c` stranded at the end of a
// line, a column of field names — is what sent it back.
func TestExample_LyrafmtBreaksChains(t *testing.T) {
	bin := buildLyrafmt(t)
	cases := []struct{ name, src, want string }{
		{
			"a chain of calls breaks, one call per line",
			"let names = () -> string => people().filter(is_active).map(full_name).sorted().join(\", \").trim()\n",
			"let names = () -> string => people()\n  .filter(is_active)\n  .map(full_name)\n  .sorted()\n  .join(\", \")\n  .trim()\n",
		},
		{
			// Not a chain, and the condition rule takes it instead — a break after each
			// `||`, never one before a `.`. It is the same line either rule would have
			// claimed, and which one claims it is the whole distinction.
			"calls joined by an operator break as a condition, not as a chain",
			"let is_word = pure (c: rune) -> bool => c.is_ascii_alpha() || c.is_ascii_digit() || c == '_'\n",
			"let is_word = pure (c: rune) -> bool => c.is_ascii_alpha() ||\n  c.is_ascii_digit() ||\n  c == '_'\n",
		},
		{
			"a walk through fields is not a chain",
			"let id = (m: Model) -> i64 => m.materials.offsets.maps.textures.entries.first.id.value.inner\n",
			"let id = (m: Model) -> i64 => m.materials.offsets.maps.textures.entries.first.id.value.inner\n",
		},
		{
			"one call is not a chain, however long the line",
			"let only = () -> string => people_of_the_longest_possible_name_there_is().joined_at_last(\"-\")\n",
			"let only = () -> string => people_of_the_longest_possible_name_there_is().joined_at_last(\"-\")\n",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// **Each case must be over the budget to be a case at all**: a line the
			// formatter leaves alone proves nothing about chains if it simply fits. The
			// first draft of this test had three that did.
			if width := len(strings.TrimRight(c.src, "\n")); width <= 90 {
				t.Fatalf("the input is %d columns, so nothing would break it; make it longer", width)
			}
			path := filepath.Join(t.TempDir(), "in.lyra")
			if err := os.WriteFile(path, []byte(c.src), 0o644); err != nil {
				t.Fatal(err)
			}
			got, err := exec.Command(bin, path).Output()
			if err != nil {
				t.Fatalf("formatting: %v", err)
			}
			if string(got) != c.want {
				t.Errorf("formatted:\n%s\nwant:\n%s", got, c.want)
			}
		})
	}
}

// **A long boolean condition breaks after its operators, at one precedence level** — the
// third breakable construct, and the one the language constrains rather than taste (09/21).
//
// **After, never before.** A line beginning with `&&` does not continue the statement
// above it: only `.`, `|`, `else` and `where` do, so a leading `&&` is re-read as two
// address-of operators and the file stops compiling. That is why the conditions written by
// hand in this repo trail their operators, and why this rule could never have matched the
// leading-operator style other languages' formatters use.
//
// **One level at a time**: with both present the `||`s break and the `&&`s stay, so the
// `&&` groups read as the units they are. Breaking both would put operands of different
// precedence at one indent — a formatter lying about grouping, which is worse than a long
// line.
func TestExample_LyrafmtBreaksConditions(t *testing.T) {
	bin := buildLyrafmt(t)
	cases := []struct{ name, src, want string }{
		{
			"operands break after the operator, which is where they may",
			"let punct = pure (c: rune) -> bool => {\n  c.is_printable() && !c.is_alpha() && !c.is_digit() && !c.is_space() && !c.is_ascii_control()\n}\n",
			"let punct = pure (c: rune) -> bool => {\n  c.is_printable() &&\n    !c.is_alpha() &&\n    !c.is_digit() &&\n    !c.is_space() &&\n    !c.is_ascii_control()\n}\n",
		},
		{
			"only the loosest operator breaks, so the groups stay whole",
			"let pick = pure (a: bool, b: bool, c: bool, d: bool) -> bool => {\n  a && longer_name(b) || b && other_name(c) || c && third_name(d) || d && last_name_here(a)\n}\n",
			"let pick = pure (a: bool, b: bool, c: bool, d: bool) -> bool => {\n  a && longer_name(b) ||\n    b && other_name(c) ||\n    c && third_name(d) ||\n    d && last_name_here(a)\n}\n",
		},
		{
			"a condition a body follows on the same line is left alone",
			"let f = (n: f64) -> bool => {\n  if n >= 0.0 - 9.0e18 && n <= 9.0e18 && n - f64(n.floor()) <= 0.0 { true } else { false_v }\n}\n",
			"let f = (n: f64) -> bool => {\n  if n >= 0.0 - 9.0e18 && n <= 9.0e18 && n - f64(n.floor()) <= 0.0 { true } else { false_v }\n}\n",
		},
		{
			"one operator is not a condition to break",
			"let both = pure (a: bool, b: bool) -> bool => {\n  first_of_a_really_quite_long_name_here_now(a) && second_of_a_really_long_name_as_well_too(b)\n}\n",
			"let both = pure (a: bool, b: bool) -> bool => {\n  first_of_a_really_quite_long_name_here_now(a) && second_of_a_really_long_name_as_well_too(b)\n}\n",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// The condition's own line must be over the budget, or the case proves
			// nothing — the same self-check the chain test needed.
			widest := 0
			for _, line := range strings.Split(c.src, "\n") {
				if len(line) > widest {
					widest = len(line)
				}
			}
			if widest <= 90 {
				t.Fatalf("the widest input line is %d columns, so nothing would break it", widest)
			}
			path := filepath.Join(t.TempDir(), "in.lyra")
			if err := os.WriteFile(path, []byte(c.src), 0o644); err != nil {
				t.Fatal(err)
			}
			got, err := exec.Command(bin, path).Output()
			if err != nil {
				t.Fatalf("formatting: %v", err)
			}
			if string(got) != c.want {
				t.Errorf("formatted:\n%s\nwant:\n%s", got, c.want)
			}
		})
	}
}
