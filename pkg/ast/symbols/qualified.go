package symbols

import (
	"strings"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/types"
)

// A name reached through a namespace import (10/29): `shapes.Circle(2)`, `shapes.Pt { … }`,
// `shapes.Dot =>`, `p: shapes.Pt`. The grammar reads `shapes.Circle` as one token, so the
// name arrives dotted, and is resolved two ways.
//
//   - **A type keeps its dotted name.** `declKeyIn` reads `ns.Name` as the namespace's
//     module's own `Name`, so every lookup a type takes — resolution, the written-name
//     gate, layout — finds it with no change of its own.
//   - **A constructor, a struct literal's name and a pattern's are erased to the bare
//     name** (driver, eraseQualifiedNames), because a constructor is compared by its own
//     name in a dozen places. The erased node's location is recorded with the module the
//     qualifier named, and the two resolvers a construction or pattern asks — declKey and
//     DeclaringDataType — answer from that module at that location, for that name only.

// QualifiedRef is the module a qualified name named, and the bare name it was erased to.
type QualifiedRef struct {
	Module string
	Name   string
}

// RecordQualified notes that the node at loc wrote module's name through a namespace.
func (st *SymbolTable) RecordQualified(loc ast.Location, module, name string) {
	if st.qualified == nil {
		st.qualified = map[ast.Location]QualifiedRef{}
	}
	st.qualified[loc] = QualifiedRef{Module: module, Name: name}
}

// QualifiedAt reports the module the node at loc named name through, when it did.
func (st *SymbolTable) QualifiedAt(loc ast.Location, name string) (string, bool) {
	if st == nil {
		return "", false
	}
	q, ok := st.qualified[loc]
	if !ok || q.Name != name {
		return "", false
	}
	return q.Module, true
}

// RecordQualifiedFailure notes that the qualified name at loc was refused and reported
// (no such namespace or member), so a pass meeting the name still dotted reports nothing
// more about it.
func (st *SymbolTable) RecordQualifiedFailure(loc ast.Location) {
	if st.qualifiedFailed == nil {
		st.qualifiedFailed = map[ast.Location]bool{}
	}
	st.qualifiedFailed[loc] = true
}

// QualifiedFailed reports whether the qualified name at loc was refused already.
func (st *SymbolTable) QualifiedFailed(loc ast.Location) bool {
	return st != nil && st.qualifiedFailed[loc]
}

// SplitQualified splits a dotted name, `shapes.Pt`, into its namespace and its name.
func SplitQualified(name string) (namespace, member string, ok bool) {
	namespace, member, ok = strings.Cut(name, ".")
	return namespace, member, ok && namespace != "" && member != ""
}

// NamespaceIn is the module module's imports bind to namespace (`import shapes`, `import
// geo.shapes as s`), when one does.
func (st *SymbolTable) NamespaceIn(module, namespace string) (string, bool) {
	if st == nil {
		return "", false
	}
	for _, imp := range st.Imports[module] {
		if imp.IsNamespace() && imp.Alias == namespace {
			return imp.Path, true
		}
	}
	return "", false
}

// qualifiedKey is the key a dotted name means from module: the namespace's module's own
// declaration, provided it exports it. A private one, a namespace module does not import,
// or a module without the name leave it unresolved, for the caller's "unknown" path.
func (st *SymbolTable) qualifiedKey(module, name string) (string, bool) {
	namespace, member, ok := SplitQualified(name)
	if !ok {
		return "", false
	}
	path, ok := st.NamespaceIn(module, namespace)
	if !ok || !st.ModuleExports(path, member) {
		return "", false
	}
	return qualifiedName(path, member), true
}

// QualifiedTypeKey is the declaration key a dotted type name (`collections.HashMap`) means
// from module, with the bare name it is declared under; ok=false for a bare name or one
// whose namespace does not resolve it.
func (st *SymbolTable) QualifiedTypeKey(module, name string) (key, member string, ok bool) {
	if st == nil {
		return "", "", false
	}
	key, ok = st.qualifiedKey(module, name)
	if !ok {
		return "", "", false
	}
	_, member, _ = SplitQualified(name)
	return key, member, true
}

// DataTypeWithConstructorIn is module's own data type declaring a constructor named
// ctorName — the owner a qualified constructor (`shapes.Circle`) means.
func (st *SymbolTable) DataTypeWithConstructorIn(module, ctorName string) (*ast.TypeDeclStmt, bool) {
	if st == nil {
		return nil, false
	}
	var found *ast.TypeDeclStmt
	for _, decl := range st.Types {
		if st.ModuleOfFile[decl.GetLocation().File] != module {
			continue
		}
		dt, ok := decl.Type.(types.DataType)
		if !ok {
			continue
		}
		for _, c := range dt.Constructors {
			if c.Name == ctorName {
				// Two of one module's types cannot share a constructor name, so the first
				// is the only; the comparison keeps the answer independent of map order
				// should that ever change.
				if found == nil || decl.Name < found.Name {
					found = decl
				}
			}
		}
	}
	return found, found != nil
}
