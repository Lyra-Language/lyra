package symbols

import "github.com/Lyra-Language/lyra/pkg/ast"

// Where each **named type reference** appears in the source — `Point` in `(p: Point)`,
// in `-> Maybe<Point>`, in a field's declared type, in a `where` bound.
//
// **A side table, because a type cannot carry its own position.** `TypesEqual` is
// structural, so a `Location` field on `types.UnresolvedType` would make `Point` written
// on one line unequal to `Point` written on another unless every comparison learned to
// skip it — the drift hazard rule 8 is about. The language already settled the same
// question for documentation: docs attach to declarations, not to types, so that two
// structurally identical types stay equal. This is that principle applied to positions.
//
// **What it is for.** Every position-based editor feature starts from `findExprAtPos`,
// which walks *expressions* — and a type in a type position is not an expression at all,
// so go-to-definition and hover found nothing on `(p: Point)` while working perfectly on
// `Point { … }`. Nothing here resolves anything: it answers *which name is under the
// cursor*, and the resolution stays `LookupTypeFrom`, the one answer that already exists.
type TypeRefTable struct {
	// Keyed by file, because every consumer is asking about one open document and a
	// program's references otherwise accumulate across the whole import graph.
	byFile map[string][]TypeRef
}

// TypeRef is one written occurrence of a type's name.
type TypeRef struct {
	Name string
	Loc  ast.Location
}

func NewTypeRefTable() *TypeRefTable {
	return &TypeRefTable{byFile: map[string][]TypeRef{}}
}

// Add records a reference. A nil receiver is a no-op, so a caller with no table — a test
// collecting a snippet, say — needs no guard of its own.
//
// An **empty file name is a real key**, not a reason to skip: a snippet with no path on
// disk is exactly what a test collects and what the LSP analyzes for a buffer that has
// never been saved, and `diagnosticsFor` already treats "" as "this document".
func (t *TypeRefTable) Add(name string, loc ast.Location) {
	if t == nil || name == "" {
		return
	}
	t.byFile[loc.File] = append(t.byFile[loc.File], TypeRef{Name: name, Loc: loc})
}

// At returns the reference the cursor is inside, if any. Line and column are 1-based, the
// convention `ast.Location` uses.
//
// **The innermost match wins.** `Maybe<Point>` records both names, and their spans nest —
// the head's does not, but an `array_type`'s and a `weak_type`'s do — so a cursor inside
// the argument must resolve to the argument rather than to whatever encloses it. Scanning
// for the shortest containing span is what makes that hold without the table having to
// know which kinds nest.
func (t *TypeRefTable) At(file string, line, col int) (TypeRef, bool) {
	if t == nil {
		return TypeRef{}, false
	}
	var best TypeRef
	found := false
	for _, ref := range t.byFile[file] {
		if !contains(ref.Loc, line, col) {
			continue
		}
		if !found || shorter(ref.Loc, best.Loc) {
			best, found = ref, true
		}
	}
	return best, found
}

// Refs returns every reference recorded for a file, in source order of collection. For
// tests and for a feature that wants them all rather than one.
func (t *TypeRefTable) Refs(file string) []TypeRef {
	if t == nil {
		return nil
	}
	return t.byFile[file]
}

func contains(loc ast.Location, line, col int) bool {
	if line < loc.StartLine || line > loc.EndLine {
		return false
	}
	if line == loc.StartLine && col < loc.StartCol {
		return false
	}
	// EndCol is exclusive, matching ast.Location everywhere else.
	if line == loc.EndLine && col >= loc.EndCol {
		return false
	}
	return true
}

func shorter(a, b ast.Location) bool {
	if a.StartLine != b.StartLine || a.EndLine != b.EndLine {
		return (a.EndLine - a.StartLine) < (b.EndLine - b.StartLine)
	}
	return (a.EndCol - a.StartCol) < (b.EndCol - b.StartCol)
}

// clone deep-copies the table, so a clone's collection cannot append into the master's
// slices. A nil receiver clones to nil, matching every other field's behaviour.
func (t *TypeRefTable) clone() *TypeRefTable {
	if t == nil {
		return nil
	}
	out := NewTypeRefTable()
	for file, refs := range t.byFile {
		out.byFile[file] = append([]TypeRef(nil), refs...)
	}
	return out
}
