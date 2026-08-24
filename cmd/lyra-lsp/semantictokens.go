package main

import (
	"context"
	"log"
	"sort"

	"github.com/owenrumney/go-lsp/lsp"

	"github.com/Lyra-Language/lyra/pkg/ast"
)

// Semantic token type indices. The order must match semanticTokenTypes, which
// is advertised to the client as the legend in Initialize.
const (
	semTypeVariable = iota
	semTypeParameter
	semTypeFunction
	semTypeType
	semTypeEnumMember
	semTypeProperty
)

// semanticTokenTypes is the legend the client uses to decode the tokenType field
// of each encoded token.
var semanticTokenTypes = []string{
	"variable",
	"parameter",
	"function",
	"type",
	"enumMember",
	"property",
}

// semanticTokenModifiers is the legend for the modifier bitset. "readonly"
// marks bindings that can't be changed (a `const` or a deeply-immutable `let`),
// letting the editor distinguish constants from mutable variables.
var semanticTokenModifiers = []string{"readonly"}

const semModReadonly = 1 << 0 // index of "readonly" in semanticTokenModifiers

// semToken is one classified source span, in 0-based LSP coordinates, before
// delta-encoding.
type semToken struct {
	line      int
	startChar int
	length    int
	tokenType int
	modifiers int
}

// SemanticTokensFull implements textDocument/semanticTokens/full. It classifies
// every identifier usage, member-access property, data constructor, and struct
// type name in the document into a token type the TextMate grammar can't infer
// (e.g. constant vs mutable variable, function vs variable, type name). The
// go-lsp library registers this capability automatically via the
// SemanticTokensFullHandler interface; Initialize advertises the legend.
func (h *Handler) SemanticTokensFull(_ context.Context, params *lsp.SemanticTokensParams) (result *lsp.SemanticTokens, retErr error) {
	defer recoverHandler("semanticTokens", &result, &retErr)

	uri := string(params.TextDocument.URI)
	analysis, source, ok := h.docFor(uri)
	if !ok {
		return nil, nil
	}

	tokens := collectSemanticTokens(source, analysis)
	log.Printf("semanticTokens: %s → %d tokens", uri, len(tokens))
	return &lsp.SemanticTokens{Data: encodeSemanticTokens(tokens)}, nil
}

// collectSemanticTokens walks the program and classifies each highlightable
// span. Spans are deduplicated by start location so a position is never emitted
// twice (overlapping tokens are invalid in the LSP encoding).
func collectSemanticTokens(source string, analysis *docAnalysis) []semToken {
	var tokens []semToken
	seen := map[ast.Location]bool{}

	add := func(loc ast.Location, name string, tokenType, modifiers int) {
		if name == "" || loc.StartLine <= 0 || loc.StartCol <= 0 || seen[loc] {
			return
		}
		seen[loc] = true
		// LSP semantic tokens encode both the start character and the length in
		// UTF-16 code units.
		tokens = append(tokens, semToken{
			line:      lspPos(loc.StartLine),
			startChar: utf16Column(source, lspPos(loc.StartLine), lspPos(loc.StartCol)),
			length:    utf16Len16(name),
			tokenType: tokenType,
			modifiers: modifiers,
		})
	}

	// nameLoc returns the span covering just the leading `name` of expr, used for
	// constructors and struct literals whose location spans the whole expression.
	nameLoc := func(loc ast.Location, name string) ast.Location {
		return ast.Location{
			StartLine: loc.StartLine,
			StartCol:  loc.StartCol,
			EndLine:   loc.StartLine,
			EndCol:    loc.StartCol + len(name),
		}
	}

	// onExpr tokenizes usages (and parameter declarations carried on lambdas).
	onExpr := func(e ast.Expression) bool {
		switch ex := e.(type) {
		case *ast.IdentifierExpr:
			if tt, mods, ok := classifyIdentifier(ex, analysis); ok {
				add(ex.GetLocation(), ex.Name, tt, mods)
			}
		case *ast.MemberExpr:
			// The object is walked separately; only the property name is ours.
			add(ex.Property.GetLocation(), ex.Property.Name, semTypeProperty, 0)
		case *ast.DataConstructorExpr:
			add(nameLoc(ex.GetLocation(), ex.Constructor), ex.Constructor, semTypeEnumMember, 0)
		case *ast.StructInstanceExpr:
			add(nameLoc(ex.GetLocation(), ex.Name), ex.Name, semTypeType, 0)
		case *ast.LambdaExpr:
			for i := range ex.Parameters {
				if p, ok := ex.Parameters[i].Pattern.(*ast.IdentifierPattern); ok {
					add(p.GetLocation(), p.Name, semTypeParameter, 0)
				}
			}
		}
		return true
	}

	// onStmt tokenizes declaration names, which are the most prominent
	// identifiers and would otherwise fall back to the TextMate grammar.
	onStmt := func(s ast.Statement) bool {
		switch decl := s.(type) {
		case *ast.VarDeclStmt:
			tt, mods := classifyVarDecl(decl)
			add(decl.NameLocation, decl.Name, tt, mods)
		case *ast.TypeDeclStmt:
			add(decl.NameLocation, decl.Name, semTypeType, 0)
		case *ast.TraitDeclStmt:
			add(decl.NameLocation, decl.Name, semTypeType, 0)
		case *ast.ExternDeclStmt:
			// A foreign function highlights as a function. Two switches in this file
			// needed it, not one: this colours the *declaration's* name, and
			// classifyIdentifier below colours every reference to it.
			add(decl.NameLocation, decl.Name, semTypeFunction, 0)
		}
		return true
	}

	for _, node := range analysis.program.Statements {
		if stmt, ok := node.(ast.Statement); ok {
			ast.WalkStmt(stmt, onStmt, onExpr)
		}
	}

	return tokens
}

