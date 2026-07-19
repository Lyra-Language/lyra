package main

import (
	"context"
	"log"
	"runtime/debug"
	"sort"
	"strings"

	"github.com/owenrumney/go-lsp/lsp"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/ast/symbols"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// Completion implements textDocument/completion. It offers two flavours of
// suggestion depending on the text immediately before the cursor:
//
//   - after a `.` (e.g. `p.|`) it lists the field names of the receiver's
//     struct type;
//   - otherwise it lists every identifier in scope (variables, parameters,
//     functions) plus all declared type names.
//
// The go-lsp library registers this capability automatically via the
// CompletionHandler interface; Initialize additionally advertises `.` as a
// trigger character so member completion fires as soon as the dot is typed.
func (h *Handler) Completion(_ context.Context, params *lsp.CompletionParams) (result *lsp.CompletionList, retErr error) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("completion panic: %v\n%s", r, debug.Stack())
			result, retErr = nil, nil
		}
	}()

	uri := string(params.TextDocument.URI)
	h.mu.Lock()
	analysis, ok := h.analysisStore[uri]
	source := h.docStore[uri]
	h.mu.Unlock()
	if !ok {
		return nil, nil
	}

	// LSP positions are 0-based UTF-16; ast.Location is 1-based bytes.
	line := params.Position.Line + 1
	col := byteColumn(source, params.Position.Line, params.Position.Character)

	// Text on the cursor's line up to the caret, used to detect a `receiver.`
	// member-access prefix the broken/partial parse can't be relied upon for.
	lineStart := posToOffset(source, params.Position.Line, 0)
	cursor := posToOffset(source, params.Position.Line, params.Position.Character)
	prefix := source[lineStart:cursor]

	scope := findScopeAtPos(analysis.program, analysis.scopeTable, analysis.symTable.GlobalScope, line, col)

	var items []lsp.CompletionItem
	if receiver, isMember := memberReceiver(prefix); isMember {
		items = memberCompletions(receiver, scope, analysis)
	} else {
		items = scopeCompletions(scope, analysis)
	}

	sort.Slice(items, func(i, j int) bool { return items[i].Label < items[j].Label })
	return &lsp.CompletionList{IsIncomplete: false, Items: items}, nil
}

// memberReceiver inspects the line text before the cursor. If the caret sits in
// a member-access position (`receiver.partial`), it returns the receiver chain
// (e.g. `p` or `a.b`) and true; otherwise it returns false.
func memberReceiver(prefix string) (string, bool) {
	// Skip back over the partial field name being typed.
	i := len(prefix)
	for i > 0 && isIdentChar(prefix[i-1]) {
		i--
	}
	if i == 0 || prefix[i-1] != '.' {
		return "", false
	}
	dot := i - 1
	// Walk back over the dotted receiver chain (identifiers joined by dots).
	k := dot
	for k > 0 && (isIdentChar(prefix[k-1]) || prefix[k-1] == '.') {
		k--
	}
	receiver := prefix[k:dot]
	if receiver == "" {
		return "", false
	}
	return receiver, true
}

func isIdentChar(b byte) bool {
	return b == '_' ||
		(b >= 'a' && b <= 'z') ||
		(b >= 'A' && b <= 'Z') ||
		(b >= '0' && b <= '9')
}

// memberCompletions resolves the receiver chain to a struct type and returns a
// completion item for each of its fields.
func memberCompletions(receiver string, scope *symbols.Scope, analysis *docAnalysis) []lsp.CompletionItem {
	t := resolveReceiverType(receiver, scope, analysis)
	if t == nil {
		return nil
	}
	fields, ok := structFieldsOf(t, analysis.symTable)
	if !ok {
		return nil
	}
	kind := lsp.CompletionItemKindField
	items := make([]lsp.CompletionItem, 0, len(fields))
	for _, f := range fields {
		k := kind
		items = append(items, lsp.CompletionItem{
			Label:  f.Name,
			Kind:   &k,
			Detail: typeDetail(f.Type),
		})
	}
	return items
}

