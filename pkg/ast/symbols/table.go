package symbols

import (
	"fmt"
	"slices"
	"sort"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// Scope represents a lexical scope
type Scope struct {
	Parent   *Scope
	Children []*Scope
	Symbols  map[string]ast.Named // Variables and other named entities
	Kind     ScopeKind
}

type ScopeKind int

const (
	ScopeGlobal ScopeKind = iota
	ScopeModule
	ScopeFunction
	ScopeBlock
	ScopeLoop
	ScopePrelude
	// ScopeImports holds the names a module's `import` statements bring in, and sits
	// between that module's own scope and the prelude's. It is what makes an import's
	// member list mean something: a name reaches a module because the module asked for
	// it, not because some other module marked it `pub`. See ImportScopeFor.
	ScopeImports
)

func NewScope(parent *Scope, kind ScopeKind) *Scope {
	s := &Scope{
		Parent:   parent,
		Children: []*Scope{},
		Symbols:  make(map[string]ast.Named),
		Kind:     kind,
	}
	if parent != nil {
		parent.Children = append(parent.Children, s)
	}
	return s
}

// Define adds a named AST node to the current scope
func (s *Scope) Define(node ast.Named) error {
	name := node.GetName()
	if existing, exists := s.Symbols[name]; exists {
		existingLoc := existing.GetLocation()
		return fmt.Errorf("symbol %q already defined at %s", name, describeLocation(existingLoc))
	}
	s.Symbols[name] = node
	return nil
}

// Lookup searches for a symbol in this scope and parent scopes
func (s *Scope) Lookup(name string) (ast.Named, bool) {
	if sym, ok := s.Symbols[name]; ok {
		return sym, true
	}
	if s.Parent != nil {
		return s.Parent.Lookup(name)
	}
	return nil, false
}

// LookupLocal only searches the current scope (no parents)
func (s *Scope) LookupLocal(name string) (ast.Named, bool) {
	sym, ok := s.Symbols[name]
	return sym, ok
}

// SymbolTable is the top-level container for all symbols
// It provides quick lookups by name, pointing directly to AST nodes
type SymbolTable struct {
	GlobalScope  *Scope
	CurrentScope *Scope

	// PreludeScope holds the prelude's exported bindings, and sits **between** every
	// module scope and the global one: a lookup runs module → prelude → global.
	//
	// That middle position is the whole point. The prelude is ambient, so it has to be
	// reachable from every module; a module's own declaration has to beat it; and — the
	// case a single namespace got wrong — one module taking a prelude name must not
	// change what the name means anywhere else. Putting the prelude *under* the global
	// scope would do exactly that, since the global scope holds every module's exports
	// and is on every module's chain, so the first module to declare `Maybe` would hand
	// its own to everyone. Above the module scopes and below the global one, a shadow
	// reaches precisely as far as the scope that declares it.
	PreludeScope *Scope

	// TypeRefs records where each named type is *written* — the question every
	// position-based editor feature has to answer before it can resolve anything, and
	// the one a type value cannot answer for itself (see typerefs.go).
	TypeRefs *TypeRefTable

	// Quick lookup tables - these point to AST nodes directly
	Types     map[string]*ast.TypeDeclStmt
	Functions map[string]*ast.LambdaExpr
	Traits    map[string]*ast.TraitDeclStmt

	// ModuleOf records which module declared each top-level name, and ModuleOfFile
	// which module each source file belongs to. Together they answer the two questions
	// namespacing needs: "does `math.double` really name something in util.math?" and
	// "which module is this reference being made from?" — the latter recovered from a
	// node's Location.File, so no pass has to thread a module context through itself.
	ModuleOf     map[string]string
	ModuleOfFile map[string]string

	// PreludeModule is the implicitly-imported module's path, letting registration
	// tell "this name is already taken by the prelude" (allowed, warned) apart from a
	// genuine clash between two user modules (an error).
	PreludeModule string

	// ModuleDocs holds each module's `//!` header documentation, keyed by module path.
	//
	// Keyed by module rather than by file because a module is a file *or a directory*
	// (08/07): a directory module's several files may each say something about the
	// module they join, and a reader of the generated docs wants one page per module,
	// not one per file. The entry file of a single-file program has module path "",
	// which is a key like any other.
	ModuleDocs map[string]*ast.Doc

	// Shadowed records every declaration that took a name arriving from somewhere else
	// — the prelude, or a module this one imports. Both are warnings, never errors:
	// see noteAmbientShadow.
	Shadowed []ShadowedName

	// ImportedModules maps a module to the module paths its files import. It is the
	// import graph, handed over before the first file is walked (SetImports) because
	// a type's key is computed *during* the walk and depends on it.
	//
	// Deliberately not derived from Imports below: that map is filled in per file as
	// each is walked, so a declaration in the first file of a multi-file module would
	// be keyed before the file carrying the `import` had been seen.
	ImportedModules map[string][]string

	// PreludeNames is the set of names the prelude declares. Kept separately from
	// ModuleOf, which is last-writer-wins: when a user module declares a name the
	// prelude also declares, it overwrites ModuleOf's entry, so by the time functions
	// are registered (in Finish, after every file) ModuleOf no longer remembers the
	// prelude ever had it — and the shadow would be reported as a hard collision.
	PreludeNames map[string]bool

	// ModuleScopes holds one ScopeModule per module, each a child of that module's
	// imports scope. See ModuleScopeFor.
	ModuleScopes map[string]*Scope

	// ImportScopes holds one ScopeImports per module — the names its `import`
	// statements bring in, populated by PopulateImportScopes once every module's
	// exports are known. See ImportScopeFor.
	ImportScopes map[string]*Scope

	// Imports maps a module path to the imports its files declare. A plain
	// `import a.b` binds a *namespace* under the last segment (or its `as` alias);
	// `.{ X, Y as Z }` binds the listed names directly.
	Imports map[string][]Import

	// OverloadSets holds the names that are overloaded on their receiver, keyed by
	// declKey exactly as Functions is. A name appears in one map or the other, never
	// both — see overload.go for why an overloaded name is kept out of Functions
	// rather than being represented there by one of its members.
	OverloadSets map[string]*ast.OverloadSet
}

func NewSymbolTable() *SymbolTable {
	global := NewScope(nil, ScopeGlobal)
	st := &SymbolTable{
		GlobalScope: global,
		// **Not a child of GlobalScope.** Resolution stops at the prelude: a name reaches
		// a module through its own scope, its imports scope, or the prelude, and nowhere
		// else. Parenting the prelude on the global scope is what made *exported* and
		// *visible* the same thing — every `pub` declaration in the program sat on every
		// module's chain, so an import's member list restricted nothing.
		//
		// GlobalScope is still written (exportToGlobal) and still read, but for a
		// different question: it is the program-wide name registry that makes two modules
		// exporting one name an error, and the map ImportScopeFor draws from.
		PreludeScope: NewScope(nil, ScopePrelude),
		TypeRefs:     NewTypeRefTable(),
		Types:        make(map[string]*ast.TypeDeclStmt),
		Functions:    make(map[string]*ast.LambdaExpr),
		Traits:       make(map[string]*ast.TraitDeclStmt),
		ModuleOf:     make(map[string]string),
		ModuleOfFile: make(map[string]string),
		Imports:      make(map[string][]Import),
		ModuleScopes: make(map[string]*Scope),
		ImportScopes: make(map[string]*Scope),
		PreludeNames: make(map[string]bool),
		OverloadSets: make(map[string]*ast.OverloadSet),
		ModuleDocs:   make(map[string]*ast.Doc),

		ImportedModules: make(map[string][]string),
	}
	st.CurrentScope = st.ModuleScopeFor("")
	return st
}

// AddModuleDoc records a file's `//!` header against its module, joining it to whatever
// another file of the same module contributed.
//
// Joining rather than replacing is the only choice that does not lose text: a directory
// module's files are walked in a fixed order, and picking one file's header would
// silently discard every other file's. Two paragraphs are separated by a blank line so
// the result is still valid Markdown.
//
// `lead` puts this file's header **first**, ahead of anything already recorded. It is
// how a multi-file module gets a real opening paragraph: the joined text's first
// paragraph is the module's Summary, and that would otherwise be whichever file the
// walk happened to reach first — for the prelude, alphabetically `array.lyra`, so the
// standard library summarized as "Combinators over []t." The caller decides what
// qualifies (pkg/modules: a file named for the module's last path segment).
func (s *SymbolTable) AddModuleDoc(modulePath string, doc *ast.Doc, lead bool) {
	if doc == nil {
		return
	}
	existing, ok := s.ModuleDocs[modulePath]
	if !ok {
		s.ModuleDocs[modulePath] = doc
		return
	}
	if lead {
		s.ModuleDocs[modulePath] = ast.JoinDocs(doc, existing)
		return
	}
	s.ModuleDocs[modulePath] = ast.JoinDocs(existing, doc)
}

// LookupType, LookupTrait and LookupFunction are the **only** ways a pass should
// resolve a top-level name to its declaration.
//
// They exist as a choke point rather than as sugar over the maps. Answering "which
// declaration does this name mean" depends on *which module is asking*, and a lookup
// scattered across dozens of call sites cannot be taught that. Routing every read
// through here means module resolution is a change in one place — the same reason
// recordedType, stripNewtype and slotIsOwning exist.
//
// All three are nil-receiver-safe, so a consumer without a symbol table sees "not
// found" rather than crashing.
//
// The bare forms answer for a **program-wide** name — what a reference with no module
// context could mean: the prelude's export, an export any module published, or the
// entry module's declaration, in that order (the order the old bare-key scheme
// produced: a shadowing declaration was qualified, so the ambient one kept the bare
// key). A caller that has a referencing location must ask the From forms instead,
// which resolve as that location's module sees the name.
func (st *SymbolTable) LookupType(name string) (*ast.TypeDeclStmt, bool) {
	sym, ok := st.programWide(name)
	if !ok {
		return nil, false
	}
	decl, ok := sym.(*ast.TypeDeclStmt)
	return decl, ok
}

func (st *SymbolTable) LookupTrait(name string) (*ast.TraitDeclStmt, bool) {
	sym, ok := st.programWide(name)
	if !ok {
		return nil, false
	}
	decl, ok := sym.(*ast.TraitDeclStmt)
	return decl, ok
}

// programWide is the shared body of the bare lookups: the declaration a context-free
// reference to name would mean. Prelude before global scope, because a declaration
// taking a prelude name is the shadowing one — confined to its own module — while the
// prelude's stays ambient; the entry module last, since nothing can import it.
func (st *SymbolTable) programWide(name string) (ast.Named, bool) {
	if st == nil {
		return nil, false
	}
	if st.PreludeScope != nil {
		if sym, ok := st.PreludeScope.LookupLocal(name); ok {
			return sym, true
		}
	}
	if st.GlobalScope != nil {
		if sym, ok := st.GlobalScope.LookupLocal(name); ok {
			return sym, true
		}
	}
	if scope := st.moduleScope(""); scope != nil {
		if sym, ok := scope.LookupLocal(name); ok {
			return sym, true
		}
	}
	return nil, false
}

// ProgramWideTypeKey is the identity key of the declaration LookupType(name) answers
// with. It exists for the backend's emitted type names: a type whose key is the
// program-wide meaning of its name may carry that name verbatim in the IR — readable in
// a dump, and safe because at most one declaration per name can be the program-wide one.
func (st *SymbolTable) ProgramWideTypeKey(name string) (string, bool) {
	decl, ok := st.LookupType(name)
	if !ok {
		return "", false
	}
	return st.DeclKey(decl), true
}

// LookupTypeFrom and LookupTraitFrom resolve a name as the file at loc sees it: the
// asking module's own declaration, an imported one, or the prelude's — declKey's three
// rungs, so the import boundary is enforced by resolution itself. A declaration the
// asking module neither made nor imported resolves to that module's own (absent) key
// and misses, which is what `import lib.{ listed }` admitting only `listed` means here.
func (st *SymbolTable) LookupTypeFrom(name string, loc ast.Location) (*ast.TypeDeclStmt, bool) {
	if st == nil {
		return nil, false
	}
	decl, ok := st.Types[st.declKey(name, loc)]
	return decl, ok
}

func (st *SymbolTable) LookupTraitFrom(name string, loc ast.Location) (*ast.TraitDeclStmt, bool) {
	if st == nil {
		return nil, false
	}
	decl, ok := st.Traits[st.declKey(name, loc)]
	return decl, ok
}

// LookupTypeIn and LookupTraitIn resolve a name as a member of a named module — the
// namespace form, `shapes.Point`. Membership is the caller's job to establish
// (ModuleDeclares); these only pick the right entry once it has. Mirrors
// LookupFunctionIn.
//
// They find a **private** declaration too, which is deliberate and is why they are
// separate from LookupTypeFrom: the visibility check needs the declaration in order to
// refuse it, so a lookup that hid it would report "no such member" for a name the module
// really does declare.
func (st *SymbolTable) LookupTypeIn(module, name string) (*ast.TypeDeclStmt, bool) {
	if st == nil {
		return nil, false
	}
	decl, ok := st.Types[qualifiedName(module, name)]
	return decl, ok
}

func (st *SymbolTable) LookupTraitIn(module, name string) (*ast.TraitDeclStmt, bool) {
	if st == nil {
		return nil, false
	}
	decl, ok := st.Traits[qualifiedName(module, name)]
	return decl, ok
}

func (st *SymbolTable) LookupFunction(name string) (*ast.LambdaExpr, bool) {
	sym, ok := st.programWide(name)
	if !ok {
		return nil, false
	}
	// The program-wide scopes hold the *binding*; Functions holds the lambda. The
	// binding's own module and name recover its identity key.
	fn, ok := st.Functions[st.DeclKey(sym)]
	return fn, ok
}

// LookupFunctionFrom resolves a function name as the file at loc sees it: the asking
// module's own declaration when it has one, otherwise the program-wide entry.
//
// A bare LookupFunction cannot answer this. Functions is keyed by FunctionKey, so a
// module's private declaration — and a declaration that took a prelude name — lives
// under a module-qualified key; asking for the bare name from inside that module
// returns *another* module's function, silently. That is the wrong answer for the two
// passes that resolve a callee to read its parameter modes (ownership and the
// use-after-move check), where reading the wrong signature is a memory-safety decision
// made against the wrong function.
func (st *SymbolTable) LookupFunctionFrom(name string, loc ast.Location) (*ast.LambdaExpr, bool) {
	if st == nil {
		return nil, false
	}
	fn, ok := st.Functions[st.declKey(name, loc)]
	return fn, ok
}

// LookupFunctionIn resolves a function name as a member of a named module — the
// namespace form, `math.double`. Membership is the caller's job to establish
// (ModuleDeclares); this only picks the right entry once it has.
func (st *SymbolTable) LookupFunctionIn(module, name string) (*ast.LambdaExpr, bool) {
	if st == nil {
		return nil, false
	}
	if fn, ok := st.Functions[qualifiedName(module, name)]; ok {
		return fn, true
	}
	fn, ok := st.Functions[name]
	return fn, ok
}

func (st *SymbolTable) PushScope(kind ScopeKind) *Scope {
	st.CurrentScope = NewScope(st.CurrentScope, kind)
	return st.CurrentScope
}

// PopScope returns to the enclosing scope, stopping at a module (or the global) scope.
//
// The guard matters now that a module scope has parents of its own: an unbalanced pop
// used to be harmless because the global scope was the root, and would otherwise start
// walking out into the prelude and global scopes, where a later declaration would land
// in a scope no file owns.
func (st *SymbolTable) PopScope() {
	if st.CurrentScope == nil || st.CurrentScope.Parent == nil {
		return
	}
	switch st.CurrentScope.Kind {
	case ScopeGlobal, ScopeModule, ScopePrelude:
		return
	}
	st.CurrentScope = st.CurrentScope.Parent
}

// RegisterType adds a type declaration to the symbol table, under its identity key
// (DeclKey).
//
// **The declaration lands in its own module's scope first**, and that ordering is
// load-bearing rather than tidy: resolution (declKey) asks the module scope whether the
// name is declared there, so a lookup made before the scope knows about the declaration
// resolves the name to something else. Types are registered *during* the walk (unlike
// functions, which are registered in Finish once every file is done), so there is no
// later point at which the scope is already populated.
//
// Publication to the global scope is **not** done here. A declaration is exported by
// collector.exportToGlobal alongside `pub` bindings — one rule, one place — and only a
// `pub` one crosses. Defining every type globally is what made a private type compete
// for a program-wide name, so two modules could not each declare a `Point`.
// The shadow is noted **after** the define, not before: the import half of the question
// asks whether this module declares the name, which is true only once the declaration is
// in its scope. The prelude half never depended on the scope, so nothing moved for it.
func (st *SymbolTable) RegisterType(node *ast.TypeDeclStmt) error {
	if err := st.defineInDeclaringModule("type", node); err != nil {
		return err
	}
	st.noteAmbientShadow(node.Name, node.GetLocation())
	st.Types[st.DeclKey(node)] = node
	return nil
}

// RegisterTrait adds a trait declaration to the symbol table.
// Returns an error if a trait with the same name is already registered in the same
// module; see RegisterType for why the module scope is written first.
func (st *SymbolTable) RegisterTrait(node *ast.TraitDeclStmt) error {
	if err := st.defineInDeclaringModule("trait", node); err != nil {
		return err
	}
	st.noteAmbientShadow(node.Name, node.GetLocation())
	st.Traits[st.DeclKey(node)] = node
	return nil
}

// defineInDeclaringModule puts a top-level declaration in the scope of the module whose
// file declared it, recovered from the declaration's own location — the same way every
// other module question is answered, rather than from a "module being collected" cursor.
//
// A duplicate here is a redeclaration *within one module*, which is a genuine error and
// the one this reports. A clash between two modules is not visible from here at all,
// which is the point: it is either legal (both private) or reported when the second one
// tries to export (exportToGlobal).
//
// kind names what clashed ("type", "trait"). The message is built here rather than left
// to Scope.Define because that one says "symbol", which is right for a local binding and
// vague for a declaration — and because a duplicate trait used to report no location at
// all, the thing describeLocation exists to stop.
func (st *SymbolTable) defineInDeclaringModule(kind string, node ast.Named) error {
	scope := st.ModuleScopeFor(st.ModuleOfFile[node.GetLocation().File])
	if scope == nil {
		return nil
	}
	if existing, dup := scope.LookupLocal(node.GetName()); dup {
		return fmt.Errorf("%s %q already defined at %s",
			kind, node.GetName(), describeLocation(existing.GetLocation()))
	}
	return scope.Define(node)
}

// RegisterFunction adds a function to the symbol table. If node is declared
// `pure`.
//
// A duplicate is an error, as it already is for a type or a trait. It used to
// overwrite silently, which was harmless while a program was a single file — a
// redeclaration within one file is caught earlier, by the scope's own Define — but with
// modules it meant two modules could each define `helper` and the program would build,
// calling whichever happened to be registered last. A silently wrong program, chosen by
// collection order.
func (st *SymbolTable) RegisterFunction(name string, node *ast.LambdaExpr) error {
	st.noteAmbientShadow(name, node.GetLocation())
	// An **overloaded** name is registered as its whole set, once, and kept out of
	// Functions entirely (overload.go). The scope decided the name was overloaded, back
	// when the two declarations met during the walk; this only mirrors the result, so
	// the merge rule stays in one place. Every member reaches this function — Finish
	// walks top-level bindings, not names — and each one re-registers the same set,
	// which is why the write is idempotent rather than an append.
	//
	// The identity key is built here rather than through DeclKey because the lambda is
	// not the named thing — the binding that holds it is, and `name` is the binding's.
	key := qualifiedName(st.ModuleOfFile[node.GetLocation().File], name)
	if set, overloaded := st.OverloadSetFor(name, node.GetLocation()); overloaded {
		st.registerOverloadSet(key, set)
		return nil
	}
	// Keys are per-module, so a clash here is a redeclaration *within one module* —
	// two modules exporting one name is exportToGlobal's to report, and two modules
	// each declaring a private (or entry-module) one is legal and does not meet.
	if existing, exists := st.Functions[key]; exists && existing != node {
		return fmt.Errorf("function %q is already defined at %s%s", name,
			describeLocation(existing.GetLocation()), overloadRefusal(existing, node))
	}
	st.Functions[key] = node
	return nil
}

// overloadRefusal explains why two same-named functions were not accepted as receiver
// overloads, as a clause appended to the "already defined" message.
//
// It is only worth saying when both really are functions taking a `self` receiver: that
// is the reader who tried to overload and hit a rule, and who would otherwise be told
// only that the name is taken. For a plain redeclaration — the common case by far — the
// clause would be noise about a feature the author was not using, so it stays empty.
func overloadRefusal(existing, node *ast.LambdaExpr) string {
	if _, ok := ast.ReceiverParam(existing); !ok {
		return ""
	}
	if _, ok := ast.ReceiverParam(node); !ok {
		return ""
	}
	existingHead, _ := types.HeadName(receiverType(existing))
	nodeHead, _ := types.HeadName(receiverType(node))
	if existingHead != "" && existingHead == nodeHead {
		return fmt.Sprintf(". Both take a `%s` receiver: overloads are told apart by the"+
			" receiver's type, so two of one type cannot be", existingHead)
	}
	return ". A `self` function may be overloaded only against another declared in the" +
		" same module, on a receiver with a concrete type of its own"
}

func receiverType(fn *ast.LambdaExpr) types.Type {
	recv, ok := ast.ReceiverParam(fn)
	if !ok {
		return nil
	}
	return recv.Type
}

// Keys are **uniform**: every declaration is stored under `<module>::<name>`, with the
// entry module's empty path giving `::<name>`. Identity and resolution are therefore two
// different questions with two different functions:
//
//   - **DeclKey(node)** is a declaration's identity — the key Register* stores it under,
//     computed from the declaration's own file and name and nothing else.
//   - **declKey(name, loc)** is resolution — the identity key of the declaration that
//     `name`, written at loc, resolves to: the asking module's own declaration, then its
//     imports (following an alias to the source declaration), then the prelude's export.
//
// The two agree at a declaration's own site — its module's scope holds it, so the first
// rung answers — which is what lets a map write use DeclKey and every read use declKey.
//
// Until 08/27 the key itself encoded visibility (bare when exported or in the entry
// module, qualified when private or shadowing an ambient name), so identity and
// resolution coincided through the shared bare key — and a lookup that asked with the
// wrong context *usually* worked, which is exactly what made the wrong callers pass
// review and tests (invariant 4's four corollaries, and the purity pass's two-month
// last-writer-wins bug). A bare-key read now simply misses, so a caller without a
// context must use the program-wide forms (LookupType et al.), which say what they mean.
//
// FunctionKey and TypeKey are two names for **one** rule (declKey), not two rules that
// happen to agree — a module's `Point` and its `point` are the same kind of name as far
// as "which declaration does this mean" is concerned.
func (st *SymbolTable) FunctionKey(name string, loc ast.Location) string {
	return st.declKey(name, loc)
}

func (st *SymbolTable) TypeKey(name string, loc ast.Location) string {
	return st.declKey(name, loc)
}

// DeclKey is the identity key a top-level declaration is stored under:
// `<module>::<name>`, from the declaration's own file. Registration must use this
// rather than declKey — resolution consults scopes, and a declaration that failed to
// land in its scope (a duplicate) would resolve to something it is not.
func (st *SymbolTable) DeclKey(node ast.Named) string {
	if st == nil || node == nil {
		return ""
	}
	return qualifiedName(st.ModuleOfFile[node.GetLocation().File], node.GetName())
}

func (st *SymbolTable) declKey(name string, loc ast.Location) string {
	if st == nil {
		return name
	}
	return st.declKeyIn(st.ModuleOfFile[loc.File], name)
}

// ResolvedReachably reports whether name, written at loc, resolves through the rungs a
// *written* name may use — the module's own scope, its imports, or the prelude. The
// resolver itself has a fourth rung (any program-wide export), which exists for types
// that values carry across the import boundary; a site reporting on a name the user
// wrote asks this first, so `import lib.{ listed }` still refuses an unlisted `Point`
// annotation while a `Point` value from a listed function's return stays usable.
func (st *SymbolTable) ResolvedReachably(name string, loc ast.Location) bool {
	if st == nil {
		return false
	}
	module := st.ModuleOfFile[loc.File]
	if st.ModuleDeclares(module, name) {
		return true
	}
	if imports := st.ImportScopes[module]; imports != nil {
		if _, found := imports.LookupLocal(name); found {
			return true
		}
	}
	if st.PreludeScope != nil {
		if _, found := st.PreludeScope.LookupLocal(name); found {
			return true
		}
	}
	return false
}

// declKeyIn resolves name as module sees it: its own declaration, an imported one, or
// the prelude's — the same three rungs, in the same order, as the value scope chain
// (module → imports → prelude), so a key-based lookup and a scope-based one cannot
// disagree about which declaration a name means.
func (st *SymbolTable) declKeyIn(module, name string) string {
	if st.ModuleDeclares(module, name) {
		return qualifiedName(module, name)
	}
	// An import binds a *local* name to a source declaration, so the resolved key is
	// the declaration's own — which is what makes `import a.{ X as Y }` find `a::X`
	// under the name this module writes as `Y`.
	if imports := st.ImportScopes[module]; imports != nil {
		if sym, found := imports.LookupLocal(name); found {
			return qualifiedName(st.ModuleOfFile[sym.GetLocation().File], sym.GetName())
		}
	}
	if st.PreludeScope != nil {
		if _, found := st.PreludeScope.LookupLocal(name); found {
			return qualifiedName(st.PreludeModule, name)
		}
	}
	// A program-wide **export** resolves from anywhere, imported or not. This is the
	// rung that lets a value's type outrun the import boundary: `match ev { Mouse(m) =>
	// m.col }` reaches a struct the file never imported by name, because the value
	// carried it there. It is also the old scheme's de facto behaviour made
	// deterministic — the bare key held the export, and the type-resolution cache
	// (keyed the same way) leaked it past the import gate whenever the exporting
	// module had resolved the name first, which was nearly always. What the import
	// boundary actually polices is *identifier* resolution, which runs on the scope
	// chain and stops at the prelude.
	if st.GlobalScope != nil {
		if sym, found := st.GlobalScope.LookupLocal(name); found {
			return qualifiedName(st.ModuleOfFile[sym.GetLocation().File], sym.GetName())
		}
	}
	// Nothing answers: the name passes through unchanged. For a real name that is a
	// guaranteed miss — every declaration's key is qualified — and the pass-through is
	// load-bearing for *synthetic* names: the backend routes generic instantiation
	// symbols (`Maybe$i64`) through typeKey into a registry that shares this keyspace,
	// and those are registered raw, under no module.
	return name
}

// declIsPublic reports whether a top-level declaration is exported. Bindings, types and
// traits all carry `pub` on three different nodes, and one predicate serves every
// consumer (ModuleExports, PopulateImportScopes) rather than each switching over them.
//
// An unrecognised node counts as exported. That is the conservative direction: wrongly
// hiding a declaration takes it away from every module, while wrongly exporting it
// merely fails to confine it — the behaviour that predates module privacy at all.
func declIsPublic(sym ast.Named) bool {
	switch d := sym.(type) {
	case *ast.VarDeclStmt:
		return d.IsPublic
	case *ast.TypeDeclStmt:
		return d.IsPublic
	case *ast.TraitDeclStmt:
		return d.IsPublic
	case *ast.ExternDeclStmt:
		// An extern is **always** private, because there is no `pub extern` to write:
		// what a module exports is the Lyra wrapper it puts over one, which is the whole
		// division of labour the FFI design rests on (`std.ffi` builds in Lyra on top of
		// signatures nothing else sees). Without this case an extern took the default —
		// exported — and two modules each declaring `extern abs`, the way two modules
		// each use `strlen`, collided on the program-wide name. The C symbol they share
		// is the linker's business and the backend's, which declares it once (extern.go).
		return false
	case *ast.OverloadSet:
		// The members agree on `pub` — a set that disagreed was refused at the
		// declaration (ast.OverloadableWith) precisely so this question has an answer.
		// Without this case a set would fall to the default and be keyed as exported
		// whatever its members said, putting a module's private overloads in the
		// program-wide namespace.
		return len(d.Members) > 0 && d.Members[0].IsPublic
	default:
		return true
	}
}

// qualifiedName is the key a declaration gets when the bare name is not its alone.
func qualifiedName(module, name string) string {
	return module + "::" + name
}

// ModuleExports reports whether module declares name at its top level *and* exports it.
//
// The visibility half is what separates this from ModuleDeclares: a private name never
// reached the importer, so declaring one of your own shadows nothing.
func (st *SymbolTable) ModuleExports(module, name string) bool {
	scope := st.moduleScope(module)
	if scope == nil {
		return false
	}
	sym, ok := scope.LookupLocal(name)
	return ok && declIsPublic(sym)
}

// describeLocation renders a location for an "already defined" message, naming the file
// when there is one. Without it, two colliding modules both report a bare line:col —
// and the line:col of the *other* module's declaration is worse than useless, since it
// reads as a position in the file being reported on.
func describeLocation(loc ast.Location) string {
	if loc.File != "" {
		return fmt.Sprintf("%s:%s", loc.File, loc.Pretty())
	}
	return loc.Pretty()
}

// RegisterVariable adds a variable to the current scope
func (st *SymbolTable) RegisterVariable(node *ast.VarDeclStmt) error {
	if st.CurrentScope == nil {
		return fmt.Errorf("no current scope to register variable %s", node.Name)
	}
	return st.CurrentScope.Define(node)
}

// RedefineVariable replaces an existing binding under the same name in the
// current scope. It is used for same-scope sequential rebinding (e.g.
// `let x = parse(x)`), where the most recent declaration must win so that
// later references resolve to it.
func (st *SymbolTable) RedefineVariable(node *ast.VarDeclStmt) {
	if st.CurrentScope != nil {
		st.CurrentScope.Symbols[node.Name] = node
	}
}

// RegisterDestructuredName binds a single name introduced by a destructuring
// declaration (e.g. `x` in `let (x, y) = pair`) to that declaration in the
// current scope. The owning DestructuringDeclStmt is stored — rather than the
// leaf pattern — so consumers can recover the binding's mutability (`var` /
// `let mut`). Insertion is direct and last-wins (mirroring RedefineVariable) so
// same-scope rebinding does not error here; the collector emits duplicate
// diagnostics separately.
func (st *SymbolTable) RegisterDestructuredName(name string, decl *ast.DestructuringDeclStmt) {
	if st.CurrentScope != nil {
		st.CurrentScope.Symbols[name] = decl
	}
}

// RegisterParameter adds a lambda/function parameter to the current scope
func (st *SymbolTable) RegisterParameter(node *ast.Parameter) error {
	if st.CurrentScope == nil {
		return fmt.Errorf("no current scope to register parameter %s", node.GetName())
	}
	return st.CurrentScope.Define(node)
}

// ScopeTable maps scope-creating AST nodes (BlockExpr, LambdaExpr, etc.)
// to the Scope the collector pushed when it entered them.
// Populated during collection; used by the type-checker to enter the
// correct child scope when traversing a nested block or function body.
type ScopeTable struct {
	entries map[ast.AstNode]*Scope
}

func NewScopeTable() *ScopeTable {
	return &ScopeTable{entries: make(map[ast.AstNode]*Scope)}
}

func (st *ScopeTable) Set(node ast.AstNode, scope *Scope) {
	st.entries[node] = scope
}

func (st *ScopeTable) Get(node ast.AstNode) (*Scope, bool) {
	s, ok := st.entries[node]
	return s, ok
}

// Import is one resolved import as seen from the module that declared it.
//
// Namespace and selective imports are the two shapes the grammar allows, and they bind
// different things: a plain `import util.math` (or `... as m`) makes the module
// reachable under a name, so uses read `math.double(…)`; `import util.math.{ double }`
// puts the listed names in scope directly. Alias holds whichever name the reference
// site will use.
type Import struct {
	Path    string            // the module being imported, e.g. "util.math"
	Alias   string            // namespace name it binds; empty for a selective import
	Members map[string]string // local name → name in the imported module
	Loc     ast.Location
}

// IsNamespace reports whether this import binds a namespace rather than naming members.
func (i Import) IsNamespace() bool { return i.Alias != "" }

// ImportsFor returns the imports visible in the file at path.
func (st *SymbolTable) ImportsFor(file string) []Import {
	if st == nil {
		return nil
	}
	return st.Imports[st.ModuleOfFile[file]]
}

// NamespaceImport returns the import a namespace name refers to, as seen from file.
func (st *SymbolTable) NamespaceImport(file, name string) (Import, bool) {
	for _, imp := range st.ImportsFor(file) {
		if imp.IsNamespace() && imp.Alias == name {
			return imp, true
		}
	}
	return Import{}, false
}

// DeclaringModule reports which module declared a top-level name.
func (st *SymbolTable) DeclaringModule(name string) string {
	if st == nil {
		return ""
	}
	return st.ModuleOf[name]
}

// BindingOf returns the top-level `let`/`var` declaration that bound name.
//
// SymbolTable.Functions holds the *lambda*, but `pub` is a property of the binding
// that names it, so exporting a function can only be answered from the declaration.
func (st *SymbolTable) BindingOf(name string) (*ast.VarDeclStmt, bool) {
	if st == nil {
		return nil, false
	}
	// The declaring module's own scope first: a private binding lives only there, and
	// answering "is this exported" from the global scope alone would report it as not
	// found — which reads as "no such name" rather than "not yours".
	if scope, ok := st.ModuleScopes[st.ModuleOf[name]]; ok && scope != nil {
		if sym, found := scope.LookupLocal(name); found {
			if decl, isVar := sym.(*ast.VarDeclStmt); isVar {
				return decl, true
			}
		}
	}
	sym, ok := st.GlobalScope.LookupLocal(name)
	if !ok {
		return nil, false
	}
	decl, ok := sym.(*ast.VarDeclStmt)
	return decl, ok
}

// BindingIn is BindingOf asked of a **named** module — the binding half of LookupTypeIn
// and LookupTraitIn, and what a namespace reference (`seq.map`) needs.
//
// BindingOf cannot answer it. It finds the module through ModuleOf, which is
// last-writer-wins, so once two modules may each declare a `map` it reports on whichever
// was collected last: `seq.map` looked up the *entry* file's binding, found no `pub`, and
// reported the imported module's exported function as private to itself. That is hazard
// 4's rule — a `pub` check must ask about the declaration a reference resolved to — and
// the by-name form was simply unreachable while an imported name forbade a local one.
//
// It falls back to BindingOf, so a caller with no module in hand is no worse off than
// before.
func (st *SymbolTable) BindingIn(module, name string) (*ast.VarDeclStmt, bool) {
	if st == nil {
		return nil, false
	}
	if scope := st.moduleScope(module); scope != nil {
		if sym, found := scope.LookupLocal(name); found {
			if decl, isVar := sym.(*ast.VarDeclStmt); isVar {
				return decl, true
			}
		}
	}
	return st.BindingOf(name)
}

// ShadowedName records a user declaration that took a name reaching it from elsewhere.
//
// Source is the module the name came from — an imported module's path, or "" for the
// prelude. It is what lets the warning name a qualifier the reader can actually type
// (`seq.map`), which the prelude case has nothing to offer: the prelude is reachable
// precisely because nothing names it.
type ShadowedName struct {
	Name   string
	Loc    ast.Location
	Source string
}

// takesPreludeName reports whether name is currently held by the prelude and the module
// being collected is entitled to take it over.
//
// A prelude name must be shadowable. The prelude is implicitly in scope everywhere, so
// treating a clash as an error the way two user modules' clash is treated would mean
// every name the prelude exports is permanently unusable in user code — and adding a
// name to the prelude later would break programs that never mentioned it. The user's
// declaration wins and a warning is recorded.
//
// The shadow is confined to the declaring module (07/30): the prelude's bindings live
// in PreludeScope, which sits under every module scope, so a module's own declaration
// wins there while every other module still reaches the prelude's. What remains
// program-wide is a shadowed **type** or **trait** — see noteShadowed.
//
// The declaring module is recovered from the declaration's own file rather than from a
// "module being collected" cursor. Functions are registered in Finish, after every file
// has been walked, so such a cursor is stale by then — it pointed at the last file, and
// the prelude's own declarations were read as shadowing the prelude, which deleted them.
func (st *SymbolTable) takesPreludeName(name string, loc ast.Location) bool {
	if st == nil || st.PreludeModule == "" {
		return false
	}
	if st.ModuleOfFile[loc.File] == st.PreludeModule {
		return false // the prelude does not shadow itself
	}
	return st.PreludeNames[name]
}

// noteShadowed records the warning. Nothing is withdrawn: a shadow reaches exactly as
// far as the module that declared it, for every kind of name.
//
// This used to withdraw the prelude's **type** and **trait** entries, because their
// namespace was program-wide by construction — `Types` was keyed by bare name, so the
// prelude's `Maybe` and a user's could not both be in it, and the shadowing declaration
// had to be the program's one `Maybe`. The reachable consequence was a module that never
// mentioned `Maybe` losing the canonical one and reporting "`?` operand must be a Result
// or Maybe, got Maybe" — a diagnostic about a declaration it had never seen.
//
// Per-module keys are what removed the need: every declaration has a key of its own, so
// a shadowing declaration and the prelude's coexist exactly as a shadowing *binding* and
// the prelude's already did — each module reaching the one it should, because resolution
// tries the asking module's own declaration before the prelude's.
func (st *SymbolTable) noteShadowed(name string, loc ast.Location) {
	st.Shadowed = append(st.Shadowed, ShadowedName{Name: name, Loc: loc})
}

// noteAmbientShadow records the warning for a declaration that took a name reaching it
// from elsewhere, whichever source it came from.
//
// The prelude half asks takesPreludeName, which does not consult the declaring module's
// scope, because a *type* is registered before its own scope entry exists. The import
// half has no such wrinkle — every caller has already defined the declaration in its
// module scope by the time it gets here.
func (st *SymbolTable) noteAmbientShadow(name string, loc ast.Location) {
	if st == nil {
		return
	}
	if st.takesPreludeName(name, loc) {
		st.noteShadowed(name, loc)
		return
	}
	module := st.ModuleOfFile[loc.File]
	for _, imported := range st.ImportedModules[module] {
		if imported != module && st.ModuleExports(imported, name) && st.ModuleDeclares(module, name) {
			st.Shadowed = append(st.Shadowed,
				ShadowedName{Name: name, Loc: loc, Source: imported})
			return
		}
	}
}

// ModuleScopeFor returns the scope holding a module's top-level declarations,
// creating it as a child of PreludeScope on first use.
//
// This is where per-module name resolution lives: a declaration always lands in its
// own module's scope and only a `pub` one *also* lands in the global scope, so a
// private name is invisible outside its module and never competes for the global one.
//
// The scopes are siblings rather than nested, because modules do not contain one
// another — `util.math` is a name, not a position in a tree, and treating the dots as
// nesting would make `util` a scope that `util.math` could see into.
//
// **The entry module gets one too**, even though it declares no module path. It used to
// share the global scope on the grounds that a program root has nothing to be private
// from, and that is still true of privacy — but it also put the entry file's
// declarations in the scope every *other* module falls through to, so an entry-file
// `let unwrapOr` replaced the prelude's for the whole program. Its own scope is what
// confines that, at the cost of one thing to remember: anything walking scopes for a
// single file starts from EntryScope, not GlobalScope (the LSP's completion,
// go-to-definition and highlight walks all did the latter, which is why sharing the
// global scope looked like the simpler choice).
func (st *SymbolTable) ModuleScopeFor(module string) *Scope {
	if st == nil {
		return nil
	}
	if scope, ok := st.ModuleScopes[module]; ok {
		return scope
	}
	scope := NewScope(st.ImportScopeFor(module), ScopeModule)
	st.ModuleScopes[module] = scope
	return scope
}

// ImportScopeFor returns the scope holding what module's imports bring in, creating it
// as a child of PreludeScope on first use.
//
// **This is where "imported" stops meaning "exported by somebody".** Until it existed,
// every `pub` declaration went into one global scope that sat on every module's parent
// chain, so `import std.tui.{ bg }` admitted `grey`, `rgb` and `bold` too — the member
// list drove the namespace binding and the unused-import warning and nothing else. A name
// now reaches a module because that module asked for it.
//
// It sits *under* the module's own scope and *over* the prelude's, which is the order the
// three shadowing rules already assume: a module's own declaration wins over an imported
// name (lyra-W016), and an imported name wins over an ambient prelude one.
func (st *SymbolTable) ImportScopeFor(module string) *Scope {
	if st == nil {
		return nil
	}
	if scope, ok := st.ImportScopes[module]; ok {
		return scope
	}
	scope := NewScope(st.PreludeScope, ScopeImports)
	st.ImportScopes[module] = scope
	return scope
}

// PopulateImportScopes fills every module's imports scope from its import list, and must
// run **after every file has been walked** — a module's imports cannot be resolved until
// the modules they name have registered their exports, and exports are recorded per file.
// The Collector calls it from Finish.
//
// Only a **selective** import binds a bare name. `import shapes` binds the namespace
// `shapes.Point` and nothing else: if it also admitted bare `Point`, the two import forms
// would mean the same thing and the member list would be decoration.
//
// An alias binds only its local name — `import a.{ X as Y }` admits `Y`, never `X` — which
// is why the entry is written under the local name rather than through Define, whose key
// is the declaration's own.
//
// A member that does not exist, or exists and is not `pub`, binds nothing. It is not
// reported here: this runs inside the collector, where a missing import member is already
// the import checker's to report, and a second message about the same line would be worse
// than the one that names it precisely.
func (st *SymbolTable) PopulateImportScopes() {
	if st == nil {
		return
	}
	for module, imports := range st.Imports {
		scope := st.ImportScopeFor(module)
		for _, imp := range imports {
			if imp.IsNamespace() {
				continue
			}
			from, ok := st.ModuleScopes[imp.Path]
			if !ok || from == nil {
				continue
			}
			for local, source := range imp.Members {
				sym, found := from.LookupLocal(source)
				if !found || !declIsPublic(sym) {
					continue
				}
				scope.Symbols[local] = sym
			}
		}
	}
}

// ExportingModule reports the module that exports name, for a name that did not resolve
// where it was written. Empty when nothing exports it.
//
// This is what GlobalScope is *for* now that it is off every module's parent chain: it is
// the program-wide registry of exported names, so it can answer "this name exists, you did
// not ask for it" — which is the difference between a useful diagnostic and "undefined".
func (st *SymbolTable) ExportingModule(name string) (string, bool) {
	if st == nil || st.GlobalScope == nil {
		return "", false
	}
	sym, ok := st.GlobalScope.LookupLocal(name)
	if !ok {
		return "", false
	}
	module := st.ModuleOfFile[sym.GetLocation().File]
	if module == "" {
		return "", false
	}
	return module, true
}

// EntryScope is the scope of the file a compile started from — the module with no
// declared path. It is the scope a single-file program's declarations live in, and so
// the one any per-file scope walk should start from.
func (st *SymbolTable) EntryScope() *Scope {
	return st.ModuleScopeFor("")
}

// moduleScope returns a module's scope without creating one, so a question *about* a
// module ("does it declare this name?") cannot conjure an empty scope for a module that
// does not exist.
func (st *SymbolTable) moduleScope(module string) *Scope {
	if st == nil {
		return nil
	}
	return st.ModuleScopes[module]
}

// DeclaringModulesOf lists every module declaring name at its top level, sorted so a
// diagnostic built from it does not depend on map order.
//
// ModuleOf answers this approximately — it is last-writer-wins, so it remembers only the
// module that declared the name last. This is the exact form, and it exists for the
// not-found path: once a private declaration is confined to a module-qualified key, a
// reference from elsewhere simply fails to resolve, and "unknown type" reads as a typo
// for a name the author can plainly see in another file. Knowing who *does* declare it
// is what turns that back into "not yours" (lyra-E028).
func (st *SymbolTable) DeclaringModulesOf(name string) []string {
	if st == nil {
		return nil
	}
	var out []string
	for module, scope := range st.ModuleScopes {
		if scope == nil {
			continue
		}
		if _, ok := scope.LookupLocal(name); ok {
			out = append(out, module)
		}
	}
	sort.Strings(out)
	return out
}

// ModuleDeclares reports whether module declares name at its top level.
//
// This is the precise form of the question `ModuleOf` answers approximately: that map
// is last-writer-wins, so once two modules (or a module and the prelude) declare one
// name it remembers only the last. A module's own scope holds every top-level
// declaration it made, and nobody else's.
func (st *SymbolTable) ModuleDeclares(module, name string) bool {
	scope := st.moduleScope(module)
	if scope == nil {
		return false
	}
	_, ok := scope.LookupLocal(name)
	return ok
}

// DeclaringDataType is the declaration of the data type owning a constructor, as the file
// at loc sees it — the answer to "what does `Keyboard` in `Keyboard(Up)` belong to".
//
// A constructor is not a name the lookup maps: `data Event = Keyboard Key | …` registers
// `Event`, and a module importing `Event` may use its constructors without naming them, so
// no import admits `Keyboard` and no lookup finds it. Finding its owner means asking which
// data type has it, which is a scan — and the scan has to be ordered and visibility-checked,
// which is why it lives here rather than being rewritten by each caller.
//
// **Ordered by locality, then by key.** A module's own declaration shadows an ambient one
// for every other kind of name, and a constructor is not the exception: a program declaring
// `data Opt = Some(i64) | None` means *its* `Some`, not the prelude's. Alphabetical order
// alone is deterministic but picks by spelling, which would hand `Some` to `Maybe` on the
// strength of "Maybe" < "Opt".
//
// The candidate must also resolve, from *this* file, to *this* declaration: a type the file
// cannot see does not resolve, and one shadowed by a same-named local declaration resolves
// to the other one.
func (st *SymbolTable) DeclaringDataType(ctorName string, loc ast.Location) (*ast.TypeDeclStmt, bool) {
	if st == nil {
		return nil, false
	}
	asking := st.ModuleOfFile[loc.File]
	keys := make([]string, 0, len(st.Types))
	for k := range st.Types {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		li := st.ModuleOfFile[st.Types[keys[i]].GetLocation().File] == asking
		lj := st.ModuleOfFile[st.Types[keys[j]].GetLocation().File] == asking
		if li != lj {
			return li
		}
		return keys[i] < keys[j]
	})
	for _, k := range keys {
		decl := st.Types[k]
		dt, ok := decl.Type.(types.DataType)
		if !ok {
			continue
		}
		if !slices.ContainsFunc(dt.Constructors, func(c types.DataTypeConstructor) bool {
			return c.Name == ctorName
		}) {
			continue
		}
		if resolved, ok := st.LookupTypeFrom(decl.Name, loc); ok && resolved == decl {
			return decl, true
		}
	}
	return nil, false
}