// classifyIdentifier resolves an identifier usage to its token type and
// modifiers via the lexical scope. It returns false when the name is unbound
// (e.g. a builtin), leaving such spans to the TextMate grammar.
func classifyIdentifier(ident *ast.IdentifierExpr, analysis *docAnalysis) (int, int, bool) {
	scope := findScopeAtPos(analysis.program, analysis.scopeTable, analysis.fileScope(), ident.Location.StartLine, ident.Location.StartCol)
	named, ok := scope.Lookup(ident.Name)
	if !ok {
		if ident.IsConst {
			return semTypeVariable, semModReadonly, true
		}
		return 0, 0, false
	}

	switch n := named.(type) {
	case *ast.VarDeclStmt:
		tt, mods := classifyVarDecl(n)
		return tt, mods, true
	case *ast.Parameter:
		return semTypeParameter, 0, true
	case *ast.TypeDeclStmt, *ast.TraitDeclStmt:
		return semTypeType, 0, true
	case *ast.ExternDeclStmt:
		return semTypeFunction, 0, true
	}
	return semTypeVariable, 0, true
}

// classifyVarDecl maps a binding to its token type and modifiers: a `const` or
// a deeply-immutable `let` is a readonly variable, a lambda-valued binding is a
// function, and a `var`/`let mut` is a plain (mutable) variable.
func classifyVarDecl(n *ast.VarDeclStmt) (int, int) {
	if n.IsConstant() {
		return semTypeVariable, semModReadonly
	}
	if _, isLambda := n.Value.(*ast.LambdaExpr); isLambda {
		return semTypeFunction, 0
	}
	if !n.CanMutateInterior() {
		return semTypeVariable, semModReadonly
	}
	return semTypeVariable, 0
}

// encodeSemanticTokens sorts the tokens by position and delta-encodes them into
// the flat [deltaLine, deltaStart, length, tokenType, modifiers] integer array
// the LSP protocol expects. The returned slice is non-nil (possibly empty) so it
// marshals to a JSON array rather than null.
func encodeSemanticTokens(tokens []semToken) []int {
	sort.Slice(tokens, func(i, j int) bool {
		if tokens[i].line != tokens[j].line {
			return tokens[i].line < tokens[j].line
		}
		return tokens[i].startChar < tokens[j].startChar
	})

	data := make([]int, 0, len(tokens)*5)
	prevLine, prevChar := 0, 0
	for _, t := range tokens {
		deltaLine := t.line - prevLine
		deltaChar := t.startChar
		if deltaLine == 0 {
			deltaChar = t.startChar - prevChar
		}
		data = append(data, deltaLine, deltaChar, t.length, t.tokenType, t.modifiers)
		prevLine, prevChar = t.line, t.startChar
	}
	return data
}
