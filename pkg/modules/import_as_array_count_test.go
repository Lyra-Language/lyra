package modules_test

import (
	"testing"

	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
)

// **An import used only as a fixed array's count is used**, and saying otherwise is worse
// than the local twin: acting on `lyra-W004` here deletes the import and the program stops
// compiling.
//
// The name is erased from the AST by the typechecker, which folds a `#[v; N]` count to a
// literal so the backend needs no `const` resolver. `ast.ConstRefNames` recovers it, as it
// already did for a range-pattern bound — the same fold, the same debt, and the same
// diagnostic before it was paid.
//
// Multi-file on purpose: this is the one shape the single-file unused-*variable* test cannot
// reach, since it takes an import to reach W004 at all.
func TestModules_AnImportUsedOnlyAsAnArrayCountIsUsed(t *testing.T) {
	root := buildTree(t, map[string]string{
		"app.lyra": `import util.sizes.{ WIDTH }
let main = () -> u8 => {
  let row: [4]i64 = #[0; WIDTH]
  u8(row.len())
}`,
		"util/sizes.lyra": "module util.sizes\npub const WIDTH = 4",
	})
	res := analyze(t, root)
	if errs := res.Errors(); len(errs) != 0 {
		t.Fatalf("expected a clean program; got %v", errs)
	}
	for _, d := range res.Diagnostics {
		if d.Code == diag.CodeUnusedImport {
			t.Errorf("WIDTH is the array's count, so the import is used; got %v", d)
		}
	}
}

// An import that really is unused still warns, so the recovery has not blanketed the check.
func TestModules_AGenuinelyUnusedImportStillWarns(t *testing.T) {
	root := buildTree(t, map[string]string{
		"app.lyra": `import util.sizes.{ WIDTH }
let main = () -> u8 => 0`,
		"util/sizes.lyra": "module util.sizes\npub const WIDTH = 4",
	})
	res := analyze(t, root)
	if !warnsWith(res, diag.CodeUnusedImport, "WIDTH") {
		t.Errorf("an unused import should still warn; got %v", res.Diagnostics)
	}
}
