package llvm

import (
	"strings"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
	"github.com/Lyra-Language/lyra/pkg/typetable"
)

// Per-module type identity, the backend half.
//
// A type name is not program-wide: two modules may each declare a private `Point`, and a
// module may declare its own `Maybe` over the prelude's. The symbol table settles which
// declaration a name means with declKey — bare when exported, `<module>::<name>` when
// private or shadowing a prelude name — and `l.structTypes` has to be keyed the same way,
// or the second `Point` finds the first one's registered LLVM struct and is lowered
// against the wrong layout. (It was: two same-named private structs emitted one `%Point`
// carrying the union of both field lists, which clang rejected as a redefinition.)
//
// **The asking module is ambient rather than threaded**, unlike the function path's
// `funcKey(name, loc)`. It has to be: a resolved `NamedStructType` reaching `lowerType`
// carries only a name — the location where it was written is long gone — and lowerType is
// reached from essentially every expression. What the backend does have is the item it is
// currently lowering, which belongs to exactly one module, and every type reference in
// that item is made *from* that module. So the module is set once per top-level item and
// read by the key function, which then asks the symbol table — the same authority the
// front end asks, so the two cannot disagree about which declaration a name means.

// enterModuleOf sets the location lookups are made *from* — a position inside the item
// being lowered — and returns a function restoring the previous one. Save/restore rather
// than assignment because lowering nests: a field of `one`'s struct may materialize a
// generic declared in `two`.
//
// A location rather than a module path, so the backend deals in the same currency as
// every other caller (LookupTypeFrom, ownership.OwnsManaged) and the symbol table keeps
// sole authority over the file → module step.
func (l *lowerer) enterModuleOf(loc ast.Location) func() {
	previous := l.currentLoc
	l.currentLoc = loc
	return func() { l.currentLoc = previous }
}

// typeKey is the key a named type is registered and found under, as the code currently
// being lowered sees it.
func (l *lowerer) typeKey(name string) string {
	if l.res == nil || l.res.SymbolTable == nil {
		return name
	}
	return l.res.SymbolTable.TypeKey(name, l.currentLoc)
}

// llvmTypeName is the name an emitted LLVM type definition carries.
//
// A key equal to the declared name — every exported type, and every type in a
// single-module program — keeps that name verbatim, so the emitted IR is unchanged for
// everything that was already expressible. Only a module-qualified key is mangled, since
// `::` would have to be quoted in LLVM's syntax and the point of the name is to be
// readable in a dump.
func llvmTypeName(key, name string) string {
	if key == name {
		return name
	}
	return mangleTypeKey(key)
}

// lookupTypeDecl resolves a named type to its declaration as the module being lowered
// sees it. The backend's equivalent of the front end's LookupTypeFrom — and the reason a
// bare SymbolTable.LookupType is wrong here: it cannot see a private declaration at all,
// and hands back a same-named one from another module when there is one.
func (l *lowerer) lookupTypeDecl(name string) (*ast.TypeDeclStmt, bool) {
	if l.res == nil || l.res.SymbolTable == nil {
		return nil, false
	}
	return l.res.SymbolTable.LookupTypeFrom(name, l.currentLoc)
}

// instantiationSymbol is the name a generic instantiation is registered under —
// `Box$i64`, prefixed by the declaring module when the declaration is not program-wide,
// so `one`'s private `Box<i64>` and `two`'s are two layouts rather than one.
//
// An instantiation is registered under this symbol as both key and name: it is already
// mangled, already unique, and unlike a plain declaration there is no separate "readable"
// spelling to preserve.
func (l *lowerer) instantiationSymbol(p types.ParameterizedType) string {
	symbol := typetable.TypeSymbol(p.Name, p.TypeArguments)
	key := l.typeKey(p.Name)
	if key == p.Name {
		return symbol
	}
	return mangleTypeKey(strings.TrimSuffix(key, p.Name)) + symbol
}
