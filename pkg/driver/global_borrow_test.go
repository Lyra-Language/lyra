package driver_test

import (
	"testing"

	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
)

func globalBorrowCount(t *testing.T, files map[string]string) int {
	t.Helper()
	got := 0
	for _, d := range analyzeModules(t, files).Errors() {
		if d.Code == diag.CodeGlobalBorrow {
			got++
		} else {
			t.Errorf("unexpected error: %s", d.Message)
		}
	}
	return got
}

// Another module's mutable global, imported by name, is reachable from every function as this
// module's own is (lyra-E091).
func TestGlobalBorrow_AnImportedGlobal(t *testing.T) {
	got := globalBorrowCount(t, map[string]string{
		"lib.lyra": `module lib
pub var queue = [0, 0]
pub let add = (x: mut i64) -> void => { x += 1 }
`,
		"main.lyra": `module main
import lib.{ queue, add }
let main = () -> void => {
  add(queue[0])
}
`,
	})
	if got != 1 {
		t.Errorf("got %d lyra-E091, want 1", got)
	}
}

// The name means what it means in the module asking: this module's `let` is never written,
// whatever another module's private `var` of that name does.
func TestGlobalBorrow_TheAskingModulesName(t *testing.T) {
	got := globalBorrowCount(t, map[string]string{
		"lib.lyra": `module lib
var g = [0, 0]
pub let touch = () -> void => { g = [1] }
`,
		"main.lyra": `module main
import lib.{ touch }
let g = [[0, 0]]
let look = (xs: ref []i64) -> i64 => 1
let main = () -> void => {
  touch()
  let n = look(g[0])
}
`,
	})
	if got != 0 {
		t.Errorf("got %d lyra-E091, want 0", got)
	}
}
