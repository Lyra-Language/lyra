package driver_test

import (
	"strings"
	"testing"

	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
)

// aliasCollisions returns the error messages about a constructor alias that cannot be told
// from another constructor.
func aliasCollisions(t *testing.T, files map[string]string) []string {
	t.Helper()
	var out []string
	for _, d := range analyzeTree(t, files).Diagnostics {
		if d.Severity == diag.SeverityError && strings.Contains(d.Message, "cannot be told from") {
			out = append(out, d.Message)
		}
	}
	return out
}

// A constructor imported under an alias is matched by its own name once the alias is
// erased, so it collides with another module's constructor of that name even though the
// program wrote two different names — found 10/21 importing SDL's `KeyDown` beside
// `ui.backend`'s. With payloads alike the wrong constructor would be built in silence, so
// the import is refused.
func TestConstructorAlias_CollidingWithAnotherImportIsRefused(t *testing.T) {
	got := aliasCollisions(t, map[string]string{
		"sdl.lyra": "module sdl\npub data Event = KeyDown(u32) | Quit\n",
		"ui.lyra":  "module ui\npub data Input = KeyDown(u32) | Closed\n",
		"main.lyra": `import sdl.{ Event, KeyDown as SdlKeyDown }
import ui.{ Input, KeyDown }
let main = () -> u8 => {
  let e: Event = SdlKeyDown(1)
  let i: Input = KeyDown(2)
  0
}
`,
	})
	if len(got) != 1 || !strings.Contains(got[0], `the KeyDown imported from module "ui"`) {
		t.Fatalf("want one refusal naming ui's KeyDown, got %v", got)
	}
}

// The same against the module's own constructor of that name.
func TestConstructorAlias_CollidingWithAnOwnConstructorIsRefused(t *testing.T) {
	got := aliasCollisions(t, map[string]string{
		"sdl.lyra": "module sdl\npub data Event = KeyDown(u32) | Quit\n",
		"main.lyra": `import sdl.{ Event, KeyDown as SdlKeyDown }
data Input = KeyDown(u32) | Closed
let main = () -> u8 => {
  let e: Event = SdlKeyDown(1)
  0
}
`,
	})
	if len(got) != 1 || !strings.Contains(got[0], "this module's own KeyDown") {
		t.Fatalf("want one refusal naming the module's own KeyDown, got %v", got)
	}
}

// An alias with nothing else of its name in reach is what aliases are for, and passes.
func TestConstructorAlias_WithoutACollisionIsAccepted(t *testing.T) {
	got := aliasCollisions(t, map[string]string{
		"sdl.lyra": "module sdl\npub data Event = KeyDown(u32) | Quit\n",
		"main.lyra": `import sdl.{ Event, KeyDown as SdlKeyDown }
let main = () -> u8 => {
  let e: Event = SdlKeyDown(1)
  0
}
`,
	})
	if len(got) != 0 {
		t.Fatalf("want no refusal, got %v", got)
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
