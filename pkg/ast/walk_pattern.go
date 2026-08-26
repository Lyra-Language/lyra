package ast

// Walking patterns.
//
// Patterns are the third supertype — `Statement`, `Expression`, `Pattern` — and until
// 08/22 nothing walked them: `WalkStmt`/`WalkExpr` take an `onStmt` and an `onExpr` and
// never descend into a pattern, so ten passes each hand-rolled their own traversal and
// every *position*-based editor feature was blind to them. Go-to-definition on the
// `Keyboard` of `Keyboard(Up) => …` did nothing, and worse than nothing: `findExprAtPos`
// handed back whatever *expression* happened to span the cursor — a nearby tuple literal —
// so the wrong node was resolved rather than none.
//
// This is the canonical walk for the pattern half. It is deliberately separate from
// WalkStmt/WalkExpr rather than a fourth callback on them, because those two are used
// everywhere and a signature change is a worse trade than a second entry point; a caller
// that wants both walks the outer one and calls PatternsOf at each node.
//
// **Ten passes still hand-roll their own** (the collector, captures, ownership, use-before-
// declaration, exhaustiveness, the backend's match lowering, three in the typechecker).
// Converting them is the rule-8 fix — stop having more than one of it — and is not this
// change; what is here is the one walk a new consumer should use.

// WalkPattern visits p and, if onPattern returns true, each of its sub-patterns.
//
// A nil pattern is a no-op, which every holder relies on: a `case` with no payload, a
// parameter with no destructuring, an arm whose sub-pattern was dropped by a parse error.
func WalkPattern(p Pattern, onPattern func(Pattern) bool) {
	if p == nil || onPattern == nil {
		return
	}
	if !onPattern(p) {
		return
	}
	WalkPatternChildren(p, onPattern)
}

// WalkPatternChildren visits p's sub-patterns without visiting p itself.
//
// **Every kind that can hold another must have a case here.** A miss is silent and its
// symptom is remote — a cursor inside the missed sub-pattern resolves to its parent, or to
// nothing — which is hazard 8 in the pattern family. The kinds with no sub-patterns are
// listed explicitly rather than left to a default, so adding one to the language fails to
// compile here rather than being quietly skipped.
func WalkPatternChildren(p Pattern, onPattern func(Pattern) bool) {
	switch pat := p.(type) {
	case *TuplePattern:
		for _, el := range pat.Elements {
			WalkPattern(el, onPattern)
		}
	case *ArrayPattern:
		for _, el := range pat.Elements {
			WalkPattern(el, onPattern)
		}
	case *StructPattern:
		for i := range pat.Fields {
			WalkPattern(&pat.Fields[i], onPattern)
		}
	case *StructPatternField:
		WalkPattern(pat.Pattern, onPattern)
	case *DataPattern:
		WalkPattern(pat.Pattern, onPattern)
	case *BindingPattern:
		WalkPattern(pat.Pattern, onPattern)
	case *IdentifierPattern, *LiteralPattern, *RestPattern, *RangePattern,
		*WildcardPattern, *RegexPattern:
		// Leaves: nothing inside to visit.
	}
}

// PatternsOf returns the patterns a statement or expression holds directly.
//
// The other half of the walk: patterns hang off ordinary nodes, so reaching them means
// walking statements and expressions as usual and asking each one. Four node kinds hold
// them — a destructuring declaration, a match arm's expression, a lambda's parameters and
// a multi-clause lambda's clauses — and that list is the thing to extend when a fifth
// appears.
func PatternsOf(node AstNode) []Pattern {
	switch n := node.(type) {
	case *DestructuringDeclStmt:
		return []Pattern{n.Pattern}
	case *MatchExpr:
		var out []Pattern
		for _, arm := range n.MatchArms {
			out = append(out, arm.Pattern)
		}
		return out
	case *LambdaExpr:
		var out []Pattern
		for i := range n.Parameters {
			out = append(out, n.Parameters[i].Pattern)
		}
		for i := range n.LambdaClauses {
			out = append(out, n.LambdaClauses[i].Patterns...)
		}
		return out
	}
	return nil
}
