package main

import (
	"context"

	"github.com/owenrumney/go-lsp/lsp"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// DocumentSymbol implements textDocument/documentSymbol, returning the symbol
// outline (type decls, functions, constants) for the breadcrumb/outline view.
func (h *Handler) DocumentSymbol(_ context.Context, params *lsp.DocumentSymbolParams) (result []lsp.DocumentSymbol, retErr error) {
	defer recoverHandler("documentSymbol", &result, &retErr)

	uri := string(params.TextDocument.URI)
	analysis, source, ok := h.docFor(uri)
	if !ok {
		return nil, nil
	}

	var syms []lsp.DocumentSymbol
	for _, node := range analysis.program.Statements {
		if sym := stmtToSymbol(source, node); sym != nil {
			syms = append(syms, *sym)
		}
	}
	return syms, nil
}

// declSymbol is what a declaration contributes to either symbol list: its name, the kind an
// editor should show it as, the span of the whole declaration, and the span of its **name**.
//
// The two consumers want different things from it and used to derive them from two parallel
// switches — the same four cases, the same `Name == ""` guards, the same kind derivation,
// and the same reason for the extern special case, written twice. `pkg/ast`'s
// exhaustiveness test registered both, so a fifth declaration kind failed twice; it now
// fails once, at the one switch.
type declSymbol struct {
	name string
	kind lsp.SymbolKind
	// span covers the whole declaration; nameLoc covers just the name.
	//
	// They differ for exactly one kind. An extern's span begins at its `@link` or
	// `unsafe` token, so measuring the name from there highlights the wrong text — and
	// jumping to it lands above the symbol that matched. Everything else starts at its
	// own name, which is why the other three can use one location for both and why
	// writing the extern like its neighbours looks right and is not.
	span    ast.Location
	nameLoc ast.Location
}

// symbolOf answers for the declaration kinds a symbol list shows, and reports false for
// anything else — including a declaration with no name, which is a partial parse rather
// than a symbol.
func symbolOf(node ast.AstNode) (declSymbol, bool) {
	switch s := node.(type) {
	case *ast.TypeDeclStmt:
		if s.Name == "" {
			return declSymbol{}, false
		}
		loc := s.GetLocation()
		return declSymbol{name: s.Name, kind: typeDeclKind(s.Type), span: loc, nameLoc: loc}, true

	case *ast.TraitDeclStmt:
		if s.Name == "" {
			return declSymbol{}, false
		}
		loc := s.GetLocation()
		return declSymbol{name: s.Name, kind: lsp.SymbolKindInterface, span: loc, nameLoc: loc}, true

	case *ast.VarDeclStmt:
		if s.Name == "" {
			return declSymbol{}, false
		}
		loc := s.GetLocation()
		return declSymbol{name: s.Name, kind: varDeclSymbolKind(s), span: loc, nameLoc: loc}, true

	case *ast.ExternDeclStmt:
		if s.Name == "" {
			return declSymbol{}, false
		}
		// A foreign function is a function in an outline and in a search. Its name comes
		// from NameLocation — see declSymbol.
		return declSymbol{
			name:    s.Name,
			kind:    lsp.SymbolKindFunction,
			span:    s.GetLocation(),
			nameLoc: s.NameLocation,
		}, true
	}
	return declSymbol{}, false
}

// stmtToSymbol is one entry in the document outline: the declaration's whole span, with the
// name as the selection range an editor highlights.
func stmtToSymbol(source string, node ast.AstNode) *lsp.DocumentSymbol {
	d, ok := symbolOf(node)
	if !ok {
		return nil
	}
	return &lsp.DocumentSymbol{
		Name:           d.name,
		Kind:           d.kind,
		Range:          locToRange(source, d.span),
		SelectionRange: nameRange(source, d.nameLoc, d.name),
	}
}

// typeDeclKind maps a type declaration's underlying type to an LSP SymbolKind.
func typeDeclKind(t types.Type) lsp.SymbolKind {
	switch t.(type) {
	case types.NamedStructType, types.AnonymousStructType:
		return lsp.SymbolKindStruct
	case types.DataType:
		return lsp.SymbolKindEnum
	default:
		return lsp.SymbolKindClass
	}
}

// varDeclSymbolKind returns Function for lambda-valued bindings, Constant for
// const declarations, and Variable for everything else.
func varDeclSymbolKind(s *ast.VarDeclStmt) lsp.SymbolKind {
	if s.BindingKind == ast.BindingConst {
		return lsp.SymbolKindConstant
	}
	if _, isLambda := s.Value.(*ast.LambdaExpr); isLambda {
		return lsp.SymbolKindFunction
	}
	return lsp.SymbolKindVariable
}

// nameRange returns a single-line range covering just the symbol name, starting
// at the declaration's StartLine/StartCol. SelectionRange should highlight only
// the name token, not the whole declaration. The name span is measured in bytes
// (len(name)) then converted to UTF-16 columns via source.
func nameRange(source string, loc ast.Location, name string) lsp.Range {
	startByteCol := lspPos(loc.StartCol)
	return lsp.Range{
		Start: lsp.Position{Line: lspPos(loc.StartLine), Character: utf16Column(source, lspPos(loc.StartLine), startByteCol)},
		End:   lsp.Position{Line: lspPos(loc.StartLine), Character: utf16Column(source, lspPos(loc.StartLine), startByteCol+len(name))},
	}
}
