package llvm

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/Lyra-Language/lyra/pkg/driver"
	"github.com/Lyra-Language/lyra/pkg/modules"
)

// emitPartsOf analyzes a program of several files with the prelude and splits it
// (EmitParts). `files` is written to a temp dir; `app.lyra` is the entry. Modules are also
// found in libs, directories of their own (libDir).
func emitPartsOf(t *testing.T, files map[string]string, libs ...string) []Part {
	t.Helper()
	root := sourceTree(t, files)
	roots := append(append([]string{root}, libs...), repoStdRoot(t))
	units, diags := modules.Resolve(filepath.Join(root, "app.lyra"), roots, modules.Options{Prelude: modules.PreludeModule})
	if len(diags) != 0 {
		t.Fatalf("resolve: %v", diags)
	}
	res := driver.AnalyzeUnits(units)
	if res.HasErrors() {
		t.Fatalf("analysis: %v", res.Errors())
	}
	ep, epDiags := driver.ResolveEntryPoint(res)
	if ep == nil {
		t.Fatalf("no entry point: %v", epDiags)
	}
	parts, err := testBackend().EmitParts(res, ep)
	if err != nil {
		t.Fatalf("EmitParts: %v", err)
	}
	return parts
}

// runParts compiles each part on its own and links them, as lyrac does, and returns what
// the program printed.
func runParts(t *testing.T, parts []Part) string {
	t.Helper()
	clang := lookClang(t)
	dir := t.TempDir()
	var objects []string
	for _, p := range parts {
		ll := filepath.Join(dir, p.Name+".ll")
		obj := filepath.Join(dir, p.Name+".o")
		if err := os.WriteFile(ll, p.IR, 0o644); err != nil {
			t.Fatal(err)
		}
		if out, err := exec.Command(clang, "-O1", "-c", ll, "-o", obj).CombinedOutput(); err != nil {
			t.Fatalf("part %s did not compile: %v\n%s", p.Name, err, out)
		}
		objects = append(objects, obj)
	}
	exe := filepath.Join(dir, "prog")
	if out, err := exec.Command(clang, append(objects, "-lm", "-o", exe)...).CombinedOutput(); err != nil {
		t.Fatalf("link: %v\n%s", err, out)
	}
	out, err := command(exe).Output()
	if err != nil {
		t.Fatalf("run: %v\n%s", err, out)
	}
	return string(out)
}

// libDir is a directory holding the library, at one path whichever program reads it — a
// panic message names its file, so a library at another path is another text.
func libDir(t *testing.T) string {
	t.Helper()
	return sourceTree(t, map[string]string{"shapes.lyra": partsLib})
}

func partNamed(parts []Part, name string) (Part, bool) {
	for _, p := range parts {
		if p.Name == name {
			return p, true
		}
	}
	return Part{}, false
}

const partsLib = `module shapes

pub struct Sq { n: i64 }

pub let area = pure (s: Sq) -> i64 => s.n * s.n

pub let label = (s: Sq) -> string => "square ${s.n}"

pub let scaled = (xs: []Sq, k: i64) -> []i64 => xs.map((s) => s.n * k)

pub let total = pure (xs: []i64) -> i64 => {
  var sum = 0
  for x in xs { sum += x }
  sum
}
`

// A program split into parts and linked is the program: closures, generics and their
// specializations, strings and a module's mutable global all reach across parts.
func TestEmitParts_LinkedPartsAreTheProgram(t *testing.T) {
	t.Parallel()
	parts := emitPartsOf(t, map[string]string{
		"shapes.lyra": partsLib,
		"app.lyra": `import shapes.{ Sq, area, label, scaled, total }

var calls = 0

let describe = (s: Sq) -> string => label(s) ++ "!"

let twice<t> = (x: t, f: (t) -> t) -> t => {
  calls += 1
  f(f(x))
}

let main = () -> void => {
  let s = Sq { n: 3 }
  println("${area(s)} ${describe(s)} ${total(scaled([s, Sq { n: 4 }], 10))}")
  println("${twice(5, (n) => n * 3)} ${twice("ab", (x) => x ++ "!")} ${calls}")
}
`,
	})
	for _, want := range []string{"m.main", "m.shapes", "globals"} {
		if _, ok := partNamed(parts, want); !ok {
			var names []string
			for _, p := range parts {
				names = append(names, p.Name)
			}
			t.Errorf("no part %s among %v", want, names)
		}
	}
	got := runParts(t, parts)
	if want := "9 square 3! 70\n45 ab!! 2\n"; got != want {
		t.Errorf("got %q; want %q", got, want)
	}
}

// **A part's text depends only on what it holds**, so a module's object is reused by the
// next build — of this program or another. Two programs that differ in their own code, by
// a closure and a string ahead of everything else, give the library and the prelude parts
// byte for byte: the program-wide numbering of closures, glue and string constants does
// not reach them (stableNames, nameConstants).
func TestEmitParts_AModulesPartIsTheSameInEveryProgram(t *testing.T) {
	t.Parallel()
	lib := libDir(t)
	first := emitPartsOf(t, map[string]string{
		"app.lyra": `import shapes.{ Sq, label, scaled, total }
let main = () -> void => println("${label(Sq { n: 2 })} ${total(scaled([Sq { n: 1 }], 2))}")
`,
	}, lib)
	second := emitPartsOf(t, map[string]string{
		"app.lyra": `import shapes.{ Sq, label, scaled, total }
let early = (xs: []i64) -> []i64 => xs.map((x) => x + 1).filter((x) => x > 2)
let main = () -> void => {
  println("an unrelated string, numbered first")
  println("${total(early([1, 2, 3]))} ${label(Sq { n: 2 })} ${total(scaled([Sq { n: 1 }], 2))}")
}
`,
	}, lib)
	compared := 0
	for _, p := range first {
		if !strings.HasPrefix(p.Name, "m.shapes") && !strings.HasPrefix(p.Name, "m.std.") {
			continue
		}
		q, ok := partNamed(second, p.Name)
		if !ok {
			t.Errorf("part %s is missing from the second program", p.Name)
			continue
		}
		compared++
		if string(p.IR) != string(q.IR) {
			t.Errorf("part %s differs between the programs", p.Name)
		}
	}
	if compared == 0 {
		t.Fatal("no library or prelude parts to compare")
	}
}

// No name numbered program-wide is left in a part: those are what changed a part's text
// when unrelated code changed.
func TestEmitParts_NoProgramWideNumbering(t *testing.T) {
	t.Parallel()
	numbered := regexp.MustCompile(`@(\.str\.|\.cstr\.|lyra_closure_|lyra_env_drop_|lyra_drop_|lyra_retain_|lyra_eq_)\d+`)
	for _, p := range emitPartsOf(t, map[string]string{
		"shapes.lyra": partsLib,
		"app.lyra": `import shapes.{ Sq, scaled, total }
let main = () -> void => {
  let k = 3
  let f = (s: Sq) -> i64 => s.n * k
  println("${total(scaled([Sq { n: 1 }], 2))} ${f(Sq { n: 2 })} ${Some("x") == Some("x")}")
}
`,
	}) {
		if m := numbered.FindString(string(p.IR)); m != "" {
			t.Errorf("part %s names %s", p.Name, m)
		}
	}
}
