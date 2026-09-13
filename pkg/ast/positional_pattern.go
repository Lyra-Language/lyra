package ast

// Positions is how a positional pattern list — a tuple pattern's elements, or a
// constructor's payload — lines up against the n positions of the value it matches.
//
// **The one answer to "which element matches which position"**, because a `...rest` makes
// it more than an index. `(a, ...r, z)` against a 4-tuple matches `a` at 0 and `z` at 3, and
// `r` covers 1 and 2. Every pass that pairs elements with positions — the typechecker's
// binding walk, the arm and literal checks, exhaustiveness, ownership and the backend's
// test, bind and own — asks here, since pairing by index put `z` at 2 and a copy of this
// rule in each of them is how that happened (09/13).
type Positions struct {
	// Columns has one pattern per position. A position the rest covers holds nil, which
	// every consumer already reads as a wildcard.
	Columns []Pattern
	// Rest is the `...name` element, or nil when there is none. It covers positions
	// [RestFrom, RestTo), which may be empty.
	Rest             *RestPattern
	RestFrom, RestTo int
}

// MatchPositions lines elems up against n positions, reporting false when they cannot fit:
// without a rest the counts must be equal, and with one there must be no more elements
// around it than positions. A second rest is refused by the collector (lyra-E076) and is
// reported as not fitting here.
func MatchPositions(elems []Pattern, n int) (Positions, bool) {
	restAt := -1
	for i, el := range elems {
		if _, ok := el.(*RestPattern); ok {
			if restAt >= 0 {
				return Positions{}, false
			}
			restAt = i
		}
	}
	if restAt < 0 {
		if len(elems) != n {
			return Positions{}, false
		}
		return Positions{Columns: elems}, true
	}
	fixed := len(elems) - 1
	if fixed > n {
		return Positions{}, false
	}
	cols := make([]Pattern, n)
	copy(cols, elems[:restAt])
	after := elems[restAt+1:]
	copy(cols[n-len(after):], after)
	return Positions{
		Columns:  cols,
		Rest:     elems[restAt].(*RestPattern),
		RestFrom: restAt,
		RestTo:   n - len(after),
	}, true
}

// HasRest reports whether a positional pattern list contains a `...rest`.
func HasRest(elems []Pattern) bool {
	for _, el := range elems {
		if _, ok := el.(*RestPattern); ok {
			return true
		}
	}
	return false
}

// MinPositions is the fewest positions elems can match: every element but a rest.
func MinPositions(elems []Pattern) int {
	if HasRest(elems) {
		return len(elems) - 1
	}
	return len(elems)
}
