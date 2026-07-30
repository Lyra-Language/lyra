package typetable

import (
	"fmt"
	"sort"
	"strings"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// Instantiation is one use of a generic function with its type variables solved:
// the function, and the binding of each variable to a concrete type.
//
// It is what the backend monomorphizes from. The typechecker checks a generic
// declaration once, generically, and each call site against the *substituted*
// signature; the substitution has to survive that check because the backend needs
// to know both which specializations to emit and which one each call resolves to.
type Instantiation struct {
	Name  string                // the generic function's declared name
	Func  *ast.LambdaExpr       // its (shared) body
	Subst map[string]types.Type // type variable → concrete type
}

// Key is a stable identity for the *specialization*, so two call sites that solve
// to the same bindings share one emitted function. Sorted by variable name, since
// a map's iteration order is not stable and the key must be.
func (i Instantiation) Key() string {
	names := make([]string, 0, len(i.Subst))
	for n := range i.Subst {
		names = append(names, n)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, n := range names {
		parts = append(parts, fmt.Sprintf("%s=%s", n, i.Subst[n]))
	}
	return i.Name + "<" + strings.Join(parts, ",") + ">"
}

// Symbol is the emitted function's name: the generic name plus its bindings, in
// the same stable order. Readable in the IR (`identity$t.i64`) so a specialization
// can be told from its siblings at a glance.
func (i Instantiation) Symbol() string {
	names := make([]string, 0, len(i.Subst))
	for n := range i.Subst {
		names = append(names, n)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names)+1)
	parts = append(parts, i.Name)
	for _, n := range names {
		parts = append(parts, mangleTypeName(i.Subst[n].String()))
	}
	return strings.Join(parts, "$")
}

// mangleTypeName reduces a type's rendering to symbol-safe characters.
func mangleTypeName(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		default:
			return '_'
		}
	}, s)
}

// InstantiationTable maps each generic call site to the specialization it resolves
// to. Populated by the typechecker, consumed by the backend.
type InstantiationTable struct {
	byCall map[*ast.FunctionCallExpr]Instantiation
}

func NewInstantiationTable() *InstantiationTable {
	return &InstantiationTable{byCall: map[*ast.FunctionCallExpr]Instantiation{}}
}

func (t *InstantiationTable) Set(call *ast.FunctionCallExpr, inst Instantiation) {
	if t == nil {
		return
	}
	t.byCall[call] = inst
}

// Get returns the specialization a call resolves to. Nil-receiver-safe, so a
// consumer without this pass sees "not generic" rather than crashing.
func (t *InstantiationTable) Get(call *ast.FunctionCallExpr) (Instantiation, bool) {
	if t == nil {
		return Instantiation{}, false
	}
	i, ok := t.byCall[call]
	return i, ok
}

// All returns every distinct specialization the program uses, keyed by Key() so
// each is emitted once however many call sites reach it. The order is stable
// (sorted by key) so the emitted module is deterministic.
func (t *InstantiationTable) All() []Instantiation {
	if t == nil {
		return nil
	}
	unique := map[string]Instantiation{}
	for _, inst := range t.byCall {
		unique[inst.Key()] = inst
	}
	keys := make([]string, 0, len(unique))
	for k := range unique {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]Instantiation, 0, len(keys))
	for _, k := range keys {
		out = append(out, unique[k])
	}
	return out
}
