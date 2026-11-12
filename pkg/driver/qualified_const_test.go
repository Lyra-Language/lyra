package driver_test

import (
	"strings"
	"testing"
)

// Another module's `const` through its namespace is a `const` (11/11): `const WIDTH =
// assets.SCREEN_WIDTH` was "a member access is not constant", so a game wrote the qualified
// name at every use. A private one is refused as private, once; a `var` is still no constant.
func TestQualifiedConst(t *testing.T) {
	const lib = `module assets
pub const SCREEN_WIDTH: i64 = 320
const SECRET: i64 = 7
pub var counter: i64 = 1
`
	cases := []struct{ consts, want string }{
		{"const WIDTH: i64 = assets.SCREEN_WIDTH\nconst HALF = assets.SCREEN_WIDTH / 2", ""},
		{"const A = assets.SECRET", `SECRET is private to module "assets"`},
		{"const B = assets.counter", "a member access is not constant"},
	}
	for _, c := range cases {
		res := analyzeModules(t, map[string]string{
			"assets.lyra": lib,
			"main.lyra":   "module main\nimport assets\n" + c.consts + "\nlet main = () -> void => {}\n",
		})
		var all []string
		for _, d := range res.Errors() {
			all = append(all, d.Message)
		}
		switch {
		case c.want == "" && len(all) != 0:
			t.Errorf("%q: want a clean analysis, got %v", c.consts, all)
		case c.want != "" && (len(all) != 1 || !strings.Contains(all[0], c.want)):
			t.Errorf("%q: want one error containing %q, got %v", c.consts, c.want, all)
		}
	}
}
