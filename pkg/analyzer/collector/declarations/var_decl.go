package declarations

import (
	"fmt"

	"github.com/Lyra-Language/lyra/pkg/analyzer/collector/collector_ctx"
	"github.com/Lyra-Language/lyra/pkg/ast"
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
	if nameNode := node.ChildByFieldName("name"); nameNode != nil {
		return collectIdentifierDeclaration(node, nameNode, ctx)
	}
	if patternNode := node.ChildByFieldName("pattern"); patternNode != nil {
		return collectPatternDeclaration(node, patternNode, ctx)
	}
	ctx.AddError(node, diag.SeverityError, "declaration missing both name and pattern fields")
	return nil
}

func collectIdentifierDeclaration(node *sitter.Node, nameNode *sitter.Node, ctx *collector_ctx.Ctx) *ast.VarDeclStmt {
	kind := bindingKind(ctx.NodeText(node.ChildByFieldName("keyword")), ctx)
	name := ctx.NodeText(nameNode)

	genericParametersNode := node.ChildByFieldName("generic_parameters")
	genericParameters := []ast.GenericParam{}
	if genericParametersNode != nil {
		genericParameters = ctx.CollectGenericParams(genericParametersNode)
	}
	if whereNode := node.ChildByFieldName("generic_parameter_constraints"); whereNode != nil {
		genericParameters = ctx.MergeWhereConstraints(genericParameters, whereNode)
	}

	var varType types.Type
	if typeAnnotation := node.ChildByFieldName("type_annotation"); typeAnnotation != nil {
		varType = ctx.ParseType(typeAnnotation.ChildByFieldName("type"))
	}

	valueNode := node.ChildByFieldName("value")
	var initExpr ast.Expression
	if valueNode != nil {
		initExpr = ctx.CollectExpr(valueNode)
	}

	astNode := &ast.VarDeclStmt{
		AstBase:       ast.AstBase{Location: ctx.NodeLocation(node)},
		BindingKind:   kind,
		Name:          name,
		GenericParams: genericParameters,
		Type:          varType,
		Value:         initExpr,
	}

	if existing, alreadyDeclared := ctx.LookupCurrentScope(name); alreadyDeclared {
		if v, ok := existing.(*ast.VarDeclStmt); ok && v.BindingKind != ast.BindingConst {
			// Same-scope sequential rebinding (e.g. `let x = parse(x)`) is
			// idiomatic and safe in ML-family languages, so it is allowed for
			// let/var bindings. Replace the prior binding so later references
			// resolve to this declaration. Genuinely confusing nested-scope
			// shadowing is still reported separately by CheckShadowing.
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

func collectPatternDeclaration(node *sitter.Node, nameNode *sitter.Node, ctx *collector_ctx.Ctx) *ast.DestructuringDeclStmt {
	keyword := ctx.NodeText(node.ChildByFieldName("keyword"))
	pattern := ctx.ParseDestructuringPattern(nameNode)

	var varType types.Type
	if typeAnnotation := node.ChildByFieldName("type_annotation"); typeAnnotation != nil {
		varType = ctx.ParseType(typeAnnotation.ChildByFieldName("type"))
	}

	value := ctx.CollectExpr(node.ChildByFieldName("value"))

	// Check each pattern-bound name for conflicts with existing declarations in
	// the current scope. Same-scope sequential rebinding is allowed for let/var
	// bindings (see collectIdentifierDeclaration); only a const may not be
	// re-declared, and a name occupied by a non-variable symbol is still a
	// conflict.
	for _, name := range destructuringPatternBoundNames(pattern) {
		existing, alreadyDeclared := ctx.LookupCurrentScope(name)
		if !alreadyDeclared {
			continue
		}
		if v, ok := existing.(*ast.VarDeclStmt); ok && v.BindingKind != ast.BindingConst {
			continue
		}
		ctx.AddErrorRelated(node, diag.SeverityError,
			[]diag.RelatedInformation{{Location: existing.GetLocation(), Message: "previously declared here"}},
			"%s", redeclarationMessage(existing, name))
	}

	return &ast.DestructuringDeclStmt{
		AstBase: ast.AstBase{Location: ctx.NodeLocation(node)},
		Keyword: keyword,
		Pattern: pattern,
		Type:    varType,
		Value:   value,
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
