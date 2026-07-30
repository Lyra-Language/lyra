package symbols

import (
	"fmt"

	"github.com/Lyra-Language/lyra/pkg/ast"
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
	PreludeModule   string
	ShadowedPrelude []ShadowedName

	// PreludeNames is the set of names the prelude declares. Kept separately from
	// ModuleOf, which is last-writer-wins: when a user module declares a name the
	// prelude also declares, it overwrites ModuleOf's entry, so by the time functions
	// are registered (in Finish, after every file) ModuleOf no longer remembers the
	// prelude ever had it — and the shadow would be reported as a hard collision.
	PreludeNames map[string]bool

	// ModuleScopes holds one ScopeModule per module, each a child of GlobalScope.
	// See ModuleScopeFor for what it is for and what it is not yet.
	ModuleScopes map[string]*Scope

	// Imports maps a module path to the imports its files declare. A plain
	// `import a.b` binds a *namespace* under the last segment (or its `as` alias);
	// `.{ X, Y as Z }` binds the listed names directly.
	Imports map[string][]Import

	// PureFuncs maps the name of each function declared with the `pure`
	// keyword to its lambda expression. Populated during collection; used
	// by the purity checker to know which functions are explicitly pure
	// (and which are implicitly pure by default). PureFuncs is a subset
	// of Functions.
	PureFuncs map[string]*ast.LambdaExpr
}

func NewSymbolTable() *SymbolTable {
	global := NewScope(nil, ScopeGlobal)
	st := &SymbolTable{
		GlobalScope:  global,
		PreludeScope: NewScope(global, ScopePrelude),
		Types:        make(map[string]*ast.TypeDeclStmt),
		Functions:    make(map[string]*ast.LambdaExpr),
		Traits:       make(map[string]*ast.TraitDeclStmt),
		PureFuncs:    make(map[string]*ast.LambdaExpr),
		ModuleOf:     make(map[string]string),
		ModuleOfFile: make(map[string]string),
		Imports:      make(map[string][]Import),
		ModuleScopes: make(map[string]*Scope),
		PreludeNames: make(map[string]bool),
	}
	st.CurrentScope = st.ModuleScopeFor("")
	return st
}

// LookupType, LookupTrait and LookupFunction are the **only** ways a pass should
// resolve a top-level name to its declaration.
//
// They exist as a choke point rather than as sugar over the maps. Those maps are keyed
// by bare name and are read from seven packages; with modules, answering "which
// declaration does this name mean" depends on *which module is asking*, and a lookup
// scattered across dozens of call sites cannot be taught that. Routing every read
// through here means module resolution is a change in one place — the same reason
// recordedType, stripNewtype and slotIsOwning exist.
//
// All three are nil-receiver-safe, so a consumer without a symbol table sees "not
// found" rather than crashing.
func (st *SymbolTable) LookupType(name string) (*ast.TypeDeclStmt, bool) {
	if st == nil {
		return nil, false
	}
	decl, ok := st.Types[name]
	return decl, ok
}

func (st *SymbolTable) LookupTrait(name string) (*ast.TraitDeclStmt, bool) {
	if st == nil {
		return nil, false
	}
	decl, ok := st.Traits[name]
	return decl, ok
}

