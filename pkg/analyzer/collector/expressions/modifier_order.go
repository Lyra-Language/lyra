package expressions

import (
	"strings"

	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

// A callable's modifiers have a canonical order and may each appear once. The grammar used
// to enforce both, by spelling them as seven ordered `optional()`s — and that shape was the
// single most expensive thing in the generated parser: `lambda_expr` owned 57,026 of 62,663
// states, because seven independent optionals give the LR automaton 2^7 prefixes to track
// and the GLR conflicts around `(` then multiply each one across the expression grammar.
// Collapsing it to one repeated choice took `src/parser.c` from 116 MB to 12.8 MB, out of
// Git LFS entirely, at the cost of moving these two rules here.
//
// That trade is a good one beyond the size. A parse error could only point at whichever
// token failed to shift — `async pure (…)` reported a problem at `pure`, or at the `(`,
// depending on the state — where this names the modifier and the order to write. It also
// puts the rule beside the *semantic* one it belongs with: `pure` and `det` conflicting is
// already a checker diagnostic (lyra-E015), never a syntax error.
//
// Order matches how the grammar used to sequence them, which is also the order they read in
// naturally: what may I do (`unsafe`), what am I (`pure`/`det`), what may I spend
// (`noalloc`), and how do I run (`async`, `gen`, `rec`).
var modifierOrder = []string{
	"unsafe_modifier",
	"pure_modifier",
	"det_modifier",
	"noalloc_modifier",
	"async_modifier",
	"gen_modifier",
	"rec_modifier",
}

// modifierRank is modifierOrder inverted, so a node kind maps to its canonical position.
var modifierRank = func() map[string]int {
	m := make(map[string]int, len(modifierOrder))
	for i, kind := range modifierOrder {
		m[kind] = i
	}
	return m
}()

// canonicalModifierOrder renders the order for a diagnostic, with the mutually exclusive
// pair written as a choice since a callable may carry only one of them (lyra-E015).
const canonicalModifierOrder = "unsafe, pure|det, noalloc, async, gen, rec"

// CheckModifierOrder reports a callable's modifiers being repeated or written out of
// canonical order. node is the `lambda_expr` (or any node whose modifiers are direct
// children); it walks children in source order, so it sees exactly what was written.
func CheckModifierOrder(node *sitter.Node, ctx *collector_ctx.Ctx) {
	seen := map[string]*sitter.Node{}
	highest := -1
	var highestKind string
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		kind := child.Kind()
		rank, isModifier := modifierRank[kind]
		if !isModifier {
			// The modifiers are a prefix: the first non-modifier child (the parameter
			// list) ends them, and stopping there keeps a modifier-looking token deeper in
			// the body from being read as one.
			break
		}
		if first, repeated := seen[kind]; repeated {
			ctx.AddErrorCoded(child, diag.SeverityError, diag.CodeMalformedModifiers,
				"`%s` is repeated (first written at %s); each modifier may appear once",
				modifierWord(kind), ctx.NodeLocation(first).Pretty())
			continue
		}
		seen[kind] = child
		if rank < highest {
			ctx.AddErrorCoded(child, diag.SeverityError, diag.CodeMalformedModifiers,
				"`%s` must come before `%s`; modifiers are written in the order %s",
				modifierWord(kind), modifierWord(highestKind), canonicalModifierOrder)
			continue
		}
		highest, highestKind = rank, kind
	}
}

// modifierWord is the keyword itself, which is the node kind without its `_modifier`
// suffix — so a diagnostic quotes what the programmer typed.
func modifierWord(kind string) string {
	return strings.TrimSuffix(kind, "_modifier")
}
