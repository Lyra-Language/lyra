package main

import (
	"context"
	"log"

	"github.com/owenrumney/go-lsp/lsp"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/ast/symbols"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// Definition implements textDocument/definition, returning the source location
// where the symbol under the cursor is declared.
func (h *Handler) Definition(_ context.Context, params *lsp.DefinitionParams) (result []lsp.Location, retErr error) {
	defer recoverHandler("definition", &result, &retErr)

	c, ok := h.cursorAt(string(params.TextDocument.URI), params.Position)
	if !ok {
		return nil, nil
	}
	uri, analysis, source, line, col := c.uri, c.analysis, c.source, c.line, c.col

	log.Printf("definition: request at %s line=%d col=%d", uri, line, col)

	expr := findExprAtPos(analysis.program, line, col)
	loc := resolveDefinition(expr, line, col, analysis)
	if loc == nil {
		// **A type in a type position is not an expression**, so the walk above cannot
		// reach it however far it descends — `Node { … }` resolved while `(n: Node)`,
		// `-> Node` and a field's `n: Node` all did nothing, which reads as the server
		// not supporting types at all. The collector records where each type name is
		// *written* (symbols/typerefs.go), because a type value cannot carry a position
		// of its own without breaking structural equality; resolving it is then the same
		// LookupTypeFrom the struct-literal case above already uses.
		loc = resolveTypeReference(analysis, line, col)
	}
	if loc == nil {
		// **A pattern is the third supertype**, and the walk above reaches only
		// expressions — so `Keyboard(Up) => …` in a `match` arm resolved to nothing while
		// `Keyboard(k)` as a *value* resolved fine. Same shape as the type-position gap:
		// not a missing feature but a category the lookup could not see.
		loc = resolvePatternReference(analysis, line, col)
	}
	if loc == nil {
		log.Printf("definition: no definition found (expr %T)", expr)
		return nil, nil
	}

	log.Printf("definition: found at %s", loc.Pretty())
	target, ok := h.locationIn(uri, source, analysis, *loc)
	if !ok {
		return nil, nil
	}
	return []lsp.Location{target}, nil
}

// Declaration implements textDocument/declaration, which in Lyra is the same question as
// textDocument/definition.
//
// The two are distinct in a language where a name is *declared* in one place and *defined*
// in another — a C header against its translation unit, an Objective-C `@interface` against
// its `@implementation`. Lyra has no such split: a `let`, a `type`, a `trait` and an
// `extern` each introduce a name exactly once, at the place their body or signature is
// written, and there is nothing else for a client to jump to.
//
// Implemented rather than left absent because an editor's menu is static: Zed offers "Go to
// Declaration" whether or not a server answers it, so declining leaves the item there doing
// nothing — which reads as a broken feature rather than an inapplicable one. Answering with
// the definition is both correct here and what every other single-declaration language
// server does.
func (h *Handler) Declaration(ctx context.Context, params *lsp.DeclarationParams) (result []lsp.Location, retErr error) {
	return h.Definition(ctx, &lsp.DefinitionParams{
		TextDocumentPositionParams: params.TextDocumentPositionParams,
		WorkDoneProgressParams:     params.WorkDoneProgressParams,
		PartialResultParams:        params.PartialResultParams,
	})
}

// resolveTypeReference answers for a cursor sitting on a type *name* — in a parameter, a
// return type, a field, a local annotation, a generic argument, an `impl` target.
//
// Tried after the expression walk rather than before it, so nothing that already resolves
// changes: a struct literal's name is both an expression and a written type, and the
// expression answer is the one with a scope behind it.
func resolveTypeReference(analysis *docAnalysis, line, col int) *ast.Location {
	if analysis.symTable == nil {
		return nil
	}
	ref, ok := analysis.symTable.TypeRefs.At(analysis.file, line, col)
	if !ok {
		return nil
	}
	named, ok := lookupTypeOrTrait(analysis, ref)
	if !ok {
		return nil
	}
	// The name's location, not the declaration's — the same reason the identifier case
	// gives, and the same one answer to the question (namedNameLoc, rename.go).
	loc := namedNameLoc(named)
	return &loc
}

// resolvePatternReference answers for a cursor inside a **pattern**: a constructor in a
// `match` arm or a destructuring, a struct pattern's type name, or a name the pattern binds.
//
// Tried last, after the expression walk and the type index, so nothing that already resolves
// changes. That ordering also matters for a reason particular to patterns: their spans sit
// *inside* an enclosing expression's, so the expression walk always has an answer for these
// positions — the wrong one — and only a lookup that knows about patterns can tell that the
// cursor is on `Keyboard` rather than on the arm around it.
func resolvePatternReference(analysis *docAnalysis, line, col int) *ast.Location {
	pat := findPatternAtPos(analysis.program, line, col)
	if pat == nil || analysis.symTable == nil {
		return nil
	}
	switch p := pat.(type) {
	case *ast.DataPattern:
		// The constructor's own name, not the payload's: `Keyboard(Up)` with the cursor on
		// `Up` finds the *inner* pattern first, since findPatternAtPos keeps the narrowest
		// span. A constructor is registered under its own name, which is how the
		// expression-position case above resolves `Some` — so both spellings of a
		// constructor reach the same declaration.
		if cursorOnName(p.GetLocation(), p.Name, line, col) {
			// By the data type that *owns* the constructor, not by the constructor's own
			// name: `import std.tui.{ Event }` admits `Event` and not `Keyboard`, and it
			// should not have to — a module using a type's constructors has already
			// imported the type. DeclaringDataType is the same scan the typechecker uses
			// to decide which `Some` a `match` arm means.
			if decl, ok := analysis.symTable.DeclaringDataType(p.Name, p.GetLocation()); ok {
				loc := namedNameLoc(decl)
				return &loc
			}
			// **A constructor whose type the file cannot name at all.** `match m.button {
			// WheelUp => … }` never mentions `MouseButton`, and need not: the value came
			// from a field, and the typechecker resolves the arm through the *scrutinee's*
			// type rather than by looking the constructor up. Navigation has no scrutinee
			// in hand, so the scan above — which requires the owning type to resolve from
			// this file — finds nothing.
			//
			// Answered best-effort, and deliberately not by inventing a second rule about
			// which constructor a program *means*: this picks a declaration to jump to,
			// and where two types share a constructor name it may pick the other one. That
			// is a wrong jump, which a reader sees and corrects; refusing is a feature that
			// looks broken on ordinary code.
			if decl, ok := anyDeclaringDataType(analysis, p.Name); ok {
				loc := namedNameLoc(decl)
				return &loc
			}
		}
	case *ast.StructPattern:
		if p.Name != "" && cursorOnName(p.GetLocation(), p.Name, line, col) {
			if decl, ok := analysis.symTable.LookupTypeFrom(p.Name, p.GetLocation()); ok {
				loc := namedNameLoc(decl)
				return &loc
			}
		}
	case *ast.IdentifierPattern:
		// A pattern binding *is* a declaration — `m` in `Mouse(m)` is where `m` comes from
		// — so definition on it answers with itself, the same as on a `let`. Returning it
		// rather than nothing is what makes the editor's "jump to definition" land
		// somewhere instead of appearing broken on a name that is plainly a binding.
		loc := p.GetLocation()
		return &loc
	case *ast.BindingPattern:
		if cursorOnName(p.GetLocation(), p.Name, line, col) {
			loc := p.GetLocation()
			loc.EndLine, loc.EndCol = loc.StartLine, loc.StartCol+len(p.Name)
			return &loc
		}
	}
	return nil
}

// anyDeclaringDataType finds a data type owning a constructor without requiring the type to
// be nameable from the asking file. See its one caller for why that is a navigation
// concession rather than a resolution rule.
func anyDeclaringDataType(analysis *docAnalysis, ctorName string) (*ast.TypeDeclStmt, bool) {
	var best *ast.TypeDeclStmt
	for _, decl := range analysis.symTable.Types {
		dt, ok := decl.Type.(types.DataType)
		if !ok {
			continue
		}
		for _, ctor := range dt.Constructors {
			if ctor.Name != ctorName {
				continue
			}
			// Prefer a declaration in the file being edited, then settle deterministically
			// by location — a map iterates in a different order every run, and an editor
			// jumping somewhere different each time is worse than jumping somewhere wrong.
			if best == nil ||
				(sameFile(decl.GetLocation().File, analysis.file) && !sameFile(best.GetLocation().File, analysis.file)) ||
				(decl.GetLocation().File == best.GetLocation().File && decl.GetLocation().StartLine < best.GetLocation().StartLine) {
				best = decl
			}
		}
	}
	return best, best != nil
}

// lookupTypeOrTrait resolves a written name to the declaration it refers to.
//
// **Two tables, because a trait is not a type.** `where t: Shown`, `impl Shown for Point`
// and `trait Sub: Shown` all write a name in a position that looks like a type's and is
// not — traits live in `SymbolTable.Traits` and are reached by `LookupTraitFrom`. Types
// first, since a bound is the only position a trait can appear in and nothing else
// competes for the name.
func lookupTypeOrTrait(analysis *docAnalysis, ref symbols.TypeRef) (ast.Named, bool) {
	if decl, ok := analysis.symTable.LookupTypeFrom(ref.Name, ref.Loc); ok {
		return decl, true
	}
	if decl, ok := analysis.symTable.LookupTraitFrom(ref.Name, ref.Loc); ok {
		return decl, true
	}
	return nil, false
}

// locationIn builds the lsp.Location for a definition, which since the server resolves
// a document's whole import graph may live in another file — `Some` is declared in the
// prelude, not in the buffer the user is looking at. Reporting it against the current
// URI would jump to those line and column numbers *in the open document*, so the
// target's own file has to supply both the URI and the source the columns are measured
// against. It reports false when that file cannot be read, since a location that cannot
// be converted is better dropped than sent to the wrong place.
func (h *Handler) locationIn(uri, source string, analysis *docAnalysis, loc ast.Location) (lsp.Location, bool) {
	if loc.File == "" || sameFile(loc.File, analysis.file) {
		return astLocToLSPLocation(uri, source, loc), true
	}
	targetURI, targetSource, ok := h.sourceOf(loc.File)
	if !ok {
		log.Printf("definition: cannot read %s", loc.File)
		return lsp.Location{}, false
	}
	return astLocToLSPLocation(targetURI, targetSource, loc), true
}

// resolveDefinition maps the expression at the cursor to its definition location.
// Returns nil when no definition can be found.
func resolveDefinition(expr ast.Expression, line, col int, analysis *docAnalysis) *ast.Location {
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
		// The *name's* location, not the declaration's: `namedNameLoc` (rename.go) is
		// already the one answer to that question, and using it here rather than a
		// second copy is what makes an `extern` — whose declaration starts at an
		// `@link` or `unsafe` token several lines above its name — land on the name
		// like every other declaration does.
		loc := namedNameLoc(named)
		return &loc

	case *ast.StructInstanceExpr:
		// Cursor is on the struct type name (the name occupies the start of the expression).
		if cursorOnName(e.GetLocation(), e.Name, line, col) {
			if decl, ok := analysis.symTable.LookupTypeFrom(e.Name, e.GetLocation()); ok {
				// namedNameLoc, not GetLocation: these two arms landed on the
				// *declaration's* first token while every other path landed on its name,
				// so `Point` in `Point { … }` and `Point` in `(p: Point)` jumped to
				// different columns of the same line. Invisible until types resolved at
				// all, since there was nothing to be inconsistent with.
				loc := namedNameLoc(decl)
				return &loc
			}
		}

	case *ast.DataConstructorExpr:
		// Cursor is on a data-type constructor name (e.g. Some, None, Ok, Err).
		if cursorOnName(e.GetLocation(), e.Constructor, line, col) {
			if decl, ok := analysis.symTable.LookupTypeFrom(e.Constructor, e.GetLocation()); ok {
				loc := namedNameLoc(decl)
				return &loc
			}
		}
	}
	return nil
}

