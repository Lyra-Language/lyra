// Package docgen turns an analyzed program into per-module documentation.
//
// It is deliberately split in two: [Collect] builds a *model* from the AST and symbol
// table, and [RenderMarkdown] turns one module of that model into a page. Nothing about
// Markdown reaches the model, so a second renderer — a terminal `go doc` view, a JSON
// dump for another tool — is a new function beside the existing one rather than a second
// traversal of the AST that can disagree with this one about what a module contains.
package docgen

import (
	"sort"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/ast/symbols"
	"github.com/Lyra-Language/lyra/pkg/driver"
)

// Options controls what a run covers.
type Options struct {
	// Modules are the module paths to document. Empty means every module in the
	// program, which is only what a caller wants when it has already decided that.
	Modules []string
	// IncludePrivate documents declarations without `pub`. Off by default: a
	// generated page is read by someone deciding what to call, and a private
	// declaration is not callable from where they are.
	IncludePrivate bool
}

// Kind is what a declaration is, which is also how a page groups it.
type Kind int

const (
	// KindType is a struct, data, tuple, newtype or alias declaration.
	KindType Kind = iota
	KindTrait
	KindImpl
	KindFunction
	// KindValue is a `let`/`var`/`const` binding whose value is not a function.
	KindValue
)

func (k Kind) String() string {
	switch k {
	case KindType:
		return "Types"
	case KindTrait:
		return "Traits"
	case KindImpl:
		return "Implementations"
	case KindFunction:
		return "Functions"
	default:
		return "Values"
	}
}

// Module is one documented module: its own `//!` documentation and its declarations.
type Module struct {
	Path  string
	Doc   *ast.Doc
	Decls []Decl
}

// Decl is one documented declaration.
type Decl struct {
	Name string
	Kind Kind
	// Signature is the declaration re-rendered as Lyra source, without its body.
	Signature string
	Doc       *ast.Doc
	// Members are a struct's fields, a data type's constructors, a trait's method
	// signatures or an impl's methods — whichever the declaration has.
	Members  []Member
	IsPublic bool
	Location ast.Location
}

// Documented reports whether this declaration carries prose. An undocumented public
// declaration is still listed — the signature is real information, and dropping it would
// make a page silently misrepresent the module's surface — so this exists for the
// coverage report rather than for filtering.
func (d Decl) Documented() bool { return d.Doc != nil }

// Member is a field, constructor or method of a declaration.
type Member struct {
	Name      string
	Signature string
	Doc       *ast.Doc
}

