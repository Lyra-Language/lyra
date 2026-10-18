package types

import (
	"fmt"
	"strings"
)

// Source renders a type as a program writes it — `[]u8`, `[3]i64`, `(u8, Maybe<i16>)`,
// `bool` — for any text a person reads: a diagnostic, a hover, a documentation page.
//
// It is not String(), and cannot be. String() names a type for the compiler: it is
// mangled into specialization symbols (`typetable.TypeSymbol`) and keys caches and glue,
// so its spelling — `DynamicArray<u8>`, `StaticArray<i64, 3>`, `AnonymousTuple(u8, i16)`,
// `boolean` — is an identity, and changing it would rename symbols. Those words are not
// Lyra, though, and a message that says `cannot assign DynamicArray<i64> to
// DynamicArray<u8>` makes the reader translate before they can fix anything. Until 10/17
// every diagnostic spoke String(); only `lyrac doc` spoke source, through a private copy
// of this walk.
//
// An untyped literal keeps its phrase here ("cannot assign integer literal to string"),
// since that is what the value is at that point, and only the `shared` flavor is written.
// The other two forms differ in exactly those two choices.
func Source(t Type) string { return sourceOf(t, sourceStyle{}) }

// SourceSettled is a signature's spelling (`lyrac doc`): an untyped literal as the type it
// settles to — `i64`, `f64` — which is what a caller of an unannotated binding sees, and
// whichever flavor the declaration carries, as docgen always wrote it.
func SourceSettled(t Type) string {
	return sourceOf(t, sourceStyle{settleLiterals: true, everyFlavor: true})
}

// SourceAnnotation is the annotation a person would write for a value of this type: a
// literal at its default, since `let x: integer literal` is not a type, and only `shared`
// among flavors. The editor's inferred-type hint and its "insert the annotation" action
// use it, the second putting it into the program.
func SourceAnnotation(t Type) string {
	return sourceOf(t, sourceStyle{settleLiterals: true})
}

// SourceParameter is one parameter of a function type, as Source writes it inside one:
// its mode, a `shared` flavor, and the type — `mut Self`, `own []u8`.
func SourceParameter(p ParameterType) string { return parameterSource(p, sourceStyle{}) }

// sourceStyle is the two choices the forms above make differently.
type sourceStyle struct {
	// settleLiterals writes an untyped literal type as its default (`i64`) rather than its
	// phrase (`integer literal`).
	settleLiterals bool
	// everyFlavor writes `stack` as well as `shared`. `stack` is the default the checker
	// stamps on what was written bare, so outside a signature it names a word nobody wrote
	// (`weak stack Node`).
	everyFlavor bool
}

func sourceOf(t Type, st sourceStyle) string {
	switch v := t.(type) {
	case nil:
		return "?"
	case PrimitiveType:
		return primitiveSource(v.Name, st)
	case DynamicArrayType:
		return allocationSource(v.Allocation, st) + "[]" + sourceOf(v.ElementType, st)
	case StaticArrayType:
		size := fmt.Sprintf("%d", v.Size)
		if v.SizeVar != "" {
			size = v.SizeVar
		}
		return allocationSource(v.Allocation, st) + "[" + size + "]" + sourceOf(v.ElementType, st)
	case TupleType:
		// An anonymous tuple is a bare parenthesized list; a named one is its name applied
		// to that list, as its constructor is written.
		if IsAnonymousTupleName(v.Name) {
			return "(" + sourceList(v.Elements, st) + ")"
		}
		return v.Name + "(" + sourceList(v.Elements, st) + ")"
	case ParameterizedType:
		if len(v.TypeArguments) == 0 {
			return v.Name
		}
		return v.Name + "<" + sourceList(v.TypeArguments, st) + ">"
	case SelfType:
		if len(v.Args) == 0 {
			return "Self"
		}
		return "Self<" + sourceList(v.Args, st) + ">"
	case WeakType:
		return "weak " + sourceOf(v.Inner, st)
	case RawPointerType:
		if v.IsMut {
			return "^mut " + sourceOf(v.Pointee, st)
		}
		return "^" + sourceOf(v.Pointee, st)
	case NamedStructType:
		return allocationSource(v.Allocation, st) + v.Name
	case AnonymousStructType:
		// `{ x: i64 }`, as the grammar writes it (anonymous_struct_type). docgen's copy of
		// this walk wrote `struct { … }`, which does not parse; nothing round-tripped one.
		if len(v.Fields) == 0 {
			return "{}"
		}
		fields := make([]string, len(v.Fields))
		for i, f := range v.Fields {
			fields[i] = f.Name + ": " + sourceOf(f.Type, st)
		}
		return "{ " + strings.Join(fields, ", ") + " }"
	case *LambdaType:
		if v == nil {
			return "?"
		}
		params := make([]string, len(v.Parameters))
		for i, p := range v.Parameters {
			params[i] = parameterSource(p, st)
		}
		if v.IsVariadic {
			params = append(params, "...")
		}
		return v.EffectPrefix() + "(" + strings.Join(params, ", ") + ") -> " + sourceOf(v.ReturnType.Type, st)
	case *ConstrainedType:
		if v == nil {
			return "?"
		}
		return v.Name
	}
	// Every other kind — a named type, a type variable, `void`, `never`, a union, a
	// fixed-point type — already writes itself as source does.
	return t.String()
}

// parameterSource writes one parameter of a function type. A parameter's mode and flavor
// are part of the function type (`(mut Self, own i64) -> void`), and a mismatch is often
// about one of them — docgen's copy of this walk dropped both.
func parameterSource(p ParameterType, st sourceStyle) string {
	param := ""
	if p.Borrow != TypeModifier("") {
		param = string(p.Borrow) + " "
	}
	param += allocationSource(p.Modifier, st) + sourceOf(p.Type, st)
	// **A named parameter renders its name**, which only an `extern` has: E067 requires one
	// there and refuses it in a plain function type.
	if p.Name != "" {
		param = p.Name + ": " + param
	}
	return param
}

// primitiveSource is the keyword for a primitive, which is not always what the type calls
// itself: `boolean` is spelled `bool`, and an untyped literal type is a phrase for a
// sentence ("integer literal"), or — settled — the default it takes.
func primitiveSource(n PrimitiveTypeName, st sourceStyle) string {
	switch n {
	case Boolean:
		return "bool"
	case UntypedInt, UntypedSignedInt:
		if st.settleLiterals {
			return "i64"
		}
	case UntypedFloat:
		if st.settleLiterals {
			return "f64"
		}
	}
	return PrimitiveType{Name: n}.String()
}

// allocationSource writes a flavor before a type that carries one: `shared` always, and
// `stack` only where the style asks for every flavor (see sourceStyle.everyFlavor).
func allocationSource(a AllocationModifier, st sourceStyle) string {
	if a == AllocationModifier("") || a == Unspecified {
		return ""
	}
	if !st.everyFlavor && a != Shared {
		return ""
	}
	return string(a) + " "
}

func sourceList(ts []Type, st sourceStyle) string {
	parts := make([]string, len(ts))
	for i, t := range ts {
		parts[i] = sourceOf(t, st)
	}
	return strings.Join(parts, ", ")
}
