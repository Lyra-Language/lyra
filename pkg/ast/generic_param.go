package ast

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
