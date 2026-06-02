package ast

import "fmt"

// Location tracks where a symbol or ast node was defined
type Location struct {
	File      string
	StartLine int
	StartCol  int
	EndLine   int
	EndCol    int
}

// String returns the full location including the file path, e.g. "file.lyra:5:3-5:10".
// It is defined on a value receiver so that fmt verbs like %v and %s work
// whether the caller has a Location value or a *Location pointer.
func (l Location) String() string {
	return fmt.Sprintf("%s:%d:%d-%d:%d", l.File, l.StartLine, l.StartCol, l.EndLine, l.EndCol)
}

// Pretty returns a compact, human-readable position without the file path —
// suitable for inline error messages where the file is already implied.
// Single-line spans are formatted as "5:3"; multi-line spans as "5:3-6:1".
func (l Location) Pretty() string {
	if l.StartLine == l.EndLine {
		return fmt.Sprintf("%d:%d", l.StartLine, l.StartCol)
	}
	return fmt.Sprintf("%d:%d-%d:%d", l.StartLine, l.StartCol, l.EndLine, l.EndCol)
}

// AstNode is the interface for all AST nodes
type AstNode interface {
	node()
	GetLocation() Location
}

// Named is the interface for AST nodes that have a name (for symbol table lookup)
type Named interface {
	AstNode
	GetName() string
}

type AstBase struct {
	Location Location
}

func (a *AstBase) node()                 {}
func (a *AstBase) GetLocation() Location { return a.Location }

type Program struct {
	AstBase
	Statements []AstNode
}
