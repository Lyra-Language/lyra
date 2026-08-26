package driver_test

import (
	"strings"
	"testing"

	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
)

// unusedImports returns the lyra-W004 messages a program produces.
func unusedImports(t *testing.T, files map[string]string) []string {
	t.Helper()
	var out []string
	for _, d := range analyzeFiles(t, files).Diagnostics {
		if d.Code == diag.CodeUnusedImport {
			out = append(out, d.Message)
		}
	}
	return out
}

const traitLib = `module lib
pub trait Tag { pure raw: (Self) -> string }
impl Tag for i64 { raw = pure (self) => "one" }
pub struct Vec2 { x: i64, y: i64 }
pub trait Plus { (_+_): (Self, Self) -> Self }
impl Plus for Vec2 { (_+_) = pure (self, o) => Vec2 { x: self.x + o.x, y: self.y + o.y } }
pub trait Never { pure nope: (Self) -> i64 }
pub let helper = pure () -> i64 => 1
`

// **A trait reached by dispatch is used**, though nothing in the source names it:
// `(7).raw()` resolves against the receiver's type, not through the import's bound name.
// The import is nonetheless what brought the impl into the compile — without it the
// program does not even load `lib`, and fails as *"i64 has no method raw"* — so the
// warning was advice that breaks the build, the same failure `UFCSModules` exists to
// prevent one rung over.
func TestUnusedImports_ATraitReachedByDispatchIsUsed(t *testing.T) {
	got := unusedImports(t, map[string]string{
		"lib.lyra": traitLib,
		"main.lyra": `module main
import lib.{ Tag }
let main = () -> void => println((7).raw())
`,
	})
	if len(got) != 0 {
		t.Errorf("got %v; want no unused-import warning — deleting that import breaks the build", got)
	}
}

// An **operator** is a dispatch too, and names its trait even less: `a + b` writes neither
// `Plus` nor `lib`.
func TestUnusedImports_ATraitReachedByAnOperatorIsUsed(t *testing.T) {
	got := unusedImports(t, map[string]string{
		"lib.lyra": traitLib,
		"main.lyra": `module main
import lib.{ Vec2, Plus }
let main = () -> void => {
  let v = Vec2 { x: 1, y: 2 } + Vec2 { x: 3, y: 4 }
  println(v.x)
}
`,
	})
	if len(got) != 0 {
		t.Errorf("got %v; want none", got)
	}
}

// **Sparing the dispatched trait must not spare the rest of the import**, which is why
// this half is keyed by the trait's name and the UFCS half by its module. Keying both by
// module is the obvious implementation and silently trades one wrong warning for several
// missing ones — the worse direction, since a warning nobody sees never gets acted on.
func TestUnusedImports_OtherMembersOfTheSameImportStillWarn(t *testing.T) {
	got := unusedImports(t, map[string]string{
		"lib.lyra": traitLib,
		"main.lyra": `module main
import lib.{ Tag, Never, helper }
let main = () -> void => println((7).raw())
`,
	})
	joined := strings.Join(got, "; ")
	if len(got) != 2 || !strings.Contains(joined, `"Never"`) || !strings.Contains(joined, `"helper"`) {
		t.Errorf("got %v; want exactly Never and helper reported", got)
	}
}

// The name recorded is the trait's **declared** one, not the name this import chose for
// it — a trait knows what it is called and not what it was renamed to — so an alias is
// spared exactly as the plain spelling is.
func TestUnusedImports_AnAliasedTraitIsSparedToo(t *testing.T) {
	got := unusedImports(t, map[string]string{
		"lib.lyra": traitLib,
		"main.lyra": `module main
import lib.{ Tag as T }
let main = () -> void => println((7).raw())
`,
	})
	if len(got) != 0 {
		t.Errorf("got %v; want none — `Tag as T` is the same use", got)
	}
}
