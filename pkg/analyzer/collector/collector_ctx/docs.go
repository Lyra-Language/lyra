package collector_ctx

import (
	"github.com/Lyra-Language/lyra/pkg/ast"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

// Doc comments are `extras` in the grammar, so they are not children of the thing they
// document — they are *siblings* sitting before it, at whatever level of the tree that
// thing lives. That is what makes one helper enough for every site: a top-level `let`, a
// struct field, a data constructor, a trait's method signature and an impl's method all
// ask the same question of their own node, and none of them needs a grammar field.
//
// The attachment rule is adjacency, and it is deliberately strict:
//
//	/// documented          <- attaches
//	let a = 1
//
//	/// not documented      <- warns (lyra-W017): a blank line breaks the run
//
//	let b = 2
//
// A blank line is the one signal an author has for "this comment is about the file, not
// about the next declaration", and the alternative — attaching across a gap — makes the
// last comment in a file silently become the documentation of whatever is appended after
// it. Requiring adjacency means the mistake is a warning at a spot the author can see,
// which is the trade the whole language makes.

// DocFor returns the `///` block immediately above node, or nil. The comment nodes it
// consumes are marked claimed, so ReportStrayDocs can report the ones nothing took.
func (ctx *Ctx) DocFor(node *sitter.Node) *ast.Doc {
	if node == nil {
		return nil
	}
	var run []*sitter.Node
	// nextRow is the row the run must end on to be contiguous with what follows —
	// the declaration's first row to begin with, then each comment's own row as the
	// walk moves up.
	nextRow := node.StartPosition().Row
	for sib := prevSibling(node); sib != nil; sib = prevSibling(sib) {
		if sib.Kind() != "doc_comment" {
			// A token on the documented thing's **own line** may sit between it
			// and the comment without breaking adjacency. The case is the `|`
			// separating `data` constructors:
			//
			//	data Dir =
			//	  /// Towards the top of the map.
			//	  North
			//	  /// Towards the bottom of the map.
			//	  | South
			//
			// The `|` is an anonymous sibling of the constructor, so a plain
			// walk stops on it and documents the first constructor but no
			// other — the leading-bar style is the one most people write, and
			// it would have been the style that silently did not work.
			//
			// Only before any comment has been collected, and only on the same
			// row, so this cannot reach across a line and turn a stray comment
			// into documentation.
			if len(run) == 0 && sib.EndPosition().Row == nextRow {
				continue
			}
			break
		}
		row := sib.StartPosition().Row
		if row+1 != nextRow {
			break
		}
		run = append(run, sib)
		nextRow = row
	}
	if len(run) == 0 {
		return nil
	}
	// The walk collected bottom-up; a doc block reads top-down.
	slicesReverse(run)
	return ctx.buildDoc(run, false)
}

// ModuleDocFor returns the `//!` block in the file's header region, or nil.
//
// The header region runs to the first declaration, with **the `module` line itself
// allowed inside it** — so both of these document the module:
//
//	//! Maybe and the combinators over it.        module std.prelude
//	module std.prelude                            //! Maybe and the combinators over it.
//
// Only the top-of-file form exists in Rust, which has no module header to write it
// under; here the line naming the module is the obvious thing for its documentation to
// follow, and refusing that spelling would make the natural one warn. It is admitted
// exactly once and only as the *first* statement, so a `//!` further down is still the
// stray it looks like.
//
// Several runs in the region are joined, because the alternative is picking one and
// silently dropping the rest.
func (ctx *Ctx) ModuleDocFor(root *sitter.Node) *ast.Doc {
	var run []*sitter.Node
	seenModuleHeader := false
header:
	for i := uint(0); i < root.ChildCount(); i++ {
		child := root.Child(i)
		switch child.Kind() {
		case "inner_doc_comment":
			run = append(run, child)
		case "comment", "doc_comment":
			// An ordinary comment above the header is a license banner or an
			// editor directive, and a `///` there is a stray this does not
			// silently absorb — neither ends the header region.
		case "module_declaration":
			if seenModuleHeader {
				break header
			}
			seenModuleHeader = true
		default:
			// The first real declaration. Everything after it is out of the
			// header, and a `//!` down there is reported by ReportStrayDocs.
			break header
		}
	}
	if len(run) == 0 {
		return nil
	}
	return ctx.buildDoc(run, true)
}

// buildDoc turns a run of comment nodes into a Doc, spanning them all, and marks each
// as claimed.
func (ctx *Ctx) buildDoc(run []*sitter.Node, isInner bool) *ast.Doc {
	lines := make([]string, 0, len(run))
	for _, n := range run {
		lines = append(lines, ctx.NodeText(n))
		ctx.claimDoc(n)
	}
	loc := ctx.NodeLocation(run[0])
	last := ctx.NodeLocation(run[len(run)-1])
	loc.EndLine, loc.EndCol = last.EndLine, last.EndCol
	return ast.NewDoc(lines, loc, isInner)
}

// claimDoc records that a comment node was attached to something. The key is the start
// byte, which is unique per file and stable across the walk — a *sitter.Node is a value
// handle rather than an identity, so it cannot be the key itself.
func (ctx *Ctx) claimDoc(node *sitter.Node) {
	if ctx.claimedDocs == nil {
		ctx.claimedDocs = make(map[uint]bool)
	}
	ctx.claimedDocs[node.StartByte()] = true
}

// ResetDocs clears the per-file claim set. Called by the collector before each file is
// walked; without it the second file's byte offsets are tested against the first's.
func (ctx *Ctx) ResetDocs() { ctx.claimedDocs = nil }

// ReportStrayDocs warns (lyra-W017) for every doc comment in the tree that attached to
// nothing.
//
// A doc comment that documents nothing is silently discarded otherwise, which is the
// exact failure this language spends its design budget avoiding — the author writes the
// documentation, the generator never sees it, and nothing says so. It is a *warning*
// rather than an error because a commented-out declaration below a doc block is an
// ordinary intermediate state and must not break the build.
//
// It is one post-pass over the tree rather than a check at each attachment site, because
// the sites cannot see each other: whether a `///` was claimed is only knowable once
// every collector that might have claimed it has run.
func (ctx *Ctx) ReportStrayDocs(root *sitter.Node) {
	cursor := root.Walk()
	defer cursor.Close()

	var visit func(node *sitter.Node)
	visit = func(node *sitter.Node) {
		kind := node.Kind()
		if kind == "doc_comment" || kind == "inner_doc_comment" {
			if !ctx.claimedDocs[node.StartByte()] {
				ctx.reportStrayDoc(node, kind == "inner_doc_comment")
			}
			return
		}
		for i := uint(0); i < node.ChildCount(); i++ {
			visit(node.Child(i))
		}
	}
	visit(root)
}

// reportStrayDoc emits the warning, with advice keyed to which marker was used — the two
// are stray for different reasons and have different fixes.
func (ctx *Ctx) reportStrayDoc(node *sitter.Node, isInner bool) {
	if isInner {
		ctx.AddErrorCoded(node, diag.SeverityWarning, diag.CodeStrayDocComment,
			"`//!` documents the module and must sit in the file's header — at the top, or directly under the `module` line; write `///` to document a declaration, or `//` for an ordinary comment")
		return
	}
	ctx.AddErrorCoded(node, diag.SeverityWarning, diag.CodeStrayDocComment,
		"this doc comment documents nothing — a `///` block must sit on the line directly above the declaration it describes; write `//` for an ordinary comment")
}

// prevSibling is node.PrevSibling, except that it climbs out of a node it begins.
//
// It exists for one tree-sitter placement rule that is invisible until it bites: an
// extra lexed before a node's **first token** is attached to the *enclosing* node, not
// pushed inside. So in
//
//	trait Show {
//	  /// Renders self as a string.
//	  show: (Self) -> string
//	  /// Renders self in a debug form.
//	  debug: (Self) -> string
//	}
//
// the second doc comment is a sibling of its `trait_method`, inside `trait_methods` —
// and the **first** one is a sibling of `trait_methods` itself, one level up, because
// the `{` belongs to `trait_declaration`. A plain PrevSibling walk therefore documents
// every method in a body except the first, which is worse than documenting none: it is
// a rule with an invisible exception, and the exception is the method most likely to
// carry the block explaining the whole trait.
//
// The climb is guarded by `parent.StartByte() == node.StartByte()` — the node must
// begin its parent, so nothing of the parent's own syntax sits between the comment and
// what it documents. `struct_type_body` includes its own `{`, so a struct's fields never
// take this path; `trait_methods` and `impl_methods` do not, so their first member
// always does.
func prevSibling(node *sitter.Node) *sitter.Node {
	if sib := node.PrevSibling(); sib != nil {
		return sib
	}
	parent := node.Parent()
	if parent == nil || parent.StartByte() != node.StartByte() {
		return nil
	}
	return prevSibling(parent)
}

func slicesReverse(nodes []*sitter.Node) {
	for i, j := 0, len(nodes)-1; i < j; i, j = i+1, j-1 {
		nodes[i], nodes[j] = nodes[j], nodes[i]
	}
}
