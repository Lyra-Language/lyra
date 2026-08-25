package typetable

import (
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// Every field of every snapshot-able table must be named in its Clone literal.
//
// Same shape and same reason as pkg/ast/symbols' clone guard: a field left out is silently
// *shared* with the table it was cloned from, so one keystroke's results leak into the next.
// The typechecker's tables are the larger surface — nine maps on MethodTable alone — and
// they are keyed by AST pointer, which means a leak shows up as a wrong dispatch or a wrong
// recorded type rather than as a count being off.
func TestClone_MentionsEveryTableField(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "clone.go", nil, 0)
	if err != nil {
		t.Fatalf("parsing clone.go: %v", err)
	}
	assigned := map[string]map[string]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		lit, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		id, ok := lit.Type.(*ast.Ident)
		if !ok {
			return true
		}
		for _, el := range lit.Elts {
			if kv, ok := el.(*ast.KeyValueExpr); ok {
				if k, ok := kv.Key.(*ast.Ident); ok {
					if assigned[id.Name] == nil {
						assigned[id.Name] = map[string]bool{}
					}
					assigned[id.Name][k.Name] = true
				}
			}
		}
		return true
	})

	for _, tbl := range []struct {
		name string
		ty   reflect.Type
	}{
		{"TypeTable", reflect.TypeOf(TypeTable{})},
		{"MethodTable", reflect.TypeOf(MethodTable{})},
		{"InstantiationTable", reflect.TypeOf(InstantiationTable{})},
		{"ConstraintTable", reflect.TypeOf(ConstraintTable{})},
	} {
		got := assigned[tbl.name]
		if len(got) == 0 {
			t.Errorf("no &%s{...} literal found in clone.go", tbl.name)
			continue
		}
		var missing []string
		for i := 0; i < tbl.ty.NumField(); i++ {
			if name := tbl.ty.Field(i).Name; !got[name] {
				missing = append(missing, name)
			}
		}
		sort.Strings(missing)
		if len(missing) > 0 {
			t.Errorf("%s.Clone does not mention: %s — a field left out is shared with the "+
				"table it was cloned from, so one keystroke's results reach the next",
				tbl.name, strings.Join(missing, ", "))
		}
	}
}
