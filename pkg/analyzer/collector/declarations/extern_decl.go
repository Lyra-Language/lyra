package declarations

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/expressions"
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/cst"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

// CollectExternDeclaration builds an `extern` declaration.
//
// The grammar admits more than the language means — order, duplicates, the modifiers that
// say nothing about a foreign function, and an `unsafe` written *after* `extern` — because
// one `fn_modifiers` is four times cheaper in parser states than stacked optionals (the
// measurement is in the grammar repo's CLAUDE.md). This is where the difference is paid:
// each of those is reported here, with a message naming the fix, rather than as a syntax
// error pointing at whichever token failed to shift. Same trade `let` makes.
func CollectExternDeclaration(node *sitter.Node, ctx *collector_ctx.Ctx) *ast.ExternDeclStmt {
	nameNode, ok := ctx.MustField(node, "name")
	if !ok {
		return nil
	}
	signatureNode, ok := ctx.MustField(node, "signature")
	if !ok {
		return nil
	}
	signature := ctx.ParseLambdaType(signatureNode)
	if signature == nil {
		ctx.AddError(signatureNode, diag.SeverityError,
			"could not parse the signature of `extern %s`", ctx.NodeText(nameNode))
		return nil
	}

	decl := &ast.ExternDeclStmt{
		AstBase:      ast.AstBase{Location: ctx.NodeLocation(node)},
		Name:         ctx.NodeText(nameNode),
		NameLocation: ctx.NodeLocation(nameNode),
		Signature:    signature,
		IsUnsafe:     cst.Field(node, "is_unsafe") != nil,
	}
	if mods := cst.Field(node, "modifiers"); mods != nil {
		expressions.CheckModifierOrder(mods, ctx)
		applyExternModifiers(mods, decl, ctx)
	}
	decl.Links = collectLinkAttributes(node, ctx)
	return decl
}

// applyExternModifiers reads the effect bound off the modifier list and refuses the rest.
//
// `async`, `gen` and `rec` describe how a *Lyra* body runs, and an extern has none — a
// foreign function is not suspended, does not yield and does not recurse into a body the
// compiler emitted. Admitting them silently would make the declaration say something the
// compiler cannot mean by it.
//
// `unsafe` is refused *here* specifically: the grammar accepts it after `extern` because
// it is one of the seven `fn_modifiers`, and the language wants it before, where it reads
// as marking the declaration rather than as one bound among several.
func applyExternModifiers(mods *sitter.Node, decl *ast.ExternDeclStmt, ctx *collector_ctx.Ctx) {
	for i := uint(0); i < mods.NamedChildCount(); i++ {
		child := mods.NamedChild(i)
		switch child.Kind() {
		case "pure_modifier":
			decl.IsPure = true
		case "det_modifier":
			decl.IsDet = true
		case "noalloc_modifier":
			decl.IsNoAlloc = true
		case "unsafe_modifier":
			ctx.AddErrorCoded(child, diag.SeverityError, diag.CodeMalformedModifiers,
				"write `unsafe` before `extern`: it marks the declaration as asserting a "+
					"bound the compiler cannot check, not one bound among several")
		case "async_modifier", "gen_modifier", "rec_modifier":
			ctx.AddErrorCoded(child, diag.SeverityError, diag.CodeMalformedModifiers,
				"`%s` says nothing about a foreign function: it describes how a Lyra body "+
					"runs, and an extern has none", ctx.NodeText(child))
		}
	}
}

// collectLinkAttributes reads `@link("m")` off the declaration, in source order.
//
// Only `link` is recognized; any other attribute on an extern is reported rather than
// ignored, on the standing rule that a surface which parses and is read by nobody costs
// more than an absent one.
func collectLinkAttributes(node *sitter.Node, ctx *collector_ctx.Ctx) []string {
	attrs := cst.Field(node, "attributes")
	if attrs == nil {
		return nil
	}
	var links []string
	for i := uint(0); i < attrs.NamedChildCount(); i++ {
		attr := attrs.NamedChild(i)
		nameNode := cst.Field(attr, "name")
		if nameNode == nil {
			continue
		}
		if ctx.NodeText(nameNode) != "link" {
			ctx.AddError(attr, diag.SeverityError,
				"unknown attribute `@%s` on an extern; the only one is `@link(\"name\")`",
				ctx.NodeText(nameNode))
			continue
		}
		args := cst.Field(attr, "args")
		if args == nil {
			ctx.AddError(attr, diag.SeverityError,
				"`@link` needs the library to link, as a string: `@link(\"m\")`")
			continue
		}
		for j := uint(0); j < args.NamedChildCount(); j++ {
			arg := args.NamedChild(j)
			if arg.Kind() != "string_literal" {
				ctx.AddError(arg, diag.SeverityError,
					"`@link` takes a library name as a string: `@link(\"m\")`")
				continue
			}
			// The name as the linker takes it, not a flag: `@link("m")` becomes `-lm`.
			// Reading the literal's text rather than evaluating it is what keeps an
			// attribute argument data — see the grammar note on why it is a plain string.
			links = append(links, stringLiteralText(arg, ctx))
		}
	}
	return links
}

// stringLiteralText is the content of a plain string literal, without its quotes.
func stringLiteralText(node *sitter.Node, ctx *collector_ctx.Ctx) string {
	for i := uint(0); i < node.NamedChildCount(); i++ {
		child := node.NamedChild(i)
		if child.Kind() == "string_content" {
			return ctx.NodeText(child)
		}
	}
	return ""
}