func (st *SymbolTable) LookupFunction(name string) (*ast.LambdaExpr, bool) {
	if st == nil {
		return nil, false
	}
	fn, ok := st.Functions[name]
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
	fn, ok := st.Functions[st.functionKey(name, loc)]
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

// RegisterType adds a type declaration to the symbol table
func (st *SymbolTable) RegisterType(node *ast.TypeDeclStmt) error {
	if st.takesPreludeName(node.Name, node.GetLocation()) {
		st.noteShadowed(node.Name, node.GetLocation())
	}
	if err := st.GlobalScope.Define(node); err != nil {
		return err
	}
	st.Types[node.Name] = node
	return nil
}

// RegisterTrait adds a trait declaration to the symbol table.
// Returns an error if a trait with the same name is already registered.
func (st *SymbolTable) RegisterTrait(node *ast.TraitDeclStmt) error {
	if st.takesPreludeName(node.Name, node.GetLocation()) {
		st.noteShadowed(node.Name, node.GetLocation())
	}
	if _, exists := st.Traits[node.Name]; exists {
		return fmt.Errorf("trait %q already defined", node.Name)
	}
	st.Traits[node.Name] = node
	return nil
}

// RegisterFunction adds a function to the symbol table. If node is declared
// `pure`, it is also recorded in PureFuncs (a subset of Functions).
//
// A duplicate is an error, as it already is for a type or a trait. It used to
// overwrite silently, which was harmless while a program was a single file — a
// redeclaration within one file is caught earlier, by the scope's own Define — but with
// modules it meant two modules could each define `helper` and the program would build,
// calling whichever happened to be registered last. A silently wrong program, chosen by
// collection order.
func (st *SymbolTable) RegisterFunction(name string, node *ast.LambdaExpr) error {
	if st.takesPreludeName(name, node.GetLocation()) {
		st.noteShadowed(name, node.GetLocation())
	}
	// A *private* function is keyed by module, so two modules may each declare one
	// without competing. Only exported names share the bare key, where a clash is
	// genuine: a bare reference to either would be ambiguous.
	key := st.functionKey(name, node.GetLocation())
	if existing, exists := st.Functions[key]; exists && existing != node {
		return fmt.Errorf("function %q is already defined at %s", name, describeLocation(existing.GetLocation()))
	}
	st.Functions[key] = node
	if node.IsPure {
		st.PureFuncs[key] = node
	}
	return nil
}

// FunctionKey is the key a function is stored under: its bare name when exported (or
// declared in the entry module), and `<module>::<name>` when private.
//
// Qualifying only the private ones keeps every existing lookup working — an exported
// name is still found by the name the source writes — while giving each module's
// private names a space of their own.
func (st *SymbolTable) FunctionKey(name string, loc ast.Location) string {
	return st.functionKey(name, loc)
}

func (st *SymbolTable) functionKey(name string, loc ast.Location) string {
	if st == nil {
		return name
	}
	module := st.ModuleOfFile[loc.File]
	// A declaration that took a prelude name is qualified whatever its visibility and
	// whichever module made it — including the entry module, which otherwise keeps the
	// bare key. The prelude keeps the bare key, so every module that did *not* shadow
	// the name still finds the prelude's function under it.
	if st.shadowsPrelude(module, name) {
		return qualifiedName(module, name)
	}
	if module == "" {
		return name
	}
	// Exported names live under the bare key so a cross-module reference finds them.
	if decl := st.moduleScope(module); decl != nil {
		if sym, found := decl.LookupLocal(name); found {
			if vd, isVar := sym.(*ast.VarDeclStmt); isVar && !vd.IsPublic {
				return qualifiedName(module, name)
			}
		}
	}
	return name
}

// qualifiedName is the key a declaration gets when the bare name is not its alone.
func qualifiedName(module, name string) string {
	return module + "::" + name
}

// shadowsPrelude reports whether module declares its own `name` over one the prelude
// exports — the case a warning is issued for, and the reason the declaration needs a
// key of its own.
//
// It asks the module's *scope*, not ModuleOf: that map is last-writer-wins, so it
// forgets the prelude ever had the name the moment a user module takes it.
func (st *SymbolTable) shadowsPrelude(module, name string) bool {
	if st == nil || st.PreludeModule == "" || module == st.PreludeModule {
		return false
	}
	return st.PreludeNames[name] && st.ModuleDeclares(module, name)
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

// ShadowedName records a user declaration that took a name the prelude exports.
type ShadowedName struct {
	Name string
	Loc  ast.Location
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

// noteShadowed records the warning, and withdraws the prelude's entry for the name only
// where the namespace it lives in is genuinely program-wide.
//
// A **binding** is not withdrawn any more. The prelude's bindings live in PreludeScope
// and a shadowing declaration lives in its own module's scope under its own
// FunctionKey, so the two coexist and each module reaches the one it should. Deleting
// the prelude's was what made a shadow program-wide: one module's private `unwrapOr`
// left every other module with none at all, and one module's public one handed its own
// to everybody.
//
// A **type** or **trait** still is. Their namespace is program-wide by construction —
// SymbolTable.Types is keyed by bare name, and so is the backend's registry of emitted
// LLVM struct types, which resolves a type reference with no location to say who is
// asking. A program therefore has exactly one `Maybe`, and the shadowing declaration is
// it. Confining a type shadow means giving types per-module identity end to end, which
// is its own piece of work (see todo.md).
//
// Each removal is guarded on the entry actually belonging to the prelude. Deleting by
// name alone is wrong at the point functions are registered: that happens in Finish,
// after every file has been walked, so the user's own declaration is already in place —
// and blindly removing it left the program reporting its own declaration as undefined.
func (st *SymbolTable) noteShadowed(name string, loc ast.Location) {
	st.ShadowedPrelude = append(st.ShadowedPrelude, ShadowedName{Name: name, Loc: loc})
	if sym, ok := st.GlobalScope.Symbols[name]; ok && st.declaredInPrelude(sym.GetLocation()) {
		delete(st.GlobalScope.Symbols, name)
	}
	if decl, ok := st.Types[name]; ok && st.declaredInPrelude(decl.GetLocation()) {
		delete(st.Types, name)
	}
	if decl, ok := st.Traits[name]; ok && st.declaredInPrelude(decl.GetLocation()) {
		delete(st.Traits, name)
	}
}

// declaredInPrelude reports whether a declaration at loc came from the prelude.
func (st *SymbolTable) declaredInPrelude(loc ast.Location) bool {
	return st.PreludeModule != "" && st.ModuleOfFile[loc.File] == st.PreludeModule
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
	scope := NewScope(st.PreludeScope, ScopeModule)
	st.ModuleScopes[module] = scope
	return scope
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
