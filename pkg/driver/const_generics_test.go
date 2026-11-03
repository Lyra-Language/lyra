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

// What a `const` parameter on a type, as a value, or in arithmetic refuses. Driver-level, as
// these come from three passes.
func TestConstGenerics_Refused(t *testing.T) {
	const ring = "struct Ring<t, const N: i64> { items: [N]t }\n"
	const main = "\nlet main = () -> void => {}\n"
	for _, c := range []struct{ name, src, want string }{
		{"a type where a size goes", ring + "let r: Ring<i64, i64> = Ring { items: #[1] }" + main,
			"N is a `const` parameter and takes a size, not the type i64"},
		{"a size where a type goes", "struct Box<t> { v: t }\nlet b: Box<3> = Box { v: 1 }" + main,
			"t is a type parameter and takes a type, not the size 3"},
		{"the wrong size", ring + "let r: Ring<i64, 2> = Ring { items: #[1, 2, 3] }" + main,
			"cannot assign Ring<i64, 3> to Ring<i64, 2>"},
		{"two sizes of one type", ring + "let a: Ring<i64, 2> = Ring { items: #[1, 2] }\nlet b: Ring<i64, 3> = a" + main,
			"cannot assign Ring<i64, 2> to Ring<i64, 3>"},
		{"a size in a function no `const` declares", ring + "let f = (r: ref Ring<i64, M>) -> i64 => 1" + main,
			"array size \"M\" is not a `const` parameter"},
		{"a value outside its function", "let f = (x: i64) -> i64 => Q" + main, "undefined identifier \"Q\""},
		{"a name multiplying a name", "let f<const N: i64, const M: i64> = (a: ref [N * M]i64) -> i64 => 1" + main,
			"one name can't multiply another"},
		{"a `const` parameter no signature mentions", "let f<const N: i64> = () -> i64 => N" + main,
			"so no call can solve it"},
		{"assigning to one", "let f<const B: i64> = (xs: ref [B]i64) -> i64 => {\n  B = 3\n  B\n}" + main,
			"cannot reassign a `const` parameter"},
		{"a size no N gives", "let f<const N: i64> = (a: ref [2 * N]i64) -> i64 => N\nlet main = () -> void => { print(\"${f(#[1, 2, 3])}\") }\n",
			"is an array of 3, and no N makes 2*N that"},
		{"a size that comes out negative", "let f<const N: i64> = (a: ref [N]i64) -> [N - 1]i64 => #[0; N - 1]\nlet main = () -> void => {\n  let e = #[0; 0]\n  print(\"${f(e).len()}\")\n}\n",
			"comes to a negative number"},
		{"a size that doesn't fit its type", "let f<const B: u8> = (xs: ref [B]i64) -> u8 => B\nlet main = () -> void => {\n  let z = #[0; 300]\n  print(\"${f(z)}\")\n}\n",
			"is 300 here, which doesn't fit its type u8"},
		{"a return of another size", "let f<const N: i64> = (a: ref [N]i64) -> [N + 1]i64 => #[0; N]" + main,
			"return type mismatch: expected [N+1]i64"},
		{"a binding of another size", "let f<const N: i64> = (a: ref [N]i64) -> i64 => {\n  let b: [N + 1]i64 = a\n  1\n}" + main,
			"cannot assign [N]i64 to [N+1]i64"},
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
