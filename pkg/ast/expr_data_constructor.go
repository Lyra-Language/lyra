package ast

type DataConstructorExpr struct {
	ExprBase
	Constructor string
	Value       Expression
	// Namespace is the namespace import the name was written through (`shapes` in
	// `shapes.Dot`), empty for a bare name. Set when the driver erases the qualifier
	// (eraseQualifiedNames), so a re-analysis of the same node can record it again.
	Namespace string
}

func (d *DataConstructorExpr) GetName() string { return d.Constructor }
