package driver_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/driver"
	"github.com/Lyra-Language/lyra/pkg/modules"
)

// writeModules writes each source into a fresh directory under its file name, returning
// the path of its main.lyra.
func writeModules(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, src := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return filepath.Join(dir, "main.lyra")
}

// analyzeMain analyzes the program whose entry is main, through cache when it is not nil.
func analyzeMain(t *testing.T, main string, cache *driver.CollectCache) *driver.Result {
	t.Helper()
	repoRoot, _ := filepath.Abs("../..")
	units, err := modules.Resolve(main, []string{repoRoot, filepath.Dir(main)}, modules.DefaultOptions())
	if err != nil {
		t.Fatal(err)
	}
	return driver.AnalyzeUnitsCached(units, cache)
}

func analyzeModules(t *testing.T, files map[string]string) *driver.Result {
	t.Helper()
	return analyzeMain(t, writeModules(t, files), nil)
}

const qualifiedLib = `module lib
pub data Shape = Circle(i64) | Dot
pub struct Pt { x: i64, y: i64 }
pub data Box<t> = Wrap(t)
data Secret = Shh(i64)
`

// A constructor, a literal, a pattern and a type written through a namespace mean that
// module's, beside a same-named constructor and type of this module's own (10/29).
func TestQualifiedNames_MeanTheNamespacesOwn(t *testing.T) {
	res := analyzeModules(t, map[string]string{
		"lib.lyra": qualifiedLib,
		"main.lyra": `module main
import lib
data Shape = Circle(string) | Other
let theirs = pure () -> lib.Shape => lib.Circle(2)
let radius = pure (s: lib.Shape) -> i64 => match s {
  lib.Circle(r) => r,
  lib.Dot => 0,
}
let mine = pure (s: Shape) -> string => match s { Circle(n) => n, Other => "" }
let main = () -> void => {
  let p = lib.Pt { x: 1, y: 2 }
  let b: lib.Box<i64> = lib.Wrap(3)
  let lib.Wrap(v) = b else { return }
  println("${radius(theirs())} ${mine(Circle("x"))} ${p.x} ${v}")
}
`,
	})
	if errs := res.Errors(); len(errs) != 0 {
		t.Fatalf("want a clean analysis, got %v", errs)
	}
}

// The erasure is re-derived from the node on every analysis: the collect cache shares the
// AST of every file but the edited one with the next run, whose symbol table starts without
// the record of what the qualifier named. Lost, `lib.Circle(2)` in the cached module means
// its own `Circle(string)` to everything that asks afterwards — the editor's hover and
// go-to-definition among them — though the cached type check never notices.
func TestQualifiedNames_SurviveACachedReanalysis(t *testing.T) {
	cache := driver.NewCollectCache()
	files := map[string]string{
		"lib.lyra": qualifiedLib,
		"middle.lyra": `module middle
import lib
data Shape = Circle(string) | Other
pub let theirs = pure () -> lib.Shape => lib.Circle(2)
pub let radius = pure (s: lib.Shape) -> i64 => match s { lib.Circle(r) => r, lib.Dot => 0 }
`,
		"main.lyra": `module main
import middle.{ theirs, radius }
let main = () -> void => println("${radius(theirs())}")
`,
	}
	main := writeModules(t, files)
	for run := 1; run <= 2; run++ {
		res := analyzeMain(t, main, cache)
		if errs := res.Errors(); len(errs) != 0 {
			t.Fatalf("analysis %d: want it clean, got %v", run, errs)
		}
		var circle *ast.TupleLiteralExpr
		for _, node := range res.Program.Statements {
			stmt, ok := node.(ast.Statement)
			if !ok || !strings.HasSuffix(node.GetLocation().File, "middle.lyra") {
				continue
			}
			ast.WalkStmt(stmt, nil, func(e ast.Expression) bool {
				if tl, ok := e.(*ast.TupleLiteralExpr); ok && tl.Name == "Circle" {
					circle = tl
				}
				return true
			})
		}
		if circle == nil {
			t.Fatalf("analysis %d: no Circle construction in middle", run)
		}
		owner, ok := res.SymbolTable.DeclaringDataType(circle.Name, circle.GetLocation())
		if !ok || !strings.HasSuffix(owner.GetLocation().File, "lib.lyra") {
			t.Errorf("analysis %d: lib.Circle(2) in a cached module resolves to %v, want lib's Shape", run, owner)
		}
	}
}

// Each refusal is one diagnostic that says what is wrong, and the typechecker's own
// "undefined" does not follow it.
func TestQualifiedNames_Refusals(t *testing.T) {
	cases := []struct{ body, want string }{
		{"let x = lib.Shh(1)", `lib.Shh is private to module "lib"`},
		{"let x = lib.Nope(1)", `module "lib" has no type or constructor "Nope"`},
		{"let x = nothing.Circle(1)", `no namespace import named "nothing" here`},
		{"let x = match lib.Dot { lib.Nope => 1, _ => 0 }", `module "lib" has no type or constructor "Nope"`},
		{"let x: lib.Nope = lib.Dot", `module "lib" has no type "Nope"`},
		{"let x: nothing.Pt = lib.Dot", `no namespace import named "nothing" here`},
		{"let x: Shape = lib.Circle(1)", "cannot assign lib.Shape to main.Shape"},
		{"let x = match Other { lib.Circle(r) => r, _ => 0 }", "lib.Circle builds lib.Shape, but this match is on"},
	}
	for _, c := range cases {
		res := analyzeModules(t, map[string]string{
			"lib.lyra": qualifiedLib,
			"main.lyra": "module main\nimport lib\ndata Shape = Circle(i64) | Other\nlet main = () -> void => {\n  " +
				c.body + "\n  println(\"${x == x}\")\n}\n",
		})
		var found int
		var all []string
		for _, d := range res.Errors() {
			all = append(all, d.Message)
			if strings.Contains(d.Message, c.want) {
				found++
			}
		}
		if found != 1 {
			t.Errorf("%q: want one error containing %q, got %v", c.body, c.want, all)
		}
		if strings.Contains(c.want, "no type or constructor") || strings.Contains(c.want, "private") {
			for _, msg := range all {
				if strings.Contains(msg, "undefined") || strings.Contains(msg, "not a constructor") {
					t.Errorf("%q: the refusal was followed by %q", c.body, msg)
				}
			}
		}
	}
}

// A namespace import used only through qualified names is used (lyra-W004).
func TestQualifiedNames_CountTheImportAsUsed(t *testing.T) {
	for _, body := range []string{
		"let x = lib.Dot",
		"let x: lib.Pt = lib.Pt { x: 1, y: 2 }",
		"let x = match lib.Dot { lib.Dot => 1, _ => 0 }",
	} {
		res := analyzeModules(t, map[string]string{
			"lib.lyra":  qualifiedLib,
			"main.lyra": "module main\nimport lib\nlet main = () -> void => {\n  " + body + "\n  println(\"${x == x}\")\n}\n",
		})
		for _, d := range res.Diagnostics {
			if strings.Contains(d.Message, "never used") && strings.Contains(d.Message, "lib") {
				t.Errorf("%q: %s", body, d.Message)
			}
		}
	}
}
