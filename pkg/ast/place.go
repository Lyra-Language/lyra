package ast

// RootIdentifier walks a *place* expression — a binding, a field, an element, or a path of
// those (`p.x`, `xs[i]`, `grid[i].y`) — back to the identifier it is rooted at, and returns
// nil for anything that is not rooted at one.
//
// It follows only the **object** spine. An index expression's index is a separate sub-read:
// in `grid[i].y` the place is rooted at `grid`, and `i` is a value read somewhere else in
// the same expression.
//
// Two passes ask this, about two different things, and they want the same answer. The
// purity checker asks it of an assignment target — mutating `grid[i].y` is mutating `grid`,
// so whether that escapes depends on where `grid` was declared. The ownership pass asks it
// of `&x`'s operand, because taking an address pins the *binding* against last-use
// optimization, and `&xs[0]` pins `xs`.
//
// A tuple index is a step too, since `p.0 = v` became an assignment target (09/13); the
// steps are PlaceObject's, so every walker that follows a place agrees on what one is.
func RootIdentifier(expr Expression) *IdentifierExpr {
	for {
		if id, ok := expr.(*IdentifierExpr); ok {
			return id
		}
		object, ok := PlaceObject(expr)
		if !ok {
			return nil
		}
		expr = object
	}
}

// PlaceObject is one step along a place path: the object a field `p.x`, an element `xs[i]`
// or a tuple position `p.0` is read from, and false for anything else — including a deref,
// which ends a path at storage a pointer names rather than stepping into a binding.
//
// **The one definition of a path step.** Every pass that walks an assignment target back to
// its root — writability, the address-taken and captured-write checks, purity, ownership,
// the backend's owning-root test — asks here, so adding a place kind is one case rather than
// a hunt through eight switches, which is how a tuple index was missing from all of them.
func PlaceObject(expr Expression) (Expression, bool) {
	switch e := expr.(type) {
	case *MemberExpr:
		return e.Object, true
	case *IndexExpr:
		return e.Object, true
	case *TupleIndexExpr:
		return e.Object, true
	}
	return nil, false
}

// RootIdentifierName is RootIdentifier's name, or "" when the expression is not rooted at
// an identifier.
func RootIdentifierName(expr Expression) string {
	if id := RootIdentifier(expr); id != nil {
		return id.Name
	}
	return ""
}