// resolveReceiverType walks a dotted receiver chain (`a.b.c`) to the type of its
// final segment: the first segment is resolved through the lexical scope, and
// each subsequent segment is looked up as a struct field of the prior type.
func resolveReceiverType(receiver string, scope *symbols.Scope, analysis *docAnalysis) types.Type {
	parts := strings.Split(receiver, ".")
	named, ok := scope.Lookup(parts[0])
	if !ok {
		return nil
	}
	t := bindingType(named, analysis)
	for _, name := range parts[1:] {
		if t == nil {
			return nil
		}
		fields, ok := structFieldsOf(t, analysis.symTable)
		if !ok {
			return nil
		}
		t = fieldType(fields, name)
	}
	return t
}

// bindingType returns the declared or inferred type of a named binding.
func bindingType(named ast.Named, analysis *docAnalysis) types.Type {
	switch n := named.(type) {
	case *ast.VarDeclStmt:
		if n.Type != nil {
			return n.Type
		}
		if n.Value != nil {
			if t, ok := analysis.typeTable.Get(n.Value); ok {
				return t
			}
		}
	case *ast.Parameter:
		return n.Type
	}
	return nil
}

// structFieldsOf returns the fields of a struct type, following a named or
// unresolved type reference through the symbol table to its declaration.
func structFieldsOf(t types.Type, symTable *symbols.SymbolTable) ([]types.StructField, bool) {
	switch s := t.(type) {
	case types.AnonymousStructType:
		return s.Fields, true
	case types.NamedStructType:
		if decl, ok := symTable.Types[s.Name]; ok {
			if ns, ok := decl.Type.(types.NamedStructType); ok {
				return ns.Fields, true
			}
		}
		return s.Fields, true
	case types.UnresolvedType:
		if decl, ok := symTable.Types[s.Name]; ok {
			return structFieldsOf(decl.Type, symTable)
		}
	}
	return nil, false
}

// fieldType returns the type of the named field, or nil when absent.
func fieldType(fields []types.StructField, name string) types.Type {
	for _, f := range fields {
		if f.Name == name {
			return f.Type
		}
	}
	return nil
}

// scopeCompletions returns every identifier reachable from scope (inner
// bindings shadow same-named outer ones) followed by all declared type names.
func scopeCompletions(scope *symbols.Scope, analysis *docAnalysis) []lsp.CompletionItem {
	seen := map[string]bool{}
	var items []lsp.CompletionItem

	for s := scope; s != nil; s = s.Parent {
		for name, named := range s.Symbols {
			if seen[name] {
				continue
			}
			seen[name] = true
			kind := namedCompletionKind(named)
			items = append(items, lsp.CompletionItem{
				Label:  name,
				Kind:   &kind,
				Detail: bindingDetail(named, analysis),
			})
		}
	}

	for name, decl := range analysis.symTable.Types {
		if seen[name] {
			continue
		}
		seen[name] = true
		kind := typeDeclCompletionKind(decl.Type)
		items = append(items, lsp.CompletionItem{Label: name, Kind: &kind})
	}

	return items
}

// namedCompletionKind classifies an in-scope binding for its editor icon. Type
// and trait declarations also live in the global scope, so they are handled
// here too.
func namedCompletionKind(named ast.Named) lsp.CompletionItemKind {
	switch n := named.(type) {
	case *ast.VarDeclStmt:
		if n.BindingKind == ast.BindingConst {
			return lsp.CompletionItemKindConstant
		}
		if _, isLambda := n.Value.(*ast.LambdaExpr); isLambda {
			return lsp.CompletionItemKindFunction
		}
	case *ast.TypeDeclStmt:
		return typeDeclCompletionKind(n.Type)
	case *ast.TraitDeclStmt:
		return lsp.CompletionItemKindInterface
	}
	return lsp.CompletionItemKindVariable
}

// typeDeclCompletionKind maps a type declaration's underlying type to an icon.
func typeDeclCompletionKind(t types.Type) lsp.CompletionItemKind {
	switch t.(type) {
	case types.NamedStructType, types.AnonymousStructType:
		return lsp.CompletionItemKindStruct
	case types.DataType:
		return lsp.CompletionItemKindEnum
	default:
		return lsp.CompletionItemKindClass
	}
}

// bindingDetail returns the binding's type as a detail string, when known.
func bindingDetail(named ast.Named, analysis *docAnalysis) string {
	return typeDetail(bindingType(named, analysis))
}

// typeDetail renders a type for the completion item's detail column.
func typeDetail(t types.Type) string {
	if t == nil {
		return ""
	}
	return t.String()
}
