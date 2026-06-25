package types

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
func (p ParameterizedType) String() string {
	return p.GetName()
}
