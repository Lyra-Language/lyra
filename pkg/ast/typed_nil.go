package ast

import "reflect"

// TrueNil maps a *typed nil* to a nil interface.
//
// A collector that reports an unrecoverable error and returns a nil `*ast.BlockExpr` hands
// its caller an `ast.Expression` that is **not** nil: an interface holding a type and a nil
// pointer. It slips past `expr == nil` at every later pass, and the first method call on it
// — `GetLocation`, usually, in the typechecker — segfaults. That is hazard 3, and it cost a
// compiler crash on `'\q'` (08/30).
//
// The three dispatchers that build interface values out of concrete collector results —
// `CollectExpression`, `CollectStatement`, `CollectPattern` — pass everything through this,
// so the conversion happens in three places instead of at each of the thirty-odd collectors
// that can return nil. **That is the point of putting it here rather than fixing the
// callees**: a collector added next year gets it without knowing it exists, where a rule
// obeyed at each return site is a rule that can be forgotten exactly once.
//
// It does not make a nil *safe* — a true nil still has to be handled, and hazard 3's advice
// to return a placeholder node instead is still the better answer wherever there is a
// sensible placeholder to return. What this guarantees is that a nil which does escape is
// the kind of nil the code already checks for, rather than the kind that crashes.
//
// Reflection is the only way to ask this question of an interface value, and it costs
// nothing measurable: one `reflect.ValueOf` and an `IsNil` per AST node, against a parse and
// three analysis passes over that same node. Over a 1,600-line file, thirty checks per
// build, three rounds: 75.1/75.2/75.7 ms without it and 75.2/75.0/75.6 with — inside the
// noise, and faster in two rounds of three.
func TrueNil[T any](v T) T {
	if rv := reflect.ValueOf(v); rv.Kind() == reflect.Ptr && rv.IsNil() {
		var zero T
		return zero
	}
	return v
}
