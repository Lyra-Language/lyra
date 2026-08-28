package types

// ConversionTargetName resolves a call's callee spelling to the conversion target it
// names, or false for an ordinary function name. This is the one answer to "is
// `name(x)` a type conversion?" — the typechecker, the purity pass, the ownership
// pass and the backend all ask it, and before it existed each kept its own list;
// the purity copy had already drifted (no `rune`, so `rune(n)` was charged as
// impure). One function, so the copies cannot disagree (CLAUDE.md hazard 8).
//
// The mapping is not a bare cast of the spelling: `bool`'s source keyword is not its
// internal type name ("boolean"), and `string`/`bool` are conversion targets at all
// only as the newtype read-out spelling (lyra-E047) — identity-only, which the
// *typechecker* enforces (numericPrimitiveByName / identityConversionTargetByName
// are its split of this same set; a name added here must land in exactly one of
// those two).
// BaseReadoutName is the callee spelling of the universal newtype read-out,
// `base(v)`: the one conversion whose target is computed from its operand — the
// newtype's immediate base — rather than named by the callee. It exists so *every*
// newtype has a read-out spelling and lyra-E047 can refuse the implicit form
// uniformly; a base a conversion can name (`i64(c)`) keeps the named spelling as the
// preferred one. Unlike the names below it is a compiler builtin resolved after scope
// (a user binding shadows it), so the passes that must know whether a call *is* the
// read-out consult the typechecker's marker (TypeTable.IsBaseReadout), never this
// name.
const BaseReadoutName = "base"

func ConversionTargetName(callee string) (PrimitiveTypeName, bool) {
	switch name := PrimitiveTypeName(callee); name {
	case Int8, Int16, Int32, Int64, Int128,
		UInt8, UInt16, UInt32, UInt64, UInt128,
		Float16, Float32, Float64, Rune:
		return name, true
	}
	switch callee {
	case "string":
		return String, true
	case "bool":
		return Boolean, true
	}
	return "", false
}
