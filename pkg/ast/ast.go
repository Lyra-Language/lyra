package ast

// Location tracks where a symbol or ast node was defined
type Location struct {
	File      string
	StartLine int
	StartCol  int
	EndLine   int
	EndCol    int
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

func (p *Program) node()                 {}
func (p *Program) GetLocation() Location { return p.Location }
