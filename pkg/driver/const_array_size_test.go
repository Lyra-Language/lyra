package driver_test

import (
	"strings"
	"testing"

	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/driver"
)

// A size named by a `const` that is not a number — an arithmetic initializer, a negative
// one — stays lyra-E031, saying why; a name that is no `const` at all says that instead,
// in a function and in a type alike (foldConstArraySizes, checkSizeVars).
func TestConstArraySize_Refusals(t *testing.T) {
	for _, c := range []struct{ name, src, want string }{
		{"an arithmetic const", "const SIZE = 3 * 4\nlet f = pure (xs: [SIZE]u8) -> i64 => 0\nlet main = () -> void => {}\n",
			"array size \"SIZE\" is a `const`, but its value is not a size"},
		{"a negative const", "const NEG = -1\nlet f = pure (xs: [NEG]u8) -> i64 => 0\nlet main = () -> void => {}\n",
			"array size \"NEG\" is a `const`, but its value is not a size"},
		{"a float const", "const HALF: f64 = 2\nlet f = pure (xs: [HALF]u8) -> i64 => 0\nlet main = () -> void => {}\n",
			"array size \"HALF\" is a `const`, but its value is not a size"},
		{"no const at all", "let f = pure (xs: [NOPE]u8) -> i64 => 0\nlet main = () -> void => {}\n",
			"array size \"NOPE\" is not a `const` parameter of \"f\" or a `const`"},
		{"a struct field naming no const", "struct Buf { bytes: [NOPE]u8 }\nlet main = () -> void => {}\n",
			"array size \"NOPE\" is not a `const` — declare one"},
		{"a struct field naming an arithmetic const", "const SIZE = 3 * 4\nstruct Buf { bytes: [SIZE]u8 }\nlet main = () -> void => {}\n",
			"array size \"SIZE\" is a `const`, but its value is not a size"},
	} {
		t.Run(c.name, func(t *testing.T) {
			res := driver.Analyze([]byte(c.src))
			var e031 []string
			for _, d := range res.Errors() {
				if d.Code == diag.CodeUndeclaredTypeVariable {
					e031 = append(e031, d.Message)
				}
			}
			if len(e031) != 1 || !strings.Contains(e031[0], c.want) {
				t.Errorf("want one lyra-E031 containing %q; got %v", c.want, res.Diagnostics)
			}
		})
	}
}

// A module's private `const` sizes nothing in another module, and an exported one only
// where it is imported — the folding resolves a name as the module writing it reaches it.
func TestConstArraySize_OnlyWhatTheModuleReaches(t *testing.T) {
	res := analyzeTree(t, map[string]string{
		"lib.lyra": "module lib\nconst HIDDEN: i64 = 4\npub const SHOWN: i64 = 4\npub let n = pure () -> i64 => HIDDEN + SHOWN\n",
		"main.lyra": "import lib.{ n }\n" +
			"let f = pure (xs: [HIDDEN]u8) -> i64 => n()\n" +
			"let g = pure (xs: [SHOWN]u8) -> i64 => n()\n" +
			"let main = () -> void => {}\n",
	})
	var e031 []string
	for _, d := range res.Errors() {
		if d.Code == diag.CodeUndeclaredTypeVariable {
			e031 = append(e031, d.Message)
		}
	}
	if len(e031) != 2 || !strings.Contains(strings.Join(e031, "\n"), "\"HIDDEN\"") ||
		!strings.Contains(strings.Join(e031, "\n"), "\"SHOWN\"") {
		t.Errorf("want lyra-E031 for HIDDEN and for the unimported SHOWN; got %v", res.Diagnostics)
	}
}
