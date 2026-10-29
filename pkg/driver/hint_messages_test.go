package driver_test

import (
	"strings"
	"testing"

	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/driver"
)

func errorMessages(res *driver.Result) []string {
	var out []string
	for _, d := range res.Diagnostics {
		if d.Severity == diag.SeverityError {
			out = append(out, d.Message)
		}
	}
	return out
}

// A lambda parameter nothing types — no annotation, no context — is refused once, at the
// parameter, saying so; it was "undefined symbol" at every use of a name plainly declared,
// and then a return type that could not be inferred (10/29).
func TestHint_UninferredLambdaParameter(t *testing.T) {
	errs := errorMessages(driver.Analyze([]byte(`struct Box { w: i64 }
let main = () -> void => {
  let at = (p, q) => p.w + p.w + q
  let width: (Box) -> i64 = (b) => b.w
  println("${width(Box { w: 1 })}")
}
`)))
	want := []string{
		`cannot infer the type of parameter "p": nothing here says what it is — annotate it`,
		`cannot infer the type of parameter "q"`,
	}
	if len(errs) != len(want) {
		t.Fatalf("want %d errors; got %q", len(want), errs)
	}
	for i, w := range want {
		if !strings.Contains(errs[i], w) {
			t.Errorf("error %d: want %q; got %q", i, w, errs[i])
		}
	}
}

// A method-style call to another module's private function says it is private — not
// "import it", which no import could make true (10/07) — for a hand-written `self`
// function and an `impl` block's member alike. An exported one still says to import it.
func TestHint_PrivateMethodIsNotAnImportAway(t *testing.T) {
	const shapes = `module shapes
pub struct Sq { n: i64 }
pub let square = pure (n: i64) -> Sq => Sq { n: n }
let hidden = pure (self: Sq) -> i64 => self.n
impl Sq {
  let secret = pure (self) -> i64 => self.n
}
`
	for _, c := range []struct{ name, main, want string }{
		{"a namespace import", "import shapes\nlet main = () -> void => println(\"${shapes.square(2).hidden()}\")\n",
			`hidden takes a ` + "`self`" + ` receiver but is private to module "shapes"`},
		{"an impl member", "import shapes.{ square }\nlet main = () -> void => println(\"${square(2).secret()}\")\n",
			`secret takes a ` + "`self`" + ` receiver but is private to module "shapes"`},
	} {
		t.Run(c.name, func(t *testing.T) {
			errs := errorMessages(analyzeTree(t, map[string]string{"shapes.lyra": shapes, "main.lyra": c.main}))
			if len(errs) != 1 || !strings.Contains(errs[0], c.want) || strings.Contains(errs[0], "import it") {
				t.Errorf("want one error containing %q; got %q", c.want, errs)
			}
		})
	}
}

// An uppercase type parameter is a parse error by design (tree-sitter-lyra's
// generic_type.js); the error says the rule and gives the list lowercased, in every
// declaration form, rather than "unexpected \"<R> where R\"" (10/26).
func TestHint_UppercaseTypeParameter(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{"let id<T> = (a: T) -> T => a", "`<T>` is `<t>`"},
		{"let pair<A, B> = (a: A, b: B) -> A => a", "\"A\", \"B\" would be concrete types, not variables — so `<A, B>` is `<a, b>`"},
		{"let f<t, U> = (a: t, b: U) -> t => a", "`<t, U>` is `<t, u>`"},
		{"pub let pure g<Item: Show> = (a: Item) -> Item => a", "`<Item: Show>` is `<item: Show>`"},
		{"let f<R> where R: Show = (r: R) -> string => r.show()", "`<R>` is `<r>`"},
		{"struct Box<T> { v: T }", "`<T>` is `<t>`"},
		{"data Opt<T> = No | Yes(T)", "`<T>` is `<t>`"},
		{"trait Conv<T> { conv: (Self) -> T }", "`<T>` is `<t>`"},
	} {
		t.Run(c.src, func(t *testing.T) {
			errs := errorMessages(driver.Analyze([]byte(c.src + "\nlet main = () -> void => {}\n")))
			if len(errs) == 0 || !strings.Contains(errs[0], "a type parameter is written in lowercase") ||
				!strings.Contains(errs[0], c.want) {
				t.Errorf("want the first error to contain %q; got %q", c.want, errs)
			}
		})
	}
	// A `const` parameter is uppercase by rule, and an ordinary syntax error is left alone.
	for _, src := range []string{
		"let s<const N: i64> = pure (xs: ref [N]i64) -> i64 => xs.len()",
		"let h = (a: i64) -> i64 => a +",
	} {
		for _, e := range errorMessages(driver.Analyze([]byte(src + "\nlet main = () -> void => {}\n"))) {
			if strings.Contains(e, "lowercase") {
				t.Errorf("%s: unexpected %q", src, e)
			}
		}
	}
}
