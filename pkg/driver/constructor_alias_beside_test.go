package driver_test

import (
	"strings"
	"testing"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/driver"
)

const aliasSdl = "module sdl\npub data Event = KeyDown(u32) | Quit\n"

// A constructor imported under an alias is the one the alias names beside another module's
// constructor of its name — found 10/21 importing SDL's `KeyDown` beside `ui.backend`'s, and
// refused from then until 11/24, since once the alias was erased the two were one name. Each
// side's payload and type are its own: assigning one to the other's type is refused.
func TestConstructorAlias_BesideAnotherImportOfItsName(t *testing.T) {
	res := analyzeModules(t, map[string]string{
		"sdl.lyra": aliasSdl,
		"gui.lyra": "module gui\npub data Input = KeyDown(string) | Closed\n",
		"main.lyra": `import sdl.{ Event, KeyDown as SdlKeyDown }
import gui.{ Input, KeyDown }
let main = () -> u8 => {
  let e = SdlKeyDown(1)
  let i = KeyDown("a")
  let back: Event = e
  let forth: Input = i
  0
}
`,
	})
	if errs := res.Errors(); len(errs) != 0 {
		t.Fatalf("want a clean analysis, got %v", errs)
	}
	res = analyzeModules(t, map[string]string{
		"sdl.lyra": aliasSdl,
		"gui.lyra": "module gui\npub data Input = KeyDown(u32) | Closed\n",
		"main.lyra": `import sdl.{ Event, KeyDown as SdlKeyDown }
import gui.{ Input, KeyDown }
let main = () -> u8 => {
  let wrong: Input = SdlKeyDown(1)
  let also: Event = KeyDown(2)
  0
}
`,
	})
	got := strings.Join(errorMessages(res), "\n")
	if !strings.Contains(got, "cannot assign sdl.Event to gui.Input") && !strings.Contains(got, "cannot assign Event to Input") {
		t.Errorf("want SdlKeyDown refused as an Input, got\n%s", got)
	}
	if !strings.Contains(got, "cannot assign gui.Input to sdl.Event") && !strings.Contains(got, "cannot assign Input to Event") {
		t.Errorf("want KeyDown refused as an Event, got\n%s", got)
	}
}

// The same against the module's own constructor of that name: the bare name is the
// module's, the alias the import's.
func TestConstructorAlias_BesideAnOwnConstructorOfItsName(t *testing.T) {
	res := analyzeModules(t, map[string]string{
		"sdl.lyra": aliasSdl,
		"main.lyra": `import sdl.{ Event, KeyDown as SdlKeyDown }
data Input = KeyDown(string) | Closed
let main = () -> u8 => {
  let e: Event = SdlKeyDown(1)
  let i: Input = KeyDown("a")
  let n = match e { SdlKeyDown(k) => k, Quit => 0 }
  0
}
`,
	})
	if errs := res.Errors(); len(errs) != 0 {
		t.Fatalf("want a clean analysis, got %v", errs)
	}
}

// The original name written bare is not the alias's import: the alias is what the module
// says, as for a type imported under another name. (`wm` sorts after `sdl`, so a tie between
// the two would go to SDL's.)
func TestConstructorAlias_TheOriginalNameBareIsAnother(t *testing.T) {
	res := analyzeModules(t, map[string]string{
		"sdl.lyra": aliasSdl,
		"wm.lyra":  "module wm\npub data Input = KeyDown(string) | Closed\n",
		"main.lyra": `import sdl.{ Event, KeyDown as SdlKeyDown }
import wm.{ Input, KeyDown }
let main = () -> u8 => {
  let i: Input = KeyDown("a")
  0
}
`,
	})
	if errs := res.Errors(); len(errs) != 0 {
		t.Fatalf("want KeyDown(\"a\") to be wm's, got %v", errs)
	}
}

// The alias is kept on the node and recorded again on every analysis: the collect cache
// shares an unedited file's AST with the next run, its alias already erased, and the record
// of which module the use meant starts empty. Lost, a cached `SdlKeyDown(1)` meant the
// bare `KeyDown` beside it.
func TestConstructorAlias_SurvivesACachedReanalysis(t *testing.T) {
	cache := driver.NewCollectCache()
	main := writeModules(t, map[string]string{
		"sdl.lyra": aliasSdl,
		"gui.lyra": "module gui\npub data Input = KeyDown(string) | Closed\n",
		"middle.lyra": `module middle
import sdl.{ Event, KeyDown as SdlKeyDown }
import gui.{ Input, KeyDown }
pub let theirs = pure () -> Event => SdlKeyDown(1)
pub let ours = pure () -> Input => KeyDown("a")
`,
		"main.lyra": `module main
import middle.{ theirs, ours }
let main = () -> void => {
  let _ = theirs()
  let _ = ours()
}
`,
	})
	for run := 1; run <= 2; run++ {
		res := analyzeMain(t, main, cache)
		if errs := res.Errors(); len(errs) != 0 {
			t.Fatalf("analysis %d: want it clean, got %v", run, errs)
		}
		var down *ast.TupleLiteralExpr
		for _, node := range res.Program.Statements {
			stmt, ok := node.(ast.Statement)
			if !ok || !strings.HasSuffix(node.GetLocation().File, "middle.lyra") {
				continue
			}
			ast.WalkStmt(stmt, nil, func(e ast.Expression) bool {
				if tl, ok := e.(*ast.TupleLiteralExpr); ok && tl.Alias == "SdlKeyDown" {
					down = tl
				}
				return true
			})
		}
		if down == nil {
			t.Fatalf("analysis %d: no SdlKeyDown construction in middle", run)
		}
		owner, ok := res.SymbolTable.DeclaringDataType(down.Name, down.GetLocation())
		if !ok || !strings.HasSuffix(owner.GetLocation().File, "sdl.lyra") {
			t.Errorf("analysis %d: SdlKeyDown(1) in a cached module resolves to %v, want sdl's Event", run, owner)
		}
	}
}

// A type imported under an alias and written only inside a declaration — a field — is used.
// Stamping replaces the alias there with the declared name (typechecker.stampNames), which
// is all lyra-W004 can read once the typechecker has run, so the use is passed to it
// (AliasedTypeUses) rather than found.
func TestUnusedImport_AnAliasedTypeWrittenOnlyInAFieldIsUsed(t *testing.T) {
	got := unusedImportWarnings(t, map[string]string{
		"owner.lyra": "module owner\npub struct Box { n: i64 }\npub struct Pair<t> { a: t }\n",
		"main.lyra": `import owner.{ Box as B, Pair as P }
struct Holder { b: B, p: P<i64> }
let main = () -> u8 => 0
`,
	})
	if len(got) != 0 {
		t.Fatalf("want no W004, got %v", got)
	}
}
