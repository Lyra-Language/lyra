package declarations

import (
	"fmt"

	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/cst"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/types"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

func bindingKind(keyword string, ctx *collector_ctx.Ctx) ast.BindingKind {
	switch keyword {
	case "let":
		return ast.BindingLet
	case "var":
		return ast.BindingVar
	case "const":
		return ast.BindingConst
	default:
		ctx.AddError(nil, diag.SeverityError, "invalid binding kind: %s", keyword)
		return ast.BindingUnknown
	}
}

// CollectVariableDeclaration handles both plain identifier bindings (VarDeclStmt)
// and pattern bindings (DestructuringDeclStmt) under the unified declaration rule.
// The two branches are distinguished by which field is present: "name" for
// identifier bindings, "pattern" for destructuring bindings.
func CollectVariableDeclaration(node *sitter.Node, ctx *collector_ctx.Ctx) ast.Statement {
	if nameNode := cst.Field(node, "name"); nameNode != nil {
		return collectIdentifierDeclaration(node, nameNode, ctx)
	}
	if patternNode := cst.Field(node, "pattern"); patternNode != nil {
		return collectPatternDeclaration(node, patternNode, ctx)
	}
	ctx.AddError(node, diag.SeverityError, "declaration missing both name and pattern fields")
	return nil
}

func collectIdentifierDeclaration(node *sitter.Node, nameNode *sitter.Node, ctx *collector_ctx.Ctx) *ast.VarDeclStmt {
	kind := bindingKind(ctx.NodeText(cst.Field(node, "keyword")), ctx)
	isPublic := cst.Field(node, "visibility") != nil
	isMut := cst.Field(node, "mutability") != nil
	if isMut && kind == ast.BindingVar {
		ctx.AddError(node, diag.SeverityWarning,
			"`var mut` is redundant: a `var` is already interior-mutable; write `var` (or use `let mut` for a non-reassignable but interior-mutable binding)")
	}
	name := ctx.NodeText(nameNode)

	genericParametersNode := cst.Field(node, "generic_parameters")
	genericParameters := []ast.GenericParam{}
	if genericParametersNode != nil {
		genericParameters = ctx.CollectGenericParams(genericParametersNode)
	}
	if whereNode := cst.Field(node, "generic_parameter_constraints"); whereNode != nil {
		genericParameters = ctx.MergeWhereConstraints(genericParameters, whereNode)
	}

	var varType types.Type
	if typeAnnotation := cst.Field(node, "type_annotation"); typeAnnotation != nil {
		varType = ctx.ParseType(cst.Field(typeAnnotation, "type"))
	}

	valueNode := cst.Field(node, "value")
	var initExpr ast.Expression
	if valueNode != nil {
		initExpr = ctx.CollectExpr(valueNode)
	}

	// Modifier-led function sugar (`let pure name(params) => body`): the
	// modifiers lead the name rather than the lambda, so lift them onto the
	// collected LambdaExpr value. The result is identical to the equivalent
	// `let name = pure (params) => body` binding. The grammar only admits a
	// lambda value in this arm, so any non-lambda here is a parse-level
	// impossibility; guard anyway and report it rather than silently drop.
	if modifiersNode := cst.Field(node, "modifiers"); modifiersNode != nil {
		if lambda, ok := initExpr.(*ast.LambdaExpr); ok {
			applyFunctionModifiers(modifiersNode, lambda, ctx)
		} else {
			ctx.AddError(modifiersNode, diag.SeverityError,
				"function modifiers may only lead a function definition")
		}
	}

	// Lift the declaration's `where` bounds onto the lambda, for the same reason the
	// modifiers above are lifted: they are written on the *binding* while every
	// consumer downstream holds only the LambdaExpr. Without this the bounds were
	// collected onto the declaration and read by nobody but the unused-parameter
	// warning — `where t: Show` constrained nothing and did not even bring `show`
	// into scope in the body.
	if lambda, ok := initExpr.(*ast.LambdaExpr); ok {
		// The parameter list itself, in order, for the turbofish to bind against.
		lambda.GenericParams = genericParameters
		for _, p := range genericParameters {
			if len(p.Constraints) == 0 {
				continue
			}
			if lambda.GenericBounds == nil {
				lambda.GenericBounds = map[string][]string{}
			}
			lambda.GenericBounds[p.Name] = p.Constraints
		}
	}

	astNode := &ast.VarDeclStmt{
		AstBase:       ast.AstBase{Location: ctx.NodeLocation(node)},
		BindingKind:   kind,
		IsPublic:      isPublic,
		IsMut:         isMut,
		Name:          name,
		NameLocation:  ctx.NodeLocation(nameNode),
		GenericParams: genericParameters,
		Type:          varType,
		Value:         initExpr,
	}

	if existing, alreadyDeclared := ctx.LookupCurrentScope(name); alreadyDeclared {
		// Receiver-keyed overloading, asked first because the branch below would
		// otherwise consume the case: two module-level `let`s of one name look exactly
		// like sequential rebinding, and rebinding would keep the second and drop the
		// first with nothing reported. A refusal falls through to the existing paths,
		// so nothing that was legal before changes meaning.
		if accepted, _ := ctx.DeclareOverload(astNode); accepted {
			return astNode
		}
		if v, ok := existing.(*ast.VarDeclStmt); ok && v.BindingKind != ast.BindingConst {
			// Same-scope sequential rebinding (e.g. `let x = parse(x)`) is
			// idiomatic and safe in ML-family languages, so it is allowed for
			// let/var bindings. Replace the prior binding so later references
			// resolve to this declaration. Genuinely confusing nested-scope
			// shadowing is still reported separately by CheckShadowing.
			// Remember the prior binding so the typechecker can resolve a
			// self-reference in this declaration's own initializer (`let x = x + 1`)
			// to the previous value rather than to this not-yet-typed one.
			astNode.Shadows = v
			ctx.RedefineVariable(astNode)
		} else {
			ctx.AddErrorRelated(node, diag.SeverityError,
				[]diag.RelatedInformation{{Location: existing.GetLocation(), Message: "previously declared here"}},
				"%s", redeclarationMessage(existing, name))
		}
	} else if err := ctx.RegisterVariable(astNode); err != nil {
		// Unexpected registration failure (should not normally happen).
		ctx.AddError(node, diag.SeverityError, "failed to register variable %q: %v", name, err)
	}

	return astNode
}

// fnModifierRanks gives the one canonical order for function modifiers, matching
// the fixed order the `lambda_expr` grammar enforces for the `= <lambda>` form.
// The sugar's `fn_modifiers` rule accepts them in any order (repeat1), so the
// collector enforces order and rejects duplicates here for parity — `pure async`
// is accepted, `async pure` and `pure pure` are reported.
var fnModifierRanks = map[string]int{
	"unsafe_modifier":  0,
	"pure_modifier":    1,
	"det_modifier":     2,
	"noalloc_modifier": 3,
	"async_modifier":   4,
	"gen_modifier":     5,
	"rec_modifier":     6,
}

// applyFunctionModifiers reads a `fn_modifiers` node and lifts each modifier
// onto the function's LambdaExpr, so `let pure add(…)` yields the same AST as
// `let add = pure (…)`. `rec_modifier` is accepted for order/duplicate checking
// but, like the lambda collector, is not recorded on the AST (there is no
// IsRec field yet).
func applyFunctionModifiers(modifiersNode *sitter.Node, lambda *ast.LambdaExpr, ctx *collector_ctx.Ctx) {
	prevRank := -1
	seen := map[string]bool{}
	for i := uint(0); i < modifiersNode.NamedChildCount(); i++ {
		child := modifiersNode.NamedChild(i)
		kind := child.Kind()
		rank, ok := fnModifierRanks[kind]
		if !ok {
			continue
		}
		if seen[kind] {
			ctx.AddError(child, diag.SeverityError, "duplicate `%s` modifier", ctx.NodeText(child))
			continue
		}
		if rank < prevRank {
			ctx.AddError(child, diag.SeverityError,
				"function modifier `%s` is out of order", ctx.NodeText(child))
		}
		seen[kind] = true
		prevRank = rank

		switch kind {
		case "unsafe_modifier":
			lambda.IsUnsafe = true
		case "pure_modifier":
			lambda.IsPure = true
		case "det_modifier":
			lambda.IsDet = true
		case "noalloc_modifier":
			lambda.IsNoAlloc = true
		case "async_modifier":
			lambda.IsAsync = true
		case "gen_modifier":
			lambda.IsGenerator = true
		}
	}
}

func collectPatternDeclaration(node *sitter.Node, nameNode *sitter.Node, ctx *collector_ctx.Ctx) *ast.DestructuringDeclStmt {
	keyword := ctx.NodeText(cst.Field(node, "keyword"))
	pattern := ctx.ParseDestructuringPattern(nameNode)

	var varType types.Type
	if typeAnnotation := cst.Field(node, "type_annotation"); typeAnnotation != nil {
		varType = ctx.ParseType(cst.Field(typeAnnotation, "type"))
	}

	value := ctx.CollectExpr(cst.Field(node, "value"))

	decl := &ast.DestructuringDeclStmt{
		AstBase: ast.AstBase{Location: ctx.NodeLocation(node)},
		Keyword: keyword,
		IsMut:   cst.Field(node, "mutability") != nil,
		Pattern: pattern,
		Type:    varType,
		Value:   value,
	}

	registerDestructuredNames(node, pattern, decl, ctx)

	return decl
}

// registerDestructuredNames checks each name pattern binds for a conflict with
// a declaration that already existed in the *current* scope, then registers
// the non-conflicting ones so later references resolve to decl. All conflict
// checks run before any registration so that a name legitimately repeated
// across sub-patterns of one destructuring (e.g. two `...rest` slots) is not
// flagged against itself. Same-scope sequential rebinding is allowed for
// let/var bindings (see collectIdentifierDeclaration); a const may not be
// re-declared and a name occupied by a non-variable symbol is a conflict — in
// both cases the prior binding is kept and the name not registered.
//
// Shared by plain destructuring decls (collectPatternDeclaration) and if-let /
// let-else declarations, which call this against whichever scope is current
// at the point they invoke it (their scoping differs — see
// destructuring_if_stmt.go / destructuring_else_stmt.go).
func registerDestructuredNames(node *sitter.Node, pattern ast.Pattern, decl *ast.DestructuringDeclStmt, ctx *collector_ctx.Ctx) {
	boundNames := destructuringPatternBoundNames(pattern)
	conflicted := map[string]bool{}
	for _, name := range boundNames {
		if existing, alreadyDeclared := ctx.LookupCurrentScope(name); alreadyDeclared {
			if v, ok := existing.(*ast.VarDeclStmt); !ok || v.BindingKind == ast.BindingConst {
				ctx.AddErrorRelated(node, diag.SeverityError,
					[]diag.RelatedInformation{{Location: existing.GetLocation(), Message: "previously declared here"}},
					"%s", redeclarationMessage(existing, name))
				conflicted[name] = true
			}
		}
	}
	for _, name := range boundNames {
		if !conflicted[name] {
			ctx.RegisterDestructuredName(name, decl)
		}
	}
}

// redeclarationMessage builds the diagnostic message for an illegal re-declaration
// of name. A const may never be re-declared; any other occupied name (e.g. a
// parameter or type) reports a generic already-declared conflict.
func redeclarationMessage(existing ast.Named, name string) string {
	if v, ok := existing.(*ast.VarDeclStmt); ok && v.BindingKind == ast.BindingConst {
		return fmt.Sprintf("cannot re-declare const %s in this scope", name)
	}
	return fmt.Sprintf("%s is already declared in this scope", name)
}

// destructuringPatternBoundNames returns all variable names introduced by a
// destructuring pattern. Wildcard "_" bindings are excluded since they are
// intentional discard slots.
func destructuringPatternBoundNames(pat ast.Pattern) []string {
	if pat == nil {
		return nil
	}
	switch p := pat.(type) {
	case *ast.IdentifierPattern:
		if p.Name == "_" {
			return nil
		}
		return []string{p.Name}
	case *ast.TuplePattern:
		var names []string
		for _, el := range p.Elements {
			names = append(names, destructuringPatternBoundNames(el)...)
		}
		return names
	case *ast.ArrayPattern:
		var names []string
		for _, el := range p.Elements {
			names = append(names, destructuringPatternBoundNames(el)...)
		}
		return names
	case *ast.StructPattern:
		var names []string
		for _, f := range p.Fields {
			if f.Pattern == nil {
				// Shorthand `{a}` binds the field name itself; a rename or nested
				// pattern (`{a: b}`, `{a: [x, y]}`) carries its bindings in Pattern.
				if f.Name != "" && f.Name != "_" {
					names = append(names, f.Name)
				}
				continue
			}
			names = append(names, destructuringPatternBoundNames(f.Pattern)...)
		}
		return names
	case *ast.RestPattern:
		if p.Identifier != "" {
			return []string{p.Identifier}
		}
	}
	return nil
}
