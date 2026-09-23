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
	case *OrPattern:
		// Its alternatives are patterns, so a walk must reach them even though none of
		// them binds — a literal's *width* is still checked per pattern, and a walk that
		// stopped here would leave `1 | 300` against a `u8` unchecked on the second.
		for _, alt := range pat.Alternatives {
			WalkPattern(alt, onPattern)
		}
	case *IdentifierPattern, *LiteralPattern, *RestPattern, *RangePattern,
		*WildcardPattern, *RegexPattern:
		// Leaves: nothing inside to visit.
	}
}

// PatternsOf returns the patterns a statement or expression holds directly.
//
// The other half of the walk: patterns hang off ordinary nodes, so reaching them means
// walking statements and expressions as usual and asking each one. The node kinds that hold
// them — a destructuring declaration (bare, `if let`, `let … else`), a match arm's
// expression, a lambda's parameters and a multi-clause lambda's clauses — are the list to
// extend when another appears.
func PatternsOf(node AstNode) []Pattern {
	switch n := node.(type) {
	case *DestructuringDeclStmt:
		return []Pattern{n.Pattern}
	case *IfDestructuringStmt:
		// The declaration is held **by value**, so the statement walk never visits it as a
		// DestructuringDeclStmt of its own; missing these made an `if let` pattern
		// invisible to every pattern-position editor feature.
		return []Pattern{n.DestructuringStatement.Pattern}
	case *ElseDestructuringStmt:
		return []Pattern{n.DestructuringStatement.Pattern}
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

// RangeBoundNames returns the `const` names the range patterns node holds were written
// with (`LOW` and `HIGH` in `LOW..<=HIGH`), at any depth.
//
// **Patterns otherwise hold no names a program reads**, so a pass collecting references
// by walking expressions misses these twice over: the walk does not enter patterns, and
// after the typechecker the bounds are literals anyway. Without it an import used only as
// a bound warned as unused (lyra-W004), advising the deletion of a name the program needs.
func RangeBoundNames(node AstNode) []string {
	var names []string
	for _, id := range RangeBounds(node) {
		names = append(names, id.Name)
	}
	return names
}

// ConstRefNames is every `const` name node holds that the typechecker may have **folded to
// a literal**, so a pass counting references sees it as used.
//
// Two constructs erase a name this way, and both cost a diagnostic before they were
// answered here: a range-pattern bound (`LOW..<=HIGH`) and a fixed array's repeat count
// (`#[v; N]`). Each is folded on purpose — one rewrite beats teaching every later pass to
// resolve a `const` — and the cost lands on whoever asks a question about names afterwards.
// A `const` used only in one of these positions warned as unused (lyra-W003), and an import
// used only there warned as lyra-W004, which breaks the program if believed.
//
// **One helper rather than two**, because both callers want the same thing and a second
// list is the drift a pass discovers years later (rule 8). Anything else that folds a name
// away belongs here too.
func ConstRefNames(node AstNode) []string {
	return append(RangeBoundNames(node), RepeatCountNames(node)...)
}

// RepeatCountNames returns the names a fixed array's repeat count was written with, once
// the typechecker has folded it (ArrayRepeatExpr.CountAsWritten). Empty for every other
// node, and for a count that was already a literal.
//
// Every identifier in the expression, not just a bare one: `#[0; N * 2]` folds through the
// arithmetic and erases `N` just the same.
func RepeatCountNames(node AstNode) []string {
	repeat, ok := node.(*ArrayRepeatExpr)
	if !ok || repeat.CountAsWritten == nil {
		return nil
	}
	var names []string
	WalkExpr(repeat.CountAsWritten, nil, func(e Expression) bool {
		if id, isIdent := e.(*IdentifierExpr); isIdent {
			names = append(names, id.Name)
		}
		return true
	})
	return names
}

// RangeBounds is RangeBoundNames with the nodes as written, locations included.
func RangeBounds(node AstNode) []*IdentifierExpr {
	var ids []*IdentifierExpr
	for _, p := range PatternsOf(node) {
		WalkPattern(p, func(sub Pattern) bool {
			if rp, ok := sub.(*RangePattern); ok {
				ids = append(ids, rp.ConstBounds...)
			}
			return true
		})
	}
	return ids
}

// PatternBinding is one name a pattern introduces, and the node to attribute it to.
//
// Node is nil where the language binds a name that no node carries on its own: a struct
// pattern's shorthand (`{ x }` binds the field) and a rest pattern's `...xs` each name
// something the pattern spells rather than a sub-pattern. A caller that needs a `Named` —
// the collector, entering these into a scope — uses `At` for those.
//
// Loc is the span of the **name**, not of the pattern spelling it: `rr` in `rr @ Rect(_, _)`,
// `more` in `...more`. It is the declaration's position wherever a binding is one — the span
// a rename edits — and the whole pattern there made a rename replace `rr @ Rect(_, _)`, or
// drop the dots of `...more`.
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
			fn(PatternBinding{Name: b.Name, Loc: leadingName(b.GetLocation(), b.Name)})
		case *RestPattern:
			if b.Identifier != "" {
				fn(PatternBinding{Name: b.Identifier, Loc: trailingName(b.GetLocation(), b.Identifier)})
			}
		case *StructPatternField:
			if b.Pattern == nil {
				fn(PatternBinding{Name: b.Name, Node: b, Loc: b.GetLocation()})
			}
		}
		return true
	})
}

// leadingName narrows loc to the name that begins it (`rr` of `rr @ Rect(_, _)`).
func leadingName(loc Location, name string) Location {
	loc.EndLine, loc.EndCol = loc.StartLine, loc.StartCol+len(name)
	return loc
}

// trailingName narrows loc to the name that ends it (`more` of `...more`). From the end
// rather than past three dots, because a rest the typechecker synthesized for `Rect pair`
// spans only the name.
func trailingName(loc Location, name string) Location {
	loc.StartLine, loc.StartCol = loc.EndLine, loc.EndCol-len(name)
	return loc
}

// PatternBindingNamed is the first binding of name in p.
func PatternBindingNamed(p Pattern, name string) (PatternBinding, bool) {
	var out PatternBinding
	found := false
	EachPatternBinding(p, func(b PatternBinding) {
		if !found && b.Name == name {
			out, found = b, true
		}
	})
	return out, found
}

// PatternBoundNames is EachPatternBinding's names, in source order.
func PatternBoundNames(p Pattern) []string {
	var out []string
	EachPatternBinding(p, func(b PatternBinding) { out = append(out, b.Name) })
	return out
}

// UnwrapBinding strips any `name @ …` wrappers, returning the pattern that decides whether a
// value *matches*.
//
// A binding pattern binds unconditionally and tests nothing, so every question about what an
// arm matches — which constructor it covers, whether it is a catch-all, whether it is
// irrefutable — is a question about what is inside it. Asking with a direct type assertion
// instead is how `w @ Box(n)` came to cover no constructor at all, which reported a match as
// non-exhaustive that covered every case.
//
// Loops rather than recursing once: `a @ b @ p` is unusual but parses.
func UnwrapBinding(p Pattern) Pattern {
	for {
		bp, ok := p.(*BindingPattern)
		if !ok || bp.Pattern == nil {
			return p
		}
		p = bp.Pattern
	}
}
