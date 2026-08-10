package types

type DataType struct {
	Name         string // uppercase letter optionally followed by any number of letters or numbers
	Constructors []DataTypeConstructor
	Allocation   AllocationModifier
}

func (DataType) typeNode() {}
func (d DataType) GetName() string {
	return d.Name
}

func (d DataType) String() string {
	return d.GetName()
}

type DataTypeConstructor struct {
	Name   string
	Params []Type

	// Packed records that Params[0] is the *packed positional list* the collector
	// builds for a parenthesized payload — `Rect(i64, i64)` and `Circle(i64)` alike —
	// rather than a payload that simply happens to be a tuple.
	//
	// The two are indistinguishable by the time substitution has run, and that is the
	// bug this exists to fix: `Some t` instantiated at `t = (i64, i64)` has
	// `Params = [TupleType{i64, i64}]`, byte for byte what a declared `Rect(i64, i64)`
	// has. FieldTypes unwrapped both, so the backend read `Some` of a
	// `Maybe<(i64, i64)>` as taking two arguments while the typechecker read it as
	// taking one — and no spelling satisfied both, which made that type unconstructible.
	//
	// Only the *declaration* knows which it is, so only the collector can record it.
	Packed bool
}

// FieldTypes returns the constructor's payload field types, flattened. The
// collector wraps a positional constructor's fields in a single anonymous tuple
// (`Rect(i64, i64)` → Params = [TupleType{i64, i64}]; even one field:
// `Circle(i64)` → [TupleType{i64}]), so a lone anonymous-tuple param is unwrapped
// to its elements — matching the flat positional arguments a construction supplies
// (`Rect(3, 4)`). A nullary constructor returns an empty slice; a single
// non-tuple param (an inline record's anonymous struct, a struct reference) is
// returned as-is.
//
// **Only a *packed* param is unwrapped** (see Packed): a payload that is a tuple
// because a type argument was substituted to one is a single field, not a positional
// list, and unwrapping it is what made `Maybe<(i64, i64)>` unconstructible.
func (c DataTypeConstructor) FieldTypes() []Type {
	if c.Packed && len(c.Params) == 1 {
		if tt, ok := c.Params[0].(TupleType); ok && IsAnonymousTupleName(tt.Name) {
			return tt.Elements
		}
	}
	return c.Params
}
