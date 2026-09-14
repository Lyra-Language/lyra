package checker_test

import (
	"strings"
	"testing"

	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
)

// **A type's body must use only the variables its list declares** (lyra-E031), for every
// kind of type declaration. A type is instantiated only by writing its list's arguments, so a
// variable missing from it can never be given a type — until 09/13 each of these compiled and
// failed at a use, or (the alias) in the backend.
func TestGenericParams_TypeBodyVariablesMustBeDeclared(t *testing.T) {
	for _, c := range []struct{ name, src, variable string }{
		{"struct", `struct Box<t> { v: u, w: t }`, "u"},
		{"struct with no list", `struct Box { v: t }`, "t"},
		{"data payload", `data Opt<t> = Has(u) | Gone`, "u"},
		{"data inline record", `data Tree<t> = Node { val: u, left: shared Tree<t> } | Leaf`, "u"},
		{"named tuple", `tuple Pair<t>(t, u)`, "u"},
		{"newtype", `newtype Wrap<t> = []u`, "u"},
		{"nested in a generic argument", `struct Holder<t> { items: []Maybe<u> }`, "u"},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := errorsOnly(checkGenericParams(t, c.src))
			if len(got) != 1 || got[0].Code != diag.CodeUndeclaredTypeVariable || !strings.Contains(got[0].Message, `"`+c.variable+`"`) {
				t.Errorf("want one lyra-E031 naming %q, got %v", c.variable, got)
			}
		})
	}
}

// An alias cannot take a list, so its message says so rather than suggesting one.
func TestGenericParams_AnAliasCannotMentionAVariable(t *testing.T) {
	got := errorsOnly(checkGenericParams(t, `type Lst = []t`))
	if len(got) != 1 || !strings.Contains(got[0].Message, "an alias takes no parameters") {
		t.Errorf("want the alias form of lyra-E031, got %v", got)
	}
}

// **An unused type parameter is a phantom type, not a mistake**, so a type declaration draws
// nothing for one — `Id<User>` and `Id<Order>` are different types with one layout.
func TestGenericParams_PhantomTypeParametersAreFine(t *testing.T) {
	assertNoGenericParamDiags(t, `
struct Id<t> { n: i64 }
data Tagged<t> = Named(string) | Anon
newtype Meters<unit> = f64
struct Box<t> { v: t }
data Tree<t> = Node(t, shared Tree<t>) | Leaf
`)
}

// **A trait parameter no method mentions warns** (lyra-W013): an impl binds it and nothing
// can use what it was bound to. A method's *own* variable is not checked, since there is no
// method-level list to declare it in — `map` below is generic in `a` and `b`.
func TestGenericParams_TraitParameters(t *testing.T) {
	got := checkGenericParams(t, `
trait Conv<t, u: Show> { conv: (Self) -> t }
trait Functor { map: (Self<a>, (a) -> b) -> Self<b> }
trait Get<e> { get: (Self) -> e, map_get: (Self, (e) -> x) -> x }
`)
	if len(errorsOnly(got)) != 0 {
		t.Errorf("a trait method's own variables are not errors, got %v", errorsOnly(got))
	}
	warns := warningsOnly(got)
	if len(warns) != 1 || warns[0].Code != diag.CodeUnusedTypeParameter ||
		!strings.Contains(warns[0].Message, `"u" of trait "Conv"`) || !strings.Contains(warns[0].Message, "constrains nothing") {
		t.Errorf("want one lyra-W013 for Conv's unused, bounded u, got %v", warns)
	}
}
