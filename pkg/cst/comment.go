package cst

import sitter "github.com/tree-sitter/go-tree-sitter"

// IsComment reports whether n is one of the grammar's comment kinds.
//
// Comments are `extras` — the grammar lets them appear between any two tokens — and all
// three kinds are **named** nodes. So `child.IsNamed()`, which is how every list collector
// tells an element from a comma, is *true* for a comment sitting inside the list, and a
// comma-separated list with a comment in it collects one element too many.
//
// The element is a nil `ast.Expression`, which is rule 3's typed nil: it survives an
// `expr == nil` test and its symptom arrives in a later pass, far from the comment.
// `f(1, // hi` + newline + `2)` was reported as *"expected 2 argument(s), got 3"* — a
// diagnostic accusing correct code of a different mistake — and `[1, // one` + `2]` passed
// every analysis pass and died in the backend as *"expression lowering not implemented for
// <nil>"*.
//
// A predicate rather than a filtered-children accessor because the sites are ordinary
// index loops that also read fields and non-named tokens; naming the thing being skipped is
// what makes the skip reviewable.
func IsComment(n *sitter.Node) bool {
	if n == nil {
		return false
	}
	switch n.Kind() {
	case "comment", "doc_comment", "inner_doc_comment":
		return true
	}
	return false
}
