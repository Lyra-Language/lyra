package main

import (
	"github.com/owenrumney/go-lsp/lsp"
	"strings"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// resolveDoc returns the documentation for whatever the cursor is on, or nil.
//
// It mirrors resolveDefinition case for case, and deliberately so: "which declaration
// does this expression name?" already has an answer in this package, and a second walk
// that answers it differently is how hover comes to show one symbol's docs above another
// symbol's type. Where the two differ is only in what they take from the declaration
// once found — a location there, a Doc here.
func resolveDoc(expr ast.Expression, line, col int, analysis *docAnalysis) *ast.Doc {
	if expr == nil {
		return nil
	}
	switch e := expr.(type) {
	case *ast.IdentifierExpr:
		scope := findScopeAtPos(analysis.program, analysis.scopeTable, analysis.fileScope(), line, col)
		named, ok := scope.Lookup(e.Name)
		if !ok {
			return nil
		}
		return docOf(named)

	case *ast.StructInstanceExpr:
		if cursorOnName(e.GetLocation(), e.Name, line, col) {
			if decl, ok := analysis.symTable.LookupTypeFrom(e.Name, e.GetLocation()); ok {
				return decl.Doc
			}
		}

	case *ast.DataConstructorExpr:
		if cursorOnName(e.GetLocation(), e.Constructor, line, col) {
			// A constructor's own doc first, the type's as the fallback: the
			// cursor is on `Some`, so what `Some` means beats what `Maybe`
			// means — and an undocumented constructor of a documented type
			// should still say something rather than nothing.
			if decl, ok := analysis.symTable.LookupTypeFrom(e.Constructor, e.GetLocation()); ok {
				if d := decl.MemberDoc(e.Constructor); d != nil {
					return d
				}
				return decl.Doc
			}
		}

	case *ast.MemberExpr:
		return memberDoc(e, analysis)
	}
	return nil
}

// memberDoc resolves a field access (`pt.x`) to the field's doc on the struct
// declaration that names it.
//
// The receiver's type comes from the type table rather than from a lookup on the
// receiver's spelling, because the receiver is an arbitrary expression — `mk().x` has no
// name to look up. types.HeadName gives the declaration's name for a plain struct and for
// a generic instantiation alike, so `Box<i64>` finds `Box`.
func memberDoc(e *ast.MemberExpr, analysis *docAnalysis) *ast.Doc {
	recvType, ok := analysis.typeTable.Get(e.Object)
	if !ok || recvType == nil {
		return nil
	}
	head, ok := types.HeadName(recvType)
	if !ok {
		return nil
	}
	decl, ok := analysis.symTable.LookupTypeFrom(head, e.GetLocation())
	if !ok {
		return nil
	}
	return decl.MemberDoc(e.Property.Name)
}

// docOf pulls the Doc off whichever declaration kind a scope lookup returned. A scope
// holds ast.Named, which is every declaration and also things that carry no docs at all
// (a parameter, a destructured name) — those simply answer nil.
func docOf(named ast.Named) *ast.Doc {
	switch d := named.(type) {
	case *ast.VarDeclStmt:
		return d.Doc
	case *ast.TypeDeclStmt:
		return d.Doc
	case *ast.TraitDeclStmt:
		return d.Doc
	case *ast.ExternDeclStmt:
		return d.Doc
	}
	return nil
}

// renderHover assembles the hover body: the signature block, then the documentation
// under a rule.
//
// The type goes **first**. Hover is read at a glance to answer "what is this", and the
// type answers that in one line where prose may take a paragraph; a long doc comment
// above the signature pushes the signature out of view in a popup of any fixed height.
func renderHover(signature string, doc *ast.Doc) string {
	if doc == nil {
		return signature
	}
	var b strings.Builder
	b.WriteString(signature)
	b.WriteString("\n\n---\n\n")
	b.WriteString(doc.Text)
	return b.String()
}

// hoverPatternReference renders a constructor named in a pattern: which data type it
// belongs to, and that type's documentation.
//
// It answers for the constructor and not for a **binding**: `m` in `Mouse(m)` has a type
// the typechecker knows and nothing records, since the TypeTable is keyed by *expression*
// and a pattern is not one. Showing a name with no type would be worse than showing
// nothing — hover is read as "here is what this is" — so the binding case is left to the
// definition jump, which does answer for it.
func hoverPatternReference(analysis *docAnalysis, line, col int) *lsp.Hover {
	pat := findPatternAtPos(analysis.program, line, col)
	if pat == nil || analysis.symTable == nil {
		return nil
	}
	name := ""
	switch p := pat.(type) {
	case *ast.DataPattern:
		name = p.Name
	case *ast.StructPattern:
		name = p.Name
	default:
		return nil
	}
	if name == "" || !cursorOnName(pat.GetLocation(), name, line, col) {
		return nil
	}
	decl, ok := analysis.symTable.DeclaringDataType(name, pat.GetLocation())
	if !ok {
		if decl, ok = anyDeclaringDataType(analysis, name); !ok {
			// A struct pattern names its own type rather than a constructor.
			if decl, ok = analysis.symTable.LookupTypeFrom(name, pat.GetLocation()); !ok {
				return nil
			}
		}
	}
	summary := name + ": " + decl.Name
	content := renderHover("```lyra\n"+summary+"\n```", decl.Doc)
	return &lsp.Hover{Contents: lsp.MarkupContent{Kind: lsp.Markdown, Value: content}}
}
