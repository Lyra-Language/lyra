package ast

import "github.com/Lyra-Language/lyra/pkg/types"

type GenericParam struct {
	Name        string
	Constraints []string
	// Location is the span of the parameter within its declaration's `<…>` list,
	// so a diagnostic about the *list* can point at the offending entry rather
	// than at the whole declaration. `print:"-"`: auxiliary source position, kept
	// out of golden output like NameLocation elsewhere.
	Location Location `print:"-"`
}

func (g *GenericParam) GetName() string {
	return g.Name
}

// BindGenericParams pairs a declaration's generic parameters with a nominal type's
// arguments **positionally**, which is how a *type's* parameters bind — unlike a generic
// function's, which are solved by name from the argument types.
//
// A parameter past the end of args is left unbound and an argument past the end of params
// is ignored, so a caller that has already checked arity gets exactly what it expects and
// one that has not cannot panic on the mismatch.
//
// Nine sites built this map by hand — in the typechecker, the ownership pass and the
// backend — and they disagreed about that guard: five tested `i < len(args)` inline, four
// indexed straight into args and were safe only because of a `len(…) != len(…)` check
// several lines earlier. That is the shape rule 8 warns about arriving as a panic in the
// one pass whose earlier check someone later moved.
func BindGenericParams(params []GenericParam, args []types.Type) map[string]types.Type {
	subst := make(map[string]types.Type, len(params))
	for i, gp := range params {
		if i < len(args) {
			subst[gp.Name] = args[i]
		}
	}
	return subst
}
