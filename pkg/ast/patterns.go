package ast

import "fmt"

// Pattern is the interface for all pattern AST nodes
type Pattern interface {
	patternNode()
	GetLocation() Location
	GetName() string
}

// PatternBase is embedded in all pattern types
type PatternBase struct {
	Location Location
}

func (p *PatternBase) patternNode()          {}
func (p *PatternBase) GetLocation() Location { return p.Location }

// IdentifierPattern represents an identifier pattern (binds a name)
type IdentifierPattern struct {
	PatternBase
	Name string
}

func (p *IdentifierPattern) GetName() string { return p.Name }

// LiteralPattern represents a literal pattern (matches a value)
type LiteralPattern struct {
	PatternBase
	Value any
}

func (p *LiteralPattern) GetName() string { return fmt.Sprintf("%v", p.Value) }

// TODO: add other patterns (tuple, struct, array, etc.)
