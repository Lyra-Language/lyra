package expressions

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/cst"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func CollectMatchExpression(node *sitter.Node, ctx *collector_ctx.Ctx, loc ast.Location) ast.Expression {
	valueNode, ok := ctx.MustField(node, "value")
	if !ok {
		return nil
	}
	value := CollectExpression(valueNode, ctx)
	if value == nil {
		ctx.AddError(node, diag.SeverityError, "CollectMatchExpression: value is nil")
		return nil
	}
	matchArms := CollectMatchArms(node, ctx)
	if matchArms == nil {
		ctx.AddError(node, diag.SeverityError, "CollectMatchExpression: match arms are nil")
		return nil
	}
	return &ast.MatchExpr{
		ExprBase:  ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
		Scrutinee: value,     // guaranteed to be non-nil
		MatchArms: matchArms, // guaranteed to be non-nil
	}
}

func CollectMatchArms(node *sitter.Node, ctx *collector_ctx.Ctx) []ast.MatchArm {
	matchArms := []ast.MatchArm{}
	for i := uint(0); i < node.ChildCount(); i++ {
		if node.Child(i).Kind() == "match_arm" {
			matchArm := CollectMatchArm(node.Child(i), ctx)
			if matchArm == nil {
				ctx.AddError(node, diag.SeverityError, "CollectMatchArms: match arm is nil")
				return nil
			}
			matchArms = append(matchArms, *matchArm)
		}
	}
	return matchArms
}

func CollectMatchArm(node *sitter.Node, ctx *collector_ctx.Ctx) *ast.MatchArm {
	patternNode, ok := ctx.MustField(node, "pattern")
	if !ok {
		return nil
	}
	pattern := ctx.CollectPattern(patternNode)

	// **An arm is a scope, and its pattern's bindings live in it.** Until 08/22 no scope
	// was pushed here and nothing was registered, so `m` in `Mouse(m) => m.button` was
	// bound by the typechecker for its own purposes and known to nobody else: every
	// scope-based question — go-to-definition on a *use* of `m`, hover, find-references —
	// answered nothing, because the name was in no scope at all.
	//
	// Pushed before the guard, not just before the body: `Some(v) if v > 0 => …` is
	// exactly where a guard is worth having, and the guard sees what the pattern binds.
	armScope := ctx.PushBlockScope()
	definePatternBindings(pattern, ctx)

	guardNode := cst.Field(node, "guard")
	var guard *ast.GuardExpr = nil
	if guardNode != nil {
		guard = collectGuard(guardNode, ctx)
	}
	bodyNode, ok := ctx.MustField(node, "body")
	if !ok {
		ctx.PopScope()
		ctx.AddError(node, diag.SeverityError, "CollectMatchArm: body node is missing")
		return nil
	}
	body := collectMatchArmBody(bodyNode, ctx)
	ctx.PopScope()
	if body == nil {
		ctx.AddError(node, diag.SeverityError, "CollectMatchArm: body is nil")
		return nil
	}

	// Recorded against the **pattern**, not the body. A braced body is a BlockExpr that
	// pushed and recorded a scope of its own — a child of this one — so recording the arm
	// scope against the body would overwrite the block's entry with its own parent, and
	// every binding declared inside the block would resolve nowhere. The pattern is the
	// one node an arm always has and nothing else claims.
	ctx.RecordScope(pattern, armScope)

	return &ast.MatchArm{
		Pattern: pattern,
		Guard:   guard,
		Body:    body,
	}
}

// definePatternBindings enters every name a pattern binds into the current scope.
//
// Walked with `ast.WalkPattern` rather than by a switch of its own: that walker exists
// precisely so a new consumer does not add an eleventh hand-rolled pattern traversal, and a
// binder that misses a kind is the quiet failure — a name bound by the language and known to
// no pass, which is what this function is fixing.
func definePatternBindings(pattern ast.Pattern, ctx *collector_ctx.Ctx) {
	ast.WalkPattern(pattern, func(p ast.Pattern) bool {
		switch b := p.(type) {
		case *ast.IdentifierPattern:
			ctx.DefinePatternBinding(b)
		case *ast.BindingPattern:
			// `name @ pattern` binds the name *and* everything inside, so the walk
			// continues past it.
			ctx.DefinePatternBinding(b)
		}
		return true
	})
}

// armJumpKinds are the statement forms a match arm may hold *bare*, without the
// braces a block would need: `None => break`, `_ => continue`, `Err e => return e`.
var armJumpKinds = map[string]bool{
	"break_statement":    true,
	"continue_statement": true,
	"return_statement":   true,
}

// collectMatchArmBody collects an arm body, **erasing the bare-jump spelling**: a
// bare `break`/`continue`/`return` becomes the single-statement block the braced
// form already produced, so `None => break` and `None => { break }` collect to
// byte-identical ASTs.
//
// Doing it here is what makes the feature cheap. The jump forms are statements and
// an arm body is an expression, so the alternative — teaching `MatchArm.Body` to
// hold a statement — would push the distinction through the typechecker, the purity
// and ownership passes, and all four of the backend's arm-body lowering sites, each
// of which would need a case that does exactly what the block case already does.
// Erasing it at the boundary means nothing after the collector learns this
// alternative exists, which is the same treatment juxtaposed constructor
// application gets (`Some 42` collects as `Some(42)`).
//
// The braced form already worked end to end — the backend seals a block whose
// statement jumped and `matchMerge` drops an arm whose block is sealed — so this
// adds a spelling rather than a behaviour.
func collectMatchArmBody(bodyNode *sitter.Node, ctx *collector_ctx.Ctx) ast.Expression {
	if !armJumpKinds[bodyNode.Kind()] {
		return CollectExpression(bodyNode, ctx)
	}
	stmt := ctx.CollectStatement(bodyNode)
	if stmt == nil {
		return nil
	}
	loc := ctx.NodeLocation(bodyNode)
	return &ast.BlockExpr{
		ExprBase:   ast.ExprBase{AstBase: ast.AstBase{Location: loc}},
		Statements: []ast.Statement{stmt},
	}
}
