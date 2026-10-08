package collector_test

import (
	"strings"
	"testing"

	"github.com/Lyra-Language/lyra/pkg/ast"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// `impl Person { … }` leaves no trace in the AST: each member is the top-level declaration
// it stands for, with the block's type written into `self` and a mode-only annotation
// (`self: mut`) keeping its mode. A member's own `<…>` list gains the target's variables,
// after its own.
func TestInherentImpl_DesugarsToTopLevelMethods(t *testing.T) {
	program, _, _, _ := parseAndCollect(t, `
struct Person { name: string }
struct Box<t> { value: t }

impl Person {
  /// Greets.
  pub let say_hi = (self) => println(self.name)
  let rename = (self: mut, name: string) => { self.name = name }
}

impl Box<t> {
  let map<u> = (self, f: (t) -> u) -> Box<u> => Box { value: f(self.value) }
}
`)
	decls := map[string]*ast.VarDeclStmt{}
	for _, s := range program.Statements {
		if vd, ok := s.(*ast.VarDeclStmt); ok {
			decls[vd.Name] = vd
		}
	}
	selfOf := func(name string) ast.Parameter {
		t.Helper()
		vd := decls[name]
		if vd == nil {
			t.Fatalf("%s was not hoisted to the top level", name)
		}
		return vd.Value.(*ast.LambdaExpr).Parameters[0]
	}

	if got := selfOf("say_hi").Type; got != (types.UnresolvedType{Name: "Person"}) {
		t.Errorf("say_hi's self = %#v; want Person", got)
	}
	if !decls["say_hi"].IsPublic {
		t.Error("`pub` on a member was dropped")
	}
	if d := decls["say_hi"].Doc; d == nil || !strings.Contains(d.Text, "Greets.") {
		t.Errorf("the member's `///` did not attach: %#v", d)
	}
	rename := selfOf("rename")
	if rename.Type != (types.UnresolvedType{Name: "Person"}) || rename.TypeModifier != types.TypeModifier("mut") {
		t.Errorf("rename's self = %#v %q; want mut Person", rename.Type, rename.TypeModifier)
	}
	if pt, ok := selfOf("map").Type.(types.ParameterizedType); !ok || pt.Name != "Box" {
		t.Errorf("map's self = %#v; want Box<t>", selfOf("map").Type)
	}
	var names []string
	for _, g := range decls["map"].GenericParams {
		names = append(names, g.Name)
	}
	if got := strings.Join(names, ","); got != "u,t" {
		t.Errorf("map's generic list = %s; want u,t (its own, then the target's)", got)
	}
}

// Whatever cannot be desugared into a method of the block's type is lyra-E089, each with a
// message naming what to do instead.
func TestInherentImpl_RefusesWhatIsNotAMethod(t *testing.T) {
	cases := []struct{ name, member, want string }{
		{"var", "var count = (self) => 1", "must be a plain `let`"},
		{"let mut", "let mut count = (self) => 1", "must be a plain `let`"},
		{"a value", "let limit = 5", "is not a function"},
		{"a constant", "let LIMIT = 5", "`LIMIT` in `impl Person` is not a method"},
		{"a destructuring", "let (a, b) = (1, 2)", "is not a method"},
		{"no self", "let make = (name: string) => Person { name: name }", "takes no `self`"},
		{"no parameters", "let make = () => 1", "takes no `self`"},
		{"a typed self", "let f = (self: Person) => 1", "takes its type from the block"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			errs := parseAndCollectErrors(t, "struct Person { name: string }\nimpl Person {\n  "+tc.member+"\n}\n")
			for _, e := range errs {
				if d, ok := e.(diag.Diagnostic); ok && d.Code == diag.CodeInherentMember && strings.Contains(d.Message, tc.want) {
					return
				}
			}
			t.Errorf("expected lyra-E089 containing %q, got: %v", tc.want, errs)
		})
	}
}

// A block below the top level would make its members locals, and a local is never a
// method.
func TestInherentImpl_MustBeAtTheTopLevel(t *testing.T) {
	errs := parseAndCollectErrors(t, `
struct Person { name: string }
let main = () => {
  impl Person { let f = (self) => 1 }
}
`)
	for _, e := range errs {
		if d, ok := e.(diag.Diagnostic); ok && d.Code == diag.CodeInherentMember && strings.Contains(d.Message, "top level") {
			return
		}
	}
	t.Errorf("expected lyra-E089 for a nested impl block, got: %v", errs)
}

// The type is taken by the member alone: a `let` inside a member's body is a local, not a
// second member, and needs no `self`.
func TestInherentImpl_ALocalInAMemberIsNotAMember(t *testing.T) {
	_, _, _, errs := parseAndCollect(t, `
struct Person { name: string }
impl Person {
  let shout = (self) => {
    let loud = (s: string) => s ++ "!"
    loud(self.name)
  }
}
`)
	for _, e := range errs {
		if d, ok := e.(diag.Diagnostic); ok && d.Code == diag.CodeInherentMember {
			t.Errorf("a local inside a member was treated as a member: %v", d)
		}
	}
}

// `impl t where t: Ord { … }` bounds the target's variables in every member, as if each had
// written the `where` itself. A member with a list gets the bound on its entry; a member
// with none is given the list its signature implies, in order of appearance — the list is
// what puts a bound in scope in the body, and a partial one would be refused as
// authoritative.
func TestInherentImpl_WhereBoundsEveryMember(t *testing.T) {
	program, _, _, _ := parseAndCollect(t, `
struct Pair<k, v> { key: k, value: v }

impl Pair<k, v> where k: Show, v: Show + Eq {
  let describe = (self) -> string => "${self.key}"
  let swap_value<u> = (self, u: u) -> Pair<k, u> => Pair { key: self.key, value: u }
}
`)
	lists := map[string]string{}
	for _, s := range program.Statements {
		vd, ok := s.(*ast.VarDeclStmt)
		if !ok {
			continue
		}
		var parts []string
		for _, g := range vd.GenericParams {
			parts = append(parts, g.Name+":"+strings.Join(g.Constraints, "+"))
		}
		lists[vd.Name] = strings.Join(parts, ",")
	}
	if got := lists["describe"]; got != "k:Show,v:Show+Eq" {
		t.Errorf("describe's list = %q; want k:Show,v:Show+Eq", got)
	}
	if got := lists["swap_value"]; got != "u:,k:Show,v:Show+Eq" {
		t.Errorf("swap_value's list = %q; want u:,k:Show,v:Show+Eq", got)
	}
}

// A bound on a variable the target does not mention would bound nothing.
func TestInherentImpl_WhereMustNameATargetVariable(t *testing.T) {
	errs := parseAndCollectErrors(t, "struct Box<t> { value: t }\nimpl Box<t> where u: Show {\n  let f = (self) => 1\n}\n")
	for _, e := range errs {
		if d, ok := e.(diag.Diagnostic); ok && d.Code == diag.CodeUndeclaredTypeVariable && strings.Contains(d.Message, "not a type variable of `impl Box<t>`") {
			return
		}
	}
	t.Errorf("expected lyra-E031 for a bound on u, got: %v", errs)
}
