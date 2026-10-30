package driver_test

import (
	"reflect"
	"testing"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/driver"
)

// ast.CopyBindingFree copies an expression that binds nothing into nodes of its own, and
// refuses — returning the original — anything that binds a name: a lambda, a block, a match,
// a comprehension. Parsed through the driver so the nodes are the collector's own.
func TestCopyBindingFree(t *testing.T) {
	res := driver.Analyze([]byte(`let a = [Some((1, "x")), None]
let b = Wrap { n: -2, items: [3, 4] }
let c = (x: i64) => x
let d = { 5 }
let e = match a { _ => 1 }
let f = [x in [1, 2] | x]
struct Wrap { n: i64, items: []i64 }
`))
	values := map[string]ast.Expression{}
	for _, node := range res.Program.Statements {
		if decl, ok := node.(*ast.VarDeclStmt); ok {
			values[decl.Name] = decl.Value
		}
	}
	for _, name := range []string{"a", "b"} {
		orig := values[name]
		copied, ok := ast.CopyBindingFree(orig)
		if !ok {
			t.Fatalf("%s: refused a binding-free expression", name)
		}
		if !reflect.DeepEqual(orig, copied) {
			t.Errorf("%s: the copy differs from the original", name)
		}
		originals := map[ast.Expression]bool{}
		ast.WalkExpr(orig, nil, func(e ast.Expression) bool { originals[e] = true; return true })
		ast.WalkExpr(copied, nil, func(e ast.Expression) bool {
			if originals[e] {
				t.Errorf("%s: the copy shares the node %T with the original", name, e)
			}
			return true
		})
	}
	for _, name := range []string{"c", "d", "e", "f"} {
		if got, ok := ast.CopyBindingFree(values[name]); ok || got != values[name] {
			t.Errorf("%s: copied an expression that binds names", name)
		}
	}
}
