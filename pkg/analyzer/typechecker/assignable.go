package typechecker

import (
	"github.com/Lyra-Language/lyra/pkg/ast"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// checkImplicitNewtypeConversion enforces the rule that gives a newtype its meaning at
// a boundary: **an untyped literal may become a newtype implicitly; a value that
// already has a type may not** (lyra-E046, 08/12). `let c: Cents = 150` is fine;
// `take(plain_i64)` against `(c: Cents)` is not, and `Cents(x)` is how it is said.
//
// The distinction is not convenience, it is provenance. A literal has no unit yet — 150
// is not "150 of something else" — so adopting it costs nothing and reads as what the
// author meant. A *typed* value came from somewhere, and that somewhere is exactly
// where a unit mixup lives: before this, `let plain: i64 = 150` followed by
// `take(plain)` compiled silently, so a newtype declared a distinction the compiler then
// declined to enforce at any call boundary. It is Ada's rule for its derived types
// (`M : Meters := 3.0` is legal, `M := F` for a Float F is not, `Meters(F)` is the
// conversion), and Ada is the language that has taken this problem most seriously.
//
// It is checked here — from propagateLiteralType's newtype arm, and the one assignment
// path that does no propagation — rather than inside isAssignable, for two reasons.
// isAssignable sees only *types*, and the rule needs the expression: a string or bool
// literal has the same type as a variable holding one (there is no `untyped_string`),
// so the literal half of the rule is not expressible at the type level. And the newtype
// arm is already the single point every position a newtype context can arrive at flows
// through — the same reason the constraint checks live there.
// `from` is passed rather than read back off the value node, because one caller has
// already overwritten it: checkVarDecl records the *annotation* on its value before
// narrowing (so propagateOperandType's guard re-descends), which would leave this
// reading `Cents` where the source was an `i64` and concluding nothing was being
// converted. That is exactly the case the rule exists for, and it silently passed until
// the from-type became a parameter.
func (tc *TypeChecker) checkImplicitNewtypeConversion(value ast.Expression, from types.Type, ct *types.ConstrainedType) {
	if from == nil {
		return // nothing inferred — an error is already reported, don't invent a second
	}
	switch {
	case types.TypesEqual(from, ct):
		return // already this newtype: nothing is being converted
	case isNewtypeConversionExempt(from):
		return
	case isUntypedLiteralType(from), isSyntacticLiteral(value):
		return // a literal has no provenance to lose
	}
	tc.addErrorCode(value.GetLocation(), SeverityError, diag.CodeImplicitNewtypeConversion,
		"cannot use %s as %s implicitly: %s is a distinct type over %s, so the conversion must be written — `%s(...)`",
		from, ct.Name, ct.Name, ct.Type, ct.Name)
}

// checkImplicitNewtypeReadout is checkImplicitNewtypeConversion's mirror (lyra-E047,
// 08/12): a newtype value used where its **base** is expected must write the
// conversion — `i64(c)`, `string(e)` — where it used to flow implicitly. Reading out
// discards the name the newtype carries, and a silent discard at a call boundary is
// the same unit-mixup shape E046 closed on the way in; Ada requires `Integer(M)` in
// this direction for the same reason it requires `Meters(F)` in the other.
//
// Two deliberate limits. There is no literal half — a newtype value is never a
// literal, so the refusal is unconditional where it applies. And it applies only when
// the base is a type the conversion spelling can *name* — a primitive, `string`,
// `bool`, `rune` — because refusing with no spelling to offer would make the newtype
// write-only. A newtype over an array or a function type keeps its implicit read-out,
// which is the grammar's limit and is documented as such (todo.md).
//
// Like its mirror, it fires only when the flow is otherwise legal: a base that is not
// assignable to the target at all is the ordinary mismatch's to report, and a
// newtype-to-newtype pair is the distinctness rule's.
func (tc *TypeChecker) checkImplicitNewtypeReadout(value ast.Expression, from, to types.Type) {
	if from == nil || to == nil {
		return
	}
	from = tc.resolveTypeIfKnown(from, value.GetLocation())
	ct, ok := from.(*types.ConstrainedType)
	if !ok {
		return
	}
	if _, toCT := to.(*types.ConstrainedType); toCT {
		return
	}
	// Chained newtypes read out under the innermost base's name; resolve as we strip,
	// since a base written as a name is stored unresolved.
	base := types.Type(ct)
	for {
		inner, isCT := base.(*types.ConstrainedType)
		if !isCT {
			break
		}
		base = tc.resolveTypeIfKnown(inner.Type, value.GetLocation())
	}
	spelling, ok := readoutSpelling(base)
	if !ok {
		return
	}
	if !isAssignable(base, to) {
		return
	}
	if tc.readoutReported[value] {
		return
	}
	if tc.readoutReported == nil {
		tc.readoutReported = map[ast.Expression]bool{}
	}
	tc.readoutReported[value] = true
	tc.addErrorCode(value.GetLocation(), SeverityError, diag.CodeImplicitNewtypeReadout,
		"cannot use %s as %s implicitly: reading a newtype out discards the name it carries, so the conversion must be written — `%s(...)`",
		ct.Name, to, spelling)
}

// readoutSpelling names the conversion that reads a newtype over base back out —
// the source keyword, which for bool is not the internal type name ("boolean").
// A base with no spelling (an array, a tuple, a function type) answers false, and
// the read-out rule does not apply to it.
func readoutSpelling(base types.Type) (string, bool) {
	p, ok := base.(types.PrimitiveType)
	if !ok {
		return "", false
	}
	switch {
	case p.Name == types.Boolean:
		return "bool", true
	case isAnyConcreteInt(p.Name) || isAnyConcreteFloat(p.Name) || p.Name == types.Rune || p.Name == types.String:
		return string(p.Name), true
	}
	return "", false
}

// identityConversionTargetByName maps the two conversion targets that are not
// numeric: `string(x)` and `bool(x)`. They exist solely as the read-out spelling for
// a newtype over them (lyra-E047's fix), so inferTypeConversion admits them
// identity-only — there is no stringification and no truthiness, and an operand that
// is not the target (after the newtype strip) is refused.
func identityConversionTargetByName(name string) (types.Type, bool) {
	switch name {
	case "string":
		return types.PrimitiveType{Name: types.String}, true
	case "bool":
		return types.PrimitiveType{Name: types.Boolean}, true
	}
	return nil, false
}

// isNewtypeConversionExempt covers the source types the rule has nothing to say about:
// another newtype (isAssignable already refuses that pair, and reporting here too would
// double up), `never` (a `panic(…)` reaches no assignment), and a bare type variable
// (inside a generic body there is no concrete source type to judge, and the
// instantiation is where the question has an answer).
func isNewtypeConversionExempt(from types.Type) bool {
	switch from.(type) {
	case *types.ConstrainedType, types.NeverType, types.GenericType:
		return true
	}
	return false
}

// isUntypedLiteralType reports whether t is one of the internal literal types — the
// types a numeric literal carries until a context fixes its width. They are precisely
// "a constant with no provenance", which is why they are the numeric half of the rule:
// constant arithmetic (`100 + 50`) stays untyped too, so it is covered without this
// having to walk the expression.
func isUntypedLiteralType(t types.Type) bool {
	p, ok := t.(types.PrimitiveType)
	if !ok {
		return false
	}
	return p.Name == types.UntypedInt || p.Name == types.UntypedSignedInt || p.Name == types.UntypedFloat
}

// isSyntacticLiteral is the other half, for the literals the *type* system cannot
// identify: a string, bool or rune literal has the same type as a variable holding one,
// so only the syntax says it is a constant. An array literal counts when every element
// does, which is what lets `newtype Row = []i64` take `[1, 2, 3]`.
func isSyntacticLiteral(expr ast.Expression) bool {
	switch e := expr.(type) {
	case *ast.StringLiteralExpr, *ast.BooleanLiteralExpr, *ast.CharacterLiteralExpr,
		*ast.IntegerLiteralExpr, *ast.FloatLiteralExpr:
		return true
	case *ast.NegationExpr:
		return isSyntacticLiteral(e.Operand)
	case *ast.ArrayLiteralExpr:
		for _, el := range e.Elements {
			if !isSyntacticLiteral(el) {
				return false
			}
		}
		return true
	}
	return false
}

// isAssignable reports whether a value of type from can be assigned to a slot of type to.
//
// Untyped integer literals (UntypedInt) are assignable to any concrete integer type or
// any concrete float type. Untyped float literals (UntypedFloat) are assignable to any
// concrete float type. All concrete types require exact structural equality.
func isAssignable(from, to types.Type) bool {
	if types.TypesEqual(from, to) {
		return true
	}
	// `never` (the type of a `panic(…)`) is assignable to everything: control does
	// not reach past it, so there is no value that could be the wrong type. This is
	// what puts a diverging expression in value position — `None => panic("…")` as a
	// match arm — without every branch-joining construct learning about `panic`.
	//
	// The converse is deliberately absent: nothing is assignable *to* `never` except
	// `never` itself (handled by TypesEqual above), because a slot of type `never` is
	// one no value can fill.
	if _, ok := from.(types.NeverType); ok {
		return true
	}
	// Two function types that differ *only* in their declared effect bounds are assignable
	// here, in **either** direction: the shape is what this function decides, and whether
	// the value actually satisfies `pure () -> t` is decided by the purity pass, which is
	// the only pass that knows.
	//
	// Enforcing contravariance here instead would reject an ordinary `(x: i64) -> i64 => x * 2`
	// for a `pure` slot, because a lambda literal carries no annotation — its purity is
	// *inferred*, bottom-up, in a later pass. Requiring the word `pure` on every callback a
	// program writes would make the feature cost more than it is worth; the checker's
	// checkDeclaredCallbackBounds compares the *inferred* effect against the declared bound
	// and reports a real reason ("mutates a captured binding") rather than a shape mismatch.
	// TypesEqual still distinguishes them, so trait-signature matching and anything else
	// asking about identity sees two different types.
	if fromLam, ok := from.(*types.LambdaType); ok {
		if toLam, ok := to.(*types.LambdaType); ok {
			return types.TypesEqual(
				&types.LambdaType{Parameters: fromLam.Parameters, ReturnType: fromLam.ReturnType},
				&types.LambdaType{Parameters: toLam.Parameters, ReturnType: toLam.ReturnType},
			)
		}
	}
	// A data-type value is assignable to the same nominal type whether the slot
	// is written as a bare name, with generic arguments (`Maybe<i64>`), or as
	// another reference to the data type. The checker does not instantiate
	// generics, so nominal types unify by head name. This is required because a
	// constructor application like `Some(42)` infers to the bare `Maybe`
	// DataType while annotations are usually written `Maybe<T>`.
	if nominalDataMatch(from, to) {
		return true
	}
	// Two *different* newtypes never interconvert, even over the same base. Each
	// rule below is individually right at the *type* level — a value satisfying the
	// base is assignable *to* a newtype, and a newtype value is assignable to its
	// base — but chaining them made every newtype over a common base mutually
	// assignable: `Meters` → `i64` → `Feet` type-checked silently, which is
	// precisely the mixup a newtype exists to prevent. Rejecting the pair here
	// keeps both single-step rules intact.
	//
	// Both single steps are further gated at the **expression** level (08/12),
	// which this type-level function cannot see: base → newtype is implicit only
	// for a literal (checkImplicitNewtypeConversion, lyra-E046 — a typed value
	// needs the constructor), and newtype → base needs the base-name conversion
	// wherever one exists (checkImplicitNewtypeReadout, lyra-E047 — `i64(c)`).
	// The rules here answer "could this flow at all"; those answer "must the
	// author write it down".
	fromCT, fromIsCT := from.(*types.ConstrainedType)
	toCT, toIsCT := to.(*types.ConstrainedType)
	if fromIsCT && toIsCT && fromCT.Name != toCT.Name {
		return false
	}
	// Constrained types are nominally distinct from their base types at the
	// type-equality level, but a value is always assignable to a constrained
	// type when it satisfies the base type (the constraint check itself is
	// handled separately by checkPatternConstraints etc.).
	if toIsCT {
		return isAssignable(from, toCT.Type)
	}
	// A constrained type value is assignable to its base type (e.g.
	// `let s: string = someEmail` where Email = string @pattern(...)).
	if fromIsCT {
		return isAssignable(fromCT.Type, to)
	}
	// A static array literal is assignable to a dynamic array with a compatible
	// element type. This mirrors how `let xs: []int = [1, 2, 3]` should work:
	// the literal produces StaticArrayType{int,3} which widens to DynamicArrayType{int}.
	if fromSA, ok := from.(types.StaticArrayType); ok {
		// An empty array literal [] (Size==0, ElementType==nil) is assignable to
		// any array type — the element type is vacuously satisfied.
		if fromSA.ElementType == nil {
			switch to := to.(type) {
			case types.DynamicArrayType:
				return true
			case types.StaticArrayType:
				return fromSA.Size == to.Size
			}
		}
		if toDyn, ok := to.(types.DynamicArrayType); ok {
			return isAssignable(fromSA.ElementType, toDyn.ElementType)
		}
		// StaticArrayType → StaticArrayType: sizes must match, elements must be assignable.
		if toSA, ok := to.(types.StaticArrayType); ok {
			return fromSA.Size == toSA.Size && isAssignable(fromSA.ElementType, toSA.ElementType)
		}
	}

	// Two anonymous tuples are structural: assignable when arities match and each
	// element is assignable. This lets a tuple literal's untyped element widen to
	// an annotation's element type — `let a: (i32, i32) = (1, 2)` — the same way a
	// bare untyped literal widens to a scalar annotation. Named tuples are nominal
	// (handled by TypesEqual above), so a name-vs-name or named-vs-anonymous pair
	// is deliberately not coerced here.
	if fromT, ok := from.(types.TupleType); ok {
		if toT, ok := to.(types.TupleType); ok &&
			types.IsAnonymousTupleName(fromT.Name) && types.IsAnonymousTupleName(toT.Name) &&
			len(fromT.Elements) == len(toT.Elements) {
			for i := range fromT.Elements {
				if !isAssignable(fromT.Elements[i], toT.Elements[i]) {
					return false
				}
			}
			return true
		}
	}

	// An **anonymous struct** matches field-by-field by *name*, recursing so an
	// untyped literal field widens exactly as a tuple element does. Named structs are
	// nominal and handled by TypesEqual above; this is the structural one.
	//
	// Its absence made the type unusable rather than merely awkward: an anonymous
	// struct literal's field types come from its own leaves, so `{ x: 1 }` is
	// `{ x: untyped_int }` and `let a: { x: i64 } = { x: 1 }` fell through to
	// TypesEqual — which compares field types *exactly* — and reported **"cannot
	// assign struct to struct"**, a type refused against itself with the message
	// naming the same thing twice. Fixed 08/08. Hazard 8, in a list of aggregate forms
	// with one missing: the anonymous *tuple* arm directly above is the same rule, and
	// it had been there all along.
	if fromS, ok := from.(types.AnonymousStructType); ok {
		if toS, ok := to.(types.AnonymousStructType); ok && len(fromS.Fields) == len(toS.Fields) {
			for _, tf := range toS.Fields {
				found := false
				for _, ff := range fromS.Fields {
					if ff.Name != tf.Name {
						continue
					}
					if !isAssignable(ff.Type, tf.Type) {
						return false
					}
					found = true
					break
				}
				if !found {
					return false
				}
			}
			return true
		}
	}

	fromP, fromIsPrim := from.(types.PrimitiveType)
	toP, toIsPrim := to.(types.PrimitiveType)
	if !fromIsPrim || !toIsPrim {
		return false
	}
	switch fromP.Name {
	case types.UntypedInt:
		return isAnyConcreteInt(toP.Name) || isAnyConcreteFloat(toP.Name)
	case types.UntypedSignedInt:
		return isAnyConcreteSignedInt(toP.Name) || isAnyConcreteFloat(toP.Name)
	case types.UntypedFloat:
		return isAnyConcreteFloat(toP.Name)
	}
	return false
}

// firstAllocationMismatch decides whether owning a value of type from into a
// slot of type to crosses a storage-flavor boundary that must be spelled
// explicitly. It walks from and to in parallel — the top-level flavor first,
// then structurally into array element types and tuple element types — and
// returns the first concrete, differing flavor pair it finds (found=true), or
// found=false when the two are flavor-compatible throughout.
//
// Allocation (stack vs shared) is NOT part of nominal identity — isAssignable
// and TypesEqual ignore it, so the structural check is unchanged. This is a
// separate axis, applied only at *owning* sites (a binding's initializer, a
// reassignment, an interior lvalue write, an `own` argument, an owned return):
// storing a `shared` value where a `stack` value is expected (or vice versa)
// changes representation and so is an explicit operation, never a silent
// coercion.
//
// The rule is deliberately conservative: it flags only a concrete, differing
// pair. An Unspecified flavor on either side means "inherit from context" (an
// unannotated binding, a plain type reference), which is polymorphic and stays
// compatible with either flavor — this keeps all unannotated code, and the
// common "construct a literal, bind it `shared`" pattern, error-free. The
// structural recursion catches an element-level boundary such as a `stack`
// element assigned into a `[N]shared` slot, which the top-level flavors (both
// the array's own, here Unspecified) would miss; returning the offending pair
// lets the diagnostic name the actual flavors that clashed, even several levels
// down.
func firstAllocationMismatch(from, to types.Type) (types.AllocationModifier, types.AllocationModifier, bool) {
	fromA := types.AllocationOf(from)
	toA := types.AllocationOf(to)
	if fromA != types.Unspecified && toA != types.Unspecified && fromA != toA {
		return fromA, toA, true
	}
	switch f := from.(type) {
	case types.StaticArrayType:
		switch t := to.(type) {
		case types.StaticArrayType:
			return elementAllocationMismatch(f.ElementType, t.ElementType)
		case types.DynamicArrayType:
			return elementAllocationMismatch(f.ElementType, t.ElementType)
		}
	case types.DynamicArrayType:
		switch t := to.(type) {
		case types.DynamicArrayType:
			return elementAllocationMismatch(f.ElementType, t.ElementType)
		case types.StaticArrayType:
			return elementAllocationMismatch(f.ElementType, t.ElementType)
		}
	case types.TupleType:
		if t, ok := to.(types.TupleType); ok && len(f.Elements) == len(t.Elements) {
			for i := range f.Elements {
				if fa, ta, ok := firstAllocationMismatch(f.Elements[i], t.Elements[i]); ok {
					return fa, ta, true
				}
			}
		}
	case types.AnonymousStructType:
		// By name, like the assignability rule above — and present for the same reason
		// the tuple arm is: an allocation flavor buried in a field is as much a
		// mismatch as one in an element.
		if t, ok := to.(types.AnonymousStructType); ok {
			for _, tf := range t.Fields {
				for _, ff := range f.Fields {
					if ff.Name != tf.Name {
						continue
					}
					if fa, ta, ok := firstAllocationMismatch(ff.Type, tf.Type); ok {
						return fa, ta, true
					}
					break
				}
			}
		}
	}
	return "", "", false
}

// elementAllocationMismatch recurses into a pair of container element types,
// guarding the nil element that an empty array literal ([]) carries.
func elementAllocationMismatch(from, to types.Type) (types.AllocationModifier, types.AllocationModifier, bool) {
	if from == nil || to == nil {
		return "", "", false
	}
	return firstAllocationMismatch(from, to)
}

// paramOwnsArgument reports whether passing an argument to a parameter with this
// mode transfers ownership, so the argument's allocation flavor must match the
// parameter's declared flavor (allocationCompatible). Borrowed parameters
// (bare / `ref` / `mut`) are allocation-polymorphic — the callee references the
// caller's value in place, whatever its flavor — so only `own`, which adopts the
// value into the callee's own storage, is subject to the flavor check. This is
// FP/Imperative todo #5 Decision (b): "owned params carry a flavor; borrowed
// params are allocation-polymorphic."
func paramOwnsArgument(mod types.TypeModifier) bool {
	return mod == types.Own
}

// isOwnedReturn reports whether a function's return value is owned out to the
// caller — so its allocation flavor must match the declared return type — rather
// than borrowed. A `ref`/`mut` return is a borrow (allocation-polymorphic); a
// bare or `own` return transfers ownership. Mirror of paramOwnsArgument for the
// return position (Decision (b) applies symmetrically to owned returns).
func isOwnedReturn(mod types.TypeModifier) bool {
	return mod != types.Ref && mod != types.Mut
}

// nominalDataMatch reports whether from and to denote the same nominal data
// type by head name, ignoring generic arguments. At least one side must be a
// concrete DataType; the other may be a DataType, a ParameterizedType
// (`Maybe<i64>`), or an UnresolvedType naming the same data type. Used so a
// constructor application (which infers to the bare DataType) is accepted
// against a written instantiation of that type.
func nominalDataMatch(from, to types.Type) bool {
	if name, ok := dataTypeName(from); ok {
		if other, ok := nominalName(to); ok {
			return name == other
		}
	}
	if name, ok := dataTypeName(to); ok {
		if other, ok := nominalName(from); ok {
			return name == other
		}
	}
	return false
}

// dataTypeName returns the name of t when it is a concrete DataType.
func dataTypeName(t types.Type) (string, bool) {
	if dt, ok := t.(types.DataType); ok {
		return dt.Name, true
	}
	return "", false
}

// nominalName returns the head name of any nominal type reference: a DataType,
// a ParameterizedType, or an UnresolvedType.
func nominalName(t types.Type) (string, bool) {
	switch v := t.(type) {
	case types.DataType:
		return v.Name, true
	case types.ParameterizedType:
		return v.Name, true
	case types.UnresolvedType:
		return v.Name, true
	}
	return "", false
}

// areEqualityCompatible reports whether two types can be compared with == or !=.
// The rule is symmetric assignability: a == b is valid whenever a can be
// assigned to b's type OR b can be assigned to a's type. This covers:
//   - Same concrete types:            bool == bool, i32 == i32
//   - Untyped widening to concrete:   i32 == 5, f64 == 1.0
//   - Rejects int/float mixing:       5 == 5.0  (UntypedInt ↛ UntypedFloat)
//   - Rejects cross-kind mismatches:  string == 5, bool == 5
func areEqualityCompatible(a, b types.Type) bool {
	return isAssignable(a, b) || isAssignable(b, a)
}

// numericResultType returns the result type of a binary operation on two numeric types.
// UntypedInt/UntypedFloat widens to the concrete type when mixed with one.
// Returns nil if the operands are from different concrete numeric types
func numericResultType(a, b types.Type) types.Type {
	if types.TypesEqual(a, b) {
		return a
	}
	ap, aIsPrim := a.(types.PrimitiveType)
	bp, bIsPrim := b.(types.PrimitiveType)
	if !aIsPrim || !bIsPrim {
		return nil
	}
	if ap.Name == types.UntypedInt && (isAnyConcreteInt(bp.Name) || isFloatType(bp)) {
		return b
	}
	if bp.Name == types.UntypedInt && (isAnyConcreteInt(ap.Name) || isFloatType(ap)) {
		return a
	}
	// UntypedInt + UntypedSignedInt → UntypedSignedInt (signed takes precedence)
	if ap.Name == types.UntypedInt && bp.Name == types.UntypedSignedInt {
		return b
	}
	if bp.Name == types.UntypedInt && ap.Name == types.UntypedSignedInt {
		return a
	}
	if ap.Name == types.UntypedSignedInt && (isAnyConcreteSignedInt(bp.Name) || isFloatType(bp)) {
		return b
	}
	if bp.Name == types.UntypedSignedInt && (isAnyConcreteSignedInt(ap.Name) || isFloatType(ap)) {
		return a
	}
	if ap.Name == types.UntypedFloat && isFloatType(bp) {
		return b
	}
	if bp.Name == types.UntypedFloat && isFloatType(ap) {
		return a
	}
	return nil
}

// numericPrimitiveByName maps a Lyra type keyword to its PrimitiveType, for the
// numeric types that are valid targets of an explicit type conversion. Returns
// (t, true) if name is a known concrete numeric primitive, (nil, false) otherwise.
func numericPrimitiveByName(name string) (types.Type, bool) {
	switch types.PrimitiveTypeName(name) {
	case types.Int8, types.Int16, types.Int32, types.Int64, types.Int128,
		types.UInt8, types.UInt16, types.UInt32, types.UInt64, types.UInt128,
		types.Float16, types.Float32, types.Float64,
		// `rune` is not numeric (it is excluded from types.IsNumeric, so it has no
		// arithmetic), but it *is* a conversion target: `rune(n)` is the explicit
		// way to build a code point from an integer, the inverse of `i32(c)`.
		// inferTypeConversion restricts which source types it will accept.
		types.Rune:
		return types.PrimitiveType{Name: types.PrimitiveTypeName(name)}, true
	}
	return nil, false
}

func isIntType(t types.Type) bool {
	p, ok := t.(types.PrimitiveType)
	if !ok {
		return false
	}
	return isAnyConcreteInt(p.Name) || p.Name == types.UntypedInt || p.Name == types.UntypedSignedInt
}

func isFloatType(t types.Type) bool {
	p, ok := t.(types.PrimitiveType)
	if !ok {
		return false
	}
	return isAnyConcreteFloat(p.Name) || p.Name == types.UntypedFloat
}

// floatPrecision returns the relative precision rank of a concrete float type
// (higher = more precise). Returns 0 for non-float or untyped float types.
func floatPrecision(t types.Type) int {
	p, ok := t.(types.PrimitiveType)
	if !ok {
		return 0
	}
	switch p.Name {
	case types.Float16:
		return 1
	case types.Float32:
		return 2
	case types.Float64:
		return 3
	}
	return 0
}

func isAnyConcreteInt(n types.PrimitiveTypeName) bool {
	return isAnyConcreteSignedInt(n) || isAnyConcreteUnsignedInt(n)
}

func isAnyConcreteSignedInt(n types.PrimitiveTypeName) bool {
	switch n {
	case types.Int8, types.Int16, types.Int32, types.Int64, types.Int128:
		return true
	}
	return false
}

func isAnyConcreteUnsignedInt(n types.PrimitiveTypeName) bool {
	switch n {
	case types.UInt8, types.UInt16, types.UInt32, types.UInt64, types.UInt128:
		return true
	}
	return false
}

func isAnyConcreteFloat(n types.PrimitiveTypeName) bool {
	switch n {
	case types.Float16, types.Float32, types.Float64:
		return true
	}
	return false
}
