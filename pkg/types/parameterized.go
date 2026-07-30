package types

import "strings"

// ParameterizedType is a generic type applied to concrete arguments, e.g.
// Maybe<i64> or Box<Node>. Allocation carries a usage-site modifier (from
// e.g. `shared Box<Node>`) on top of the declaration's default; it is NOT
// part of nominal identity — TypesEqual ignores it.
type ParameterizedType struct {
	Name          string
	TypeArguments []Type
	Allocation    AllocationModifier
}

func (ParameterizedType) typeNode() {}
func (p ParameterizedType) GetName() string {
	return p.Name
}

// String renders the applied form, `Box<i64>` — the type arguments are the whole
// point of the type, and without them a mismatch between two instantiations of one
// generic reads as the nonsense "cannot assign Box to Box".
func (p ParameterizedType) String() string {
	if len(p.TypeArguments) == 0 {
		return p.GetName()
	}
	args := make([]string, len(p.TypeArguments))
	for i, a := range p.TypeArguments {
		if a == nil {
			args[i] = "?"
			continue
		}
		args[i] = a.String()
	}
	return p.GetName() + "<" + strings.Join(args, ", ") + ">"
}