// Collect builds the documentation model from an analyzed program.
//
// It keys statements to modules through SymbolTable.ModuleOfFile rather than by name,
// because a name does not identify a declaration — two modules may each declare a
// `Point`, and the file a statement came from is the only thing that says which module
// owns it.
func Collect(res *driver.Result, opts Options) []Module {
	if res == nil || res.Program == nil {
		return nil
	}
	wanted := map[string]bool{}
	for _, m := range opts.Modules {
		wanted[m] = true
	}

	byModule := map[string]*Module{}
	moduleFor := func(path string) *Module {
		if m, ok := byModule[path]; ok {
			return m
		}
		m := &Module{Path: path, Doc: moduleDoc(res.SymbolTable, path)}
		byModule[path] = m
		return m
	}

	for _, stmt := range res.Program.Statements {
		modulePath := res.SymbolTable.ModuleOfFile[stmt.GetLocation().File]
		if len(wanted) > 0 && !wanted[modulePath] {
			continue
		}
		decl, ok := declFor(stmt)
		if !ok {
			continue
		}
		if !decl.IsPublic && !opts.IncludePrivate {
			continue
		}
		m := moduleFor(modulePath)
		m.Decls = append(m.Decls, decl)
	}

	// A module named in Modules but holding no documented declaration still gets a
	// page, so a run's output matches what was asked for rather than silently
	// dropping a module whose surface is entirely private.
	for path := range wanted {
		if _, ok := byModule[path]; !ok {
			if _, known := knownModule(res.SymbolTable, path); known {
				moduleFor(path)
			}
		}
	}

	out := make([]Module, 0, len(byModule))
	for _, m := range byModule {
		sortDecls(m.Decls)
		out = append(out, *m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// sortDecls groups by kind and sorts alphabetically within each group.
//
// Alphabetical rather than source order, because for a *multi-file* module source order
// is file order — which is a fact about the filesystem, not about the author's intent —
// so it would look meaningful while being arbitrary. A reference page is read by
// looking something up.
func sortDecls(decls []Decl) {
	sort.SliceStable(decls, func(i, j int) bool {
		if decls[i].Kind != decls[j].Kind {
			return decls[i].Kind < decls[j].Kind
		}
		if decls[i].Name != decls[j].Name {
			return decls[i].Name < decls[j].Name
		}
		// A receiver-overloaded name has several declarations; order them by where
		// they were written so a run is reproducible.
		if decls[i].Location.File != decls[j].Location.File {
			return decls[i].Location.File < decls[j].Location.File
		}
		return decls[i].Location.StartLine < decls[j].Location.StartLine
	})
}

func moduleDoc(table *symbols.SymbolTable, path string) *ast.Doc {
	if table == nil {
		return nil
	}
	return table.ModuleDocs[path]
}

// knownModule reports whether the symbol table has heard of a module path at all, so a
// caller naming a module that does not exist can be told rather than handed an empty
// page.
func knownModule(table *symbols.SymbolTable, path string) (string, bool) {
	if table == nil {
		return "", false
	}
	for _, m := range table.ModuleOfFile {
		if m == path {
			return path, true
		}
	}
	_, ok := table.ModuleDocs[path]
	return path, ok
}

// declFor converts one top-level statement into a Decl, reporting false for a statement
// that is not a declaration (an import, an expression statement).
func declFor(stmt ast.AstNode) (Decl, bool) {
	switch s := stmt.(type) {
	case *ast.VarDeclStmt:
		kind := KindValue
		if _, isFn := s.Value.(*ast.LambdaExpr); isFn {
			kind = KindFunction
		}
		return Decl{
			Name:      s.Name,
			Kind:      kind,
			Signature: bindingSignature(s),
			Doc:       s.Doc,
			IsPublic:  s.IsPublic,
			Location:  s.GetLocation(),
		}, true

	case *ast.TypeDeclStmt:
		return Decl{
			Name:      s.Name,
			Kind:      KindType,
			Signature: typeSignature(s),
			Doc:       s.Doc,
			Members:   typeMembers(s),
			IsPublic:  s.IsPublic,
			Location:  s.GetLocation(),
		}, true

	case *ast.TraitDeclStmt:
		return Decl{
			Name:      s.Name,
			Kind:      KindTrait,
			Signature: traitSignature(s),
			Doc:       s.Doc,
			Members:   traitMembers(s),
			IsPublic:  s.IsPublic,
			Location:  s.GetLocation(),
		}, true

	case *ast.TraitImplStmt:
		return Decl{
			Name:      implName(s),
			Kind:      KindImpl,
			Signature: implSignature(s),
			Doc:       s.Doc,
			Members:   implMembers(s),
			// An impl has no `pub`: its visibility is the trait's and the type's,
			// and an impl that is reachable at all is reachable everywhere they
			// are. Treating it as public is what keeps `impl Ord for string` on
			// the prelude's page.
			IsPublic: true,
			Location: s.GetLocation(),
		}, true
	}
	return Decl{}, false
}

// Coverage is the documented/total count of the public surface, for --strict and for a
// run's summary line.
type Coverage struct {
	Documented   int
	Total        int
	Undocumented []string
}

// Measure counts documentation coverage across modules.
//
// Members count too: a documented `data` type whose constructors are bare is a page that
// names three things and explains one.
//
// **An impl's methods are the exception, and are not counted.** A trait method's
// documentation is the *contract* — what any implementation must do — and it lives on
// the trait, where it is required. An impl's own method doc says what this particular
// implementation does differently, so for most impls having none is the correct state
// rather than a gap; demanding one produces a paragraph restating the trait, which is a
// second copy that goes stale. They are still rendered when present.
func Measure(mods []Module) Coverage {
	var c Coverage
	for _, m := range mods {
		for _, d := range m.Decls {
			c.Total++
			if d.Documented() {
				c.Documented++
			} else {
				c.Undocumented = append(c.Undocumented, m.Path+"."+d.Name)
			}
			if d.Kind == KindImpl {
				continue
			}
			for _, mem := range d.Members {
				c.Total++
				if mem.Doc != nil {
					c.Documented++
				} else {
					c.Undocumented = append(c.Undocumented, m.Path+"."+d.Name+"."+mem.Name)
				}
			}
		}
	}
	sort.Strings(c.Undocumented)
	return c
}
