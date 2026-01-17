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

func (l *Location) ToString() string {
	return fmt.Sprintf("%s:%d:%d-%d:%d", l.File, l.StartLine, l.StartCol, l.EndLine, l.EndCol)
}

// AstNode is the interface for all AST nodes
type AstNode interface {
	node()
	GetLocation() Location
	Print(indent string)
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
func (a *AstBase) Print(indent string)   {}

type Program struct {
	AstBase
	Statements []AstNode
}

func (p *Program) node()                 {}
func (p *Program) GetLocation() Location { return p.Location }
func (p *Program) Print(indent string) {
	fmt.Printf("%sProgram(%d statements)\n", indent, len(p.Statements))
	for _, statement := range p.Statements {
		statement.Print(indent + "  ")
	}
}
