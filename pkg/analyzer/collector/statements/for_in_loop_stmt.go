package statements

import (
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	sitter "github.com/tree-sitter/go-tree-sitter"

	"github.com/Lyra-Language/lyra/pkg/cst"
)

func CollectForInLoopExpr(node *sitter.Node, ctx *collector_ctx.Ctx) *ast.ForInLoopExpr {
	loopScope := ctx.PushLoopScope()
	defer ctx.PopScope()

	var label, key, value string
	var keyLoc, valueLoc ast.Location
	var iterable ast.Expression
	var body *ast.BlockExpr
	// The `let (k, v) = <elem>` a destructuring binding desugars to, built while the
	// condition is collected so its names are in scope before the body is walked.
	var destructure *ast.DestructuringDeclStmt

	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		switch child.Kind() {
		case "label":
			label = ctx.NodeText(child)
		case "for_in_condition":
			key, value, keyLoc, valueLoc, iterable, destructure = collectForInCondition(child, ctx)
		case "for_in_body":
			expr := ctx.CollectExpr(child)
			if b, ok := expr.(*ast.BlockExpr); ok {
				body = b
			} else {
				ctx.AddError(child, diag.SeverityError, "expected block expression for for/in loop body")
			}
		}
	}

	if key == "" {
		ctx.AddError(node, diag.SeverityError, "for/in loop missing loop variable")
		return nil
	}
	if iterable == nil {
		ctx.AddError(node, diag.SeverityError, "for/in loop missing iterable expression")
		return nil
	}
	if body == nil {
		ctx.AddError(node, diag.SeverityError, "for/in loop missing body")
		return nil
	}
	// A destructuring binding is the ordinary loop with a destructuring `let` at the top
	// of its body, so nothing after the collector knows the form exists — the same
	// erasure the juxtaposed constructor and a match arm's bare jump already get.
	//
	// Prepending it costs nothing downstream and buys everything: the typechecker,
	// ownership, captures, the range pass and the backend all keep reading one shape,
	// and the destructuring they must already handle for `let (a, b) = e` is the same
	// code. The alternative — a Pattern field beside Key/Value — would make every one of
	// those passes newly wrong about a binder, which is hazard 8's most expensive family.
	if destructure != nil {
		body.Statements = append([]ast.Statement{destructure}, body.Statements...)
	}

	loop := &ast.ForInLoopExpr{
		// The loop's own span. It carried none until 08/18, which is not the cosmetic
		// gap it looks like: a diagnostic reported against a zero Location prints with no
		// `line:col` *and* escapes the driver's per-file filtering, which keeps a
		// location-less diagnostic on the grounds that it is program-level. A warning on
		// a prelude loop therefore appeared on every file compiled.
		ExprBase:      ast.ExprBase{AstBase: ast.AstBase{Location: ctx.NodeLocation(node)}},
		Label:         label,
		Key:           key,
		Value:         value,
		KeyLocation:   keyLoc,
		ValueLocation: valueLoc,
		Iterable:      iterable,
		Body:          body,
	}
	ctx.RecordScope(loop, loopScope)
	return loop
}

