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

// PatternBinding is one name a pattern introduces, and the node to attribute it to.
//
// Node is nil where the language binds a name that no node carries on its own: a struct
// pattern's shorthand (`{ x }` binds the field) and a rest pattern's `...xs` each name
// something the pattern spells rather than a sub-pattern. A caller that needs a `Named` —
// the collector, entering these into a scope — uses `At` for those.
type PatternBinding struct {
	Name string
	Node Named
	Loc  Location
}

// At is the binding as a Named, whatever kind of pattern spelled it.
//
// Node is preferred where it exists *and* agrees: `BindingPattern.GetName()` renders
// `name @ pattern`, which is right for a diagnostic and wrong as a scope key, so it is not
// used here. This is why the collector could not simply define the sub-pattern nodes it
// found.
func (b PatternBinding) At() Named {
	if b.Node != nil && b.Node.GetName() == b.Name {
		return b.Node
	}
	return &boundName{AstBase: AstBase{Location: b.Loc}, name: b.Name}
}

// boundName is a Named for a name with no node of its own.
type boundName struct {
	AstBase
	name string
}

func (b *boundName) node()           {}
func (b *boundName) GetName() string { return b.name }

// EachPatternBinding calls fn for every name a pattern introduces.
//
// **The one answer to "what does this pattern bind".** Three passes each had their own
// version — the captures analysis, use-before-declaration, and the checker's helpers — and
// they had already drifted: the third handled neither `name @ pattern` nor a struct
// pattern's shorthand, so a name bound either way was invisible to it. That is rule 8 with
// the copies agreeing about the easy kinds and disagreeing about the two that are easy to
// forget.
//
// Every kind that binds is listed, including the two that bind without a sub-pattern:
//
//   - `{ x }` — a struct pattern's shorthand field, which binds the field's own name;
//   - `...rest` — a rest pattern, when it is named.
func EachPatternBinding(p Pattern, fn func(PatternBinding)) {
	WalkPattern(p, func(sub Pattern) bool {
		switch b := sub.(type) {
		case *IdentifierPattern:
			fn(PatternBinding{Name: b.Name, Node: b, Loc: b.GetLocation()})
		case *BindingPattern:
			// The name *and* whatever the inner pattern binds, so the walk continues.
			fn(PatternBinding{Name: b.Name, Loc: b.GetLocation()})
		case *RestPattern:
			if b.Identifier != "" {
				fn(PatternBinding{Name: b.Identifier, Loc: b.GetLocation()})
			}
		case *StructPatternField:
			if b.Pattern == nil {
				fn(PatternBinding{Name: b.Name, Node: b, Loc: b.GetLocation()})
			}
		}
		return true
	})
}

// PatternBoundNames is EachPatternBinding's names, in source order.
func PatternBoundNames(p Pattern) []string {
	var out []string
	EachPatternBinding(p, func(b PatternBinding) { out = append(out, b.Name) })
	return out
}
