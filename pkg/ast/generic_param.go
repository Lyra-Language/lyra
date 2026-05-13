package ast

type GenericParam struct {
	Name        string
	Constraints []string
}

func (g *GenericParam) GetName() string {
	return g.Name
}