func collectForInCondition(node *sitter.Node, ctx *collector_ctx.Ctx) (key, value string, keyLoc, valueLoc ast.Location, iterable ast.Expression, destructure *ast.DestructuringDeclStmt) {
	for i := uint(0); i < node.ChildCount(); i++ {
		child := node.Child(i)
		switch child.Kind() {
		case "tuple_pattern":
			// `for (k, v) in xs` — the element is bound to a synthetic name and taken
			// apart by a `let` the caller prepends to the body.
			//
			// **The name is unforgeable**, on the `_` rule's reasoning: `$` is not in the
			// identifier token's character set, so nothing in the body can refer to the
			// element as a whole, which is exactly what the spelling promises. It is a
			// fixed name rather than a counter because each loop pushes its own scope, so
			// a nested loop shadows rather than collides.
			//
			// **Which slot it sits in decides what it takes apart.** The two-binding form
			// is `for <index>, <element> in …` (`for i, c in s`), so the element is the
			// *second* binding — a pattern in the first slot alongside another binding
			// would be destructuring an integer, which is refused here rather than left to
			// fail as a type error about a name the author did not write.
			if isSecondBinding(node, child) {
				value = forInElementName
				valueLoc = ctx.NodeLocation(child)
				registerLoopVar(child, value, ctx)
			} else if hasSecondBinding(node) {
				ctx.AddError(child, diag.SeverityError,
					"the first binding of a two-name loop is the index, which cannot be destructured; "+
						"write `for i, (a, b) in …` to take the element apart")
				continue
			} else {
				key = forInElementName
				keyLoc = ctx.NodeLocation(child)
				registerLoopVar(child, key, ctx)
			}
			pattern := ctx.CollectPattern(child)
			if pattern == nil {
				continue
			}
			destructure = &ast.DestructuringDeclStmt{
				AstBase: ast.AstBase{Location: ctx.NodeLocation(child)},
				Keyword: "let",
				Pattern: pattern,
				Value: &ast.IdentifierExpr{
					ExprBase: ast.ExprBase{AstBase: ast.AstBase{Location: ctx.NodeLocation(child)}},
					Name:     forInElementName,
				},
			}
			// Registered here rather than by the prepended statement, because the body is
			// collected *after* this condition and a name it references must already be
			// bound. Conflicts among them are the destructuring declaration's own concern
			// and are reported there; a loop pattern binds into a fresh scope, so the only
			// collision available is between its own elements.
			for _, name := range ast.PatternBoundNames(pattern) {
				ctx.RegisterDestructuredName(name, destructure)
			}
		case "for_variable_or_key":
			key = ctx.NodeText(child)
			keyLoc = ctx.NodeLocation(child)
			registerLoopVar(child, key, ctx)
		case "for_index_or_value":
			value = ctx.NodeText(child)
			valueLoc = ctx.NodeLocation(child)
			registerLoopVar(child, value, ctx)
		default:
			// Not `else`: a comment after `in` would overwrite the iterable with a nil,
			// which is rule 3's typed nil in the one slot the loop cannot do without.
			if child.IsNamed() && !cst.IsComment(child) {
				iterable = ctx.CollectExpr(child)
			}
		}
	}
	return
}

func registerLoopVar(node *sitter.Node, name string, ctx *collector_ctx.Ctx) {
	v := &ast.VarDeclStmt{
		AstBase:     ast.AstBase{Location: ctx.NodeLocation(node)},
		BindingKind: ast.BindingLet,
		Name:        name,
	}
	if err := ctx.RegisterVariable(v); err != nil {
		ctx.AddError(node, diag.SeverityError, "failed to register loop variable %q: %v", name, err)
	}
}

// forInElementName is the binding a destructuring loop header gives the element before
// taking it apart. `$` is outside the identifier token's character set, so no program can
// write this name — the same unforgeability `_` has as a loop binding.
const forInElementName = "for$elem"

// sawSeparator reports whether a ',' precedes child among cond's children — i.e. whether
// this binding is the *second* of the two-name form. The slot cannot be read off the node
// kind, since a `tuple_pattern` is aliased to nothing and looks identical in both.
func sawSeparator(cond *sitter.Node, child *sitter.Node) bool {
	for i := uint(0); i < cond.ChildCount(); i++ {
		c := cond.Child(i)
		if c.Id() == child.Id() {
			return false
		}
		if c.Kind() == "," {
			return true
		}
	}
	return false
}

// isSecondBinding reports whether child sits after the ',' in the condition — i.e. whether
// it is the *element* of the two-name form rather than the first binding. The slot cannot
// be read off the node's kind, since a `tuple_pattern` looks identical in both.
func isSecondBinding(cond *sitter.Node, child *sitter.Node) bool {
	for i := uint(0); i < cond.ChildCount(); i++ {
		c := cond.Child(i)
		if c.Id() == child.Id() {
			return false
		}
		if c.Kind() == "," {
			return true
		}
	}
	return false
}

// hasSecondBinding reports whether the condition binds two names.
func hasSecondBinding(cond *sitter.Node) bool {
	for i := uint(0); i < cond.ChildCount(); i++ {
		if cond.Child(i).Kind() == "," {
			return true
		}
	}
	return false
}
