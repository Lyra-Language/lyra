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
// A tuple index is deliberately absent: `&p.0` is refused as lyra-E059 (a tuple index names
// no storage), so it cannot appear beneath an address-of, and a tuple index is not an
// assignment target either. If either of those changes, this is one of the switches that
// has to gain the case.
func RootIdentifier(expr Expression) *IdentifierExpr {
	for {
		switch e := expr.(type) {
		case *IdentifierExpr:
			return e
		case *MemberExpr:
			expr = e.Object
		case *IndexExpr:
			expr = e.Object
		default:
			return nil
		}
	}
}

// RootIdentifierName is RootIdentifier's name, or "" when the expression is not rooted at
// an identifier.
func RootIdentifierName(expr Expression) string {
	if id := RootIdentifier(expr); id != nil {
		return id.Name
	}
	return ""
}
