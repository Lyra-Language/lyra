package driver_test

import (
	"strings"
	"testing"

	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
)

// importShadowWarnings returns the lyra-W016 messages a program produces.
func importShadowWarnings(t *testing.T, files map[string]string) []string {
	t.Helper()
	var out []string
	for _, d := range analyzeTree(t, files).Diagnostics {
		if d.Code == diag.CodeImportShadowed {
			out = append(out, d.Message)
		}
	}
	return out
}

// A constructor this module declares takes the name of one it imports, and every use here
// is the module's own — so the import does nothing. W016 says so of a struct, a trait or a
// function taking an imported name; a constructor is not a name the symbol table maps, so it
// was silent (10/13, Vega's map editor), until 10/19.
func TestImportShadow_AConstructorTakesAnImportedName(t *testing.T) {
	got := importShadowWarnings(t, map[string]string{
		"owner.lyra": "module owner\npub data Tool = OfMap | Other\n",
		"main.lyra": `import owner.{ OfMap }
data Mine = OfMap | Elsewhere
let main = () -> u8 => {
  let m = OfMap
  0
}
`,
	})
	if len(got) != 1 || !strings.Contains(got[0], "OfMap takes the name imported from module \"owner\"") {
		t.Fatalf("want one W016 about OfMap, got %v", got)
	}
}

// **An overload is not a shadow**: an imported `area` on another receiver stays reachable
// beside this module's own, and the import is what reaches it — so nothing is said. (A
// first cut of the constructor case refused this, as an error over every name.)
func TestImportShadow_AReceiverOverloadIsNotOne(t *testing.T) {
	got := importShadowWarnings(t, map[string]string{
		"shapes.lyra": "module shapes\npub struct Sq { s: i64 }\npub let area = (self: Sq) -> i64 => self.s * self.s\n",
		"main.lyra": `import shapes.{ Sq, area }
struct Circle { r: i64 }
let area = (self: Circle) -> i64 => 3 * self.r * self.r
let main = () -> u8 => u8(area(Sq { s: 2 }) + area(Circle { r: 1 }))
`,
	})
	if len(got) != 0 {
		t.Fatalf("want no W016, got %v", got)
	}
}

// unusedImportWarnings returns the lyra-W004 messages a program produces.
func unusedImportWarnings(t *testing.T, files map[string]string) []string {
	t.Helper()
	var out []string
	for _, d := range analyzeTree(t, files).Diagnostics {
		if d.Code == diag.CodeUnusedImport {
			out = append(out, d.Message)
		}
	}
	return out
}

// A constructor imported under an alias and used by it is a used import — its uses are
// rewritten to the original name before W004 counts references, and counted for the import
// — while one never used is still reported, under the name the import gave it (10/19).
func TestUnusedImport_AnAliasedConstructor(t *testing.T) {
	owner := "module owner\npub data Tool = OfMap | Other\npub let describe = (t: Tool) -> i64 => 1\n"
	used := unusedImportWarnings(t, map[string]string{
		"owner.lyra": owner,
		"main.lyra":  "import owner.{ OfMap as Theirs, describe }\nlet main = () -> u8 => u8(describe(Theirs))\n",
	})
	if len(used) != 0 {
		t.Errorf("a used alias: want no W004, got %v", used)
	}
	unused := unusedImportWarnings(t, map[string]string{
		"owner.lyra": owner,
		"main.lyra":  "import owner.{ OfMap as Theirs, describe }\nlet main = () -> u8 => u8(describe(Other))\n",
	})
	if len(unused) != 1 || !strings.Contains(unused[0], `"Theirs" is never used`) {
		t.Errorf("an unused alias: want one W004 naming Theirs, got %v", unused)
	}
}
