package ast

type GenericParameterConstraint struct {
	Name string
	Constraints []string
}

func (t *GenericParameterConstraint) GetName() string {
	return t.Name
}