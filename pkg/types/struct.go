package types

import "strings"

type NamedStructType struct {
	Name          string // uppercase letter optionally followed by any number of letters or numbers
	Fields        []StructField
	GenericParams []GenericType
	Allocation    AllocationModifier
}

func (NamedStructType) typeNode() {}

func (s NamedStructType) GetName() string {
	return s.Name
}

func (s NamedStructType) String() string {
	return s.GetName()
}

type StructField struct {
	Name string
	Type Type
	// Frozen marks a field declared with the `readonly` marker: it may never be
	// mutated after construction, even through a mutable (`var`/`let mut`)
	// instance. Unmarked fields are mutable and follow the holding binding.
	Frozen       bool
	DefaultValue any
}

type AnonymousStructType struct {
	Fields []StructField
}

func (AnonymousStructType) typeNode()       {}
func (AnonymousStructType) GetName() string { return "" }

// String renders the fields, because an anonymous struct **is** its fields: rendering
// every one of them as the bare word `struct` made a mismatch read as
// "cannot assign struct to struct", which names the answer as the problem and is
// indistinguishable from the self-rejection bug that message used to be (08/08). Field
// order is as declared, since that is what the reader wrote.
func (a AnonymousStructType) String() string {
	if len(a.Fields) == 0 {
		return "{}"
	}
	parts := make([]string, len(a.Fields))
	for i, f := range a.Fields {
		parts[i] = f.Name + ": " + typeString(f.Type)
	}
	return "{ " + strings.Join(parts, ", ") + " }"
}

// typeString renders a field's type, guarding the nil a partly-inferred literal can
// carry — `<unknown>` beats a panic inside a diagnostic.
func typeString(t Type) string {
	if t == nil {
		return "<unknown>"
	}
	return t.String()
}
