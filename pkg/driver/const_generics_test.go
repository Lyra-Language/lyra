package driver_test

import (
	"strings"
	"testing"

	"github.com/Lyra-Language/lyra/pkg/driver"
)

// An array size written as a name must be a declared `const` parameter (lyra-E031):
// unlike a lowercase type variable, a size never makes a function generic by being
// written, and an undeclared one is a size nothing settles. A `const` parameter's type is
// an integer. Driver-level, because the generic-parameter check is a checker pass.
func TestConstGenerics_DeclarationRules(t *testing.T) {
	for _, c := range []struct{ name, src, want string }{
		{"a size no const declares", "let f = pure (xs: ref [M]i64) -> i64 => 0\nlet main = () -> void => {}\n",
			"array size \"M\" is not a `const` parameter of \"f\""},
		{"a local sized by an undeclared name", "let f<const N: i64> = pure (xs: ref [N]i64) -> i64 => {\n  let wrong: [M]i64 = xs\n  0\n}\nlet main = () -> void => {}\n",
			"array size \"M\" is not a `const` parameter"},
		{"a non-integer const", "let h<const N: f64> = pure (xs: [N]i64) -> i64 => 0\nlet main = () -> void => {}\n",
			"`const N` must be an integer type"},
	} {
		t.Run(c.name, func(t *testing.T) {
			res := driver.Analyze([]byte(c.src))
			found := false
			for _, d := range res.Errors() {
				if strings.Contains(d.Message, c.want) {
					found = true
				}
			}
			if !found {
				t.Errorf("expected an error containing %q; got %v", c.want, res.Diagnostics)
			}
		})
	}
}
