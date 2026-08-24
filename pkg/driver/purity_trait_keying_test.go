package driver_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Lyra-Language/lyra/pkg/driver"
	"github.com/Lyra-Language/lyra/pkg/modules"
)

// analyzeFiles resolves and analyzes a small multi-module program written to a temp dir.
// The entry file is `main.lyra`; every other entry in files is a sibling module.
func analyzeFiles(t *testing.T, files map[string]string) *driver.Result {
	t.Helper()
	dir := t.TempDir()
	for name, src := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	units, diags := modules.Resolve(filepath.Join(dir, "main.lyra"), []string{dir}, modules.Options{})
	if len(units) == 0 {
		t.Fatalf("resolve produced no units: %v", diags)
	}
	return driver.AnalyzeUnits(units)
}

// A trait's declared effect bound must survive another module declaring a trait of the
// same name.
//
// The purity pass indexed trait declarations in a `map[string]*ast.TraitDeclStmt` built by
// walking the merged program — last-writer-wins on a bare name, which is exactly what rule 4
// forbids and what its trait-specific corollary already named once for
// `checkImplCoherence`. An impl inherits its trait's bound through that map
// (`effectiveMethodBounds` ORs the declared `pure`/`det`/`noalloc` into the method's own),
// so resolving to the *wrong* trait means inheriting the wrong bound.
//
// Here `lib` declares `pure say` and implements it with a body that prints. That is an
// error, and it is reported — until `main` declares its own unrelated `Speak`, at which
// point `lib`'s impl inherits main's (absent) bound and the contract is silently dropped.
// The only thing said about the collision is lyra-W016, which is about which declaration a
// *reference* means and has nothing to say about a bound going missing.
//
// The pass had no SymbolTable at all, which is why it could not do the lookup rule 4
// requires; threading one in is what the fix consists of.
func TestPurity_TraitBoundSurvivesASameNamedTraitInAnotherModule(t *testing.T) {
	const lib = `module lib
pub trait Speak { pure say: (Self) -> string }
pub struct Loud { n: i64 }
impl Speak for Loud {
  say = (self) => {
    print("side effect")
    "l"
  }
}
`
	for _, c := range []struct{ name, main string }{
		// The baseline: no collision, and the bound is enforced.
		{"no collision", `module main
import lib
let main = () -> void => println("hi")
`},
		// The regression: main declares a trait that merely shares the name.
		{"main declares its own Speak", `module main
import lib
trait Speak { say: (Self) -> string }
struct Quiet { n: i64 }
impl Speak for Quiet {
  say = (self) => "q"
}
let main = () -> void => println("hi")
`},
	} {
		t.Run(c.name, func(t *testing.T) {
			res := analyzeFiles(t, map[string]string{"lib.lyra": lib, "main.lyra": c.main})
			var found bool
			for _, d := range res.Errors() {
				if strings.Contains(d.Message, `pure function calls impure function "print"`) {
					found = true
				}
			}
			if !found {
				t.Errorf("the `pure say` bound was not enforced on lib's impl; diagnostics: %v",
					res.Diagnostics)
			}
		})
	}
}
