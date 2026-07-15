package types

import (
	"fmt"
	"strings"
)

type TupleType struct {
	Name     string // uppercase letter optionally followed by any number of letters or numbers
	Elements []Type
	// Allocation is the storage flavor a *use site* gives this tuple value
	// (`let p: shared Point`), applied via WithAllocation during resolveType.
	// Allocation is never declared on the type (there are no declaration-level
	// modifiers) — it's a property of the value's storage, read via AllocationOf.
	Allocation AllocationModifier
}

func (TupleType) typeNode() {}

// IsAnonymousTupleName reports whether name marks an anonymous tuple: "" (the
// Go zero value, e.g. a bare TupleType{} constructed without a Name) or "?"
// (the collector/typechecker's placeholder for a tuple literal or type with no
// leading name, e.g. `(1, 2)`). Anything else is a named tuple's declared name
// (`tuple Point(i32, i32)`), which — unlike an anonymous tuple — is nominal:
// see TypesEqual's TupleType case.
func IsAnonymousTupleName(name string) bool {
	return name == "" || name == "?"
}

func (t TupleType) GetName() string {
	elementNames := make([]string, len(t.Elements))
	for i, element := range t.Elements {
		elementName := "?"
		if element != nil {
			elementName = element.String()
		}
		elementNames[i] = elementName
	}
	name := "AnonymousTuple"
	if !IsAnonymousTupleName(t.Name) {
		name = t.Name
	}
	return fmt.Sprintf("%s(%s)", name, strings.Join(elementNames, ", "))
}

func (t TupleType) String() string {
	return t.GetName()
}