// cursorOnName reports whether (line, col) falls on the name string that begins
// at loc.StartLine/StartCol. Both are 1-based.
func cursorOnName(loc ast.Location, name string, line, col int) bool {
	return line == loc.StartLine && col >= loc.StartCol && col < loc.StartCol+len(name)
}

// findScopeAtPos returns the innermost scope whose block contains (line, col).
//
// Falls back to fileScope when no nested block matches — a top-level position. That is
// the scope of the module the document belongs to (SymbolTable.EntryScope for the
// single file the LSP analyzes), *not* the global scope: a file's own top-level
// declarations live in its module scope, and the chain out from there reaches the
// prelude's names and other modules' exports in the order the language resolves them.
// findScopeAtPos is the innermost scope covering a position: the one a name at that point
// resolves in.
//
// **A walk, not a descent.** It was three hand-written switches — scopeInNode, scopeInExpr
// and nodeToExpr, the last mapping a statement to the expression inside it — and nodeToExpr
// knew three statement kinds out of a dozen. So the descent stopped at the first statement
// it did not recognise and answered with whatever enclosing scope it had reached: a `match`
// nested inside another match's arm was unreachable, and every name bound in the inner arm
// resolved in the outer block instead. The symptom was a feature that worked in a small file
// and not in a real one, which is the worst shape for a bug like this to have.
//
// Now it walks with `ast.WalkStmt`/`ast.WalkExpr` — the canonical children — and keeps the
// **narrowest** recorded scope whose node contains the position. Same rule findExprAtPos
// uses, and it retires three mirrors (CLAUDE.md rule 8: a registered mirror is second best,
// retiring one is the real fix).
func findScopeAtPos(program *ast.Program, scopeTable *symbols.ScopeTable, fileScope *symbols.Scope, line, col int) *symbols.Scope {
	if program == nil || scopeTable == nil {
		return fileScope
	}
	best := fileScope
	var bestLoc *ast.Location

	consider := func(node ast.AstNode, loc ast.Location) {
		sc, ok := scopeTable.Get(node)
		if !ok || !containsPos(loc, line, col) {
			return
		}
		if bestLoc == nil || spanWithin(loc, *bestLoc) {
			best, bestLoc = sc, &loc
		}
	}

	for _, node := range program.Statements {
		stmt, ok := node.(ast.Statement)
		if !ok {
			continue
		}
		ast.WalkStmt(stmt, func(s ast.Statement) bool {
			consider(s, s.GetLocation())
			return true
		}, func(e ast.Expression) bool {
			consider(e, e.GetLocation())
			// **A match arm's scope is recorded against its pattern** (see
			// CollectMatchArm), whose span covers only the pattern — but the scope
			// governs the guard and the body too. So the arm is offered under its whole
			// extent rather than under the pattern's, which is what lets a name bound by
			// the pattern resolve at a *use* in the body.
			if me, ok := e.(*ast.MatchExpr); ok {
				for _, arm := range me.MatchArms {
					if arm.Pattern == nil || arm.Body == nil {
						continue
					}
					extent := arm.Pattern.GetLocation()
					end := arm.Body.GetLocation()
					extent.EndLine, extent.EndCol = end.EndLine, end.EndCol
					consider(arm.Pattern, extent)
				}
			}
			return true
		})
	}
	return best
}

// astLocToLSPLocation converts a 1-based, byte-based ast.Location to an
// lsp.Location, using source to translate byte columns to UTF-16.
func astLocToLSPLocation(uri string, source string, loc ast.Location) lsp.Location {
	return lsp.Location{
		URI:   lsp.DocumentURI(uri),
		Range: locToRange(source, loc),
	}
}
