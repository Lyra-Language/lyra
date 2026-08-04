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

// TypeSymbol is the emitted name for one instantiation of a generic *type*:
// `Box<i64>` → `Box$i64`, `Pair<i64, string>` → `Pair$i64$string`.
//
// Positional, unlike Symbol: a type's parameters are ordered by their declaration,
// whereas a generic function's substitution is solved by variable name and so has
// to be sorted into a stable order. Both share mangleTypeName so one convention
// covers every specialized symbol in the module.
func TypeSymbol(name string, args []types.Type) string {
	parts := make([]string, 0, len(args)+1)
	parts = append(parts, name)
	for _, a := range args {
		if a == nil {
			parts = append(parts, "_")
			continue
		}
		parts = append(parts, mangleTypeName(a.String()))
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
//
// **Two kinds of entry live here, and telling them apart is the point.** A call in
// ordinary code solves its callee's variables to concrete types — `identity<t=i64>`.
// A call *inside a generic body* solves them to whatever the enclosing body has in
// scope, which is that body's own type variables: `unwrap<t>`'s call to `expect`
// records `expect<t=t>`. The second kind is not a specialization at all — it is a
// template, one per enclosing specialization, and lowering it directly is what
// produced "type variable t has no concrete type here".
//
// So the table holds both, and `Concrete` is what separates them. The templates must
// stay: they are how a call inside a generic body is resolved, by composing the
// enclosing specialization's bindings into them (driver.closeInstantiations).
type InstantiationTable struct {
	byCall map[*ast.FunctionCallExpr]Instantiation
	// discovered holds specializations reached by composition rather than by a call
	// site of their own, keyed by Key(). `unwrap<t=i64>` calling `expect` needs
	// `expect<t=i64>` emitted, and no call site in the program mentions it.
	discovered map[string]Instantiation
}

func NewInstantiationTable() *InstantiationTable {
	return &InstantiationTable{
		byCall:     map[*ast.FunctionCallExpr]Instantiation{},
		discovered: map[string]Instantiation{},
	}
}

// Add records a specialization that no call site names directly.
func (t *InstantiationTable) Add(inst Instantiation) {
	if t == nil {
		return
	}
	if t.discovered == nil {
		t.discovered = map[string]Instantiation{}
	}
	t.discovered[inst.Key()] = inst
}

// IsConcrete reports whether every binding is a real type — nothing still mentioning
// a type variable. Only a concrete instantiation can be emitted; see the note above.
func (i Instantiation) IsConcrete() bool {
	for _, bound := range i.Subst {
		if bound == nil || types.MentionsTypeVar(bound) {
			return false
		}
	}
	return true
}

// Substituted returns this instantiation with subst applied to each of its bindings —
// the composition that turns a template into a specialization.
//
// The substitution is applied to the binding *values*, never to the keys: the keys are
// the callee's own variable names and the values are written in the caller's scope, so
// `expect<t=t>` composed under `{t: i64}` is `expect<t=i64>`. That the two `t`s are
// spelled the same is a coincidence of the source and must not be treated as a
// relationship.
func (i Instantiation) Substituted(subst map[string]types.Type, apply func(types.Type, map[string]types.Type) types.Type) Instantiation {
	if len(subst) == 0 {
		return i
	}
	out := Instantiation{Name: i.Name, Func: i.Func, Subst: make(map[string]types.Type, len(i.Subst))}
	for name, bound := range i.Subst {
		out.Subst[name] = apply(bound, subst)
	}
	return out
}

// Concrete returns every emittable specialization — those with no type variable left
// in any binding — in a stable order.
func (t *InstantiationTable) Concrete() []Instantiation {
	var out []Instantiation
	for _, inst := range t.All() {
		if inst.IsConcrete() {
			out = append(out, inst)
		}
	}
	return out
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
	for key, inst := range t.discovered {
		unique[key] = inst
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
