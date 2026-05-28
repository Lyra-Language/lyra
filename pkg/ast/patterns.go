package ast

import (
	"fmt"
)

// Pattern is the interface for all pattern AST nodes
type Pattern interface {
	AstNode
	patternNode()
	GetName() string
}

// PatternBase is embedded in all pattern types
type PatternBase struct {
	Location Location
}

func (p *PatternBase) node()                 {}
func (p *PatternBase) patternNode()          {}
func (p *PatternBase) GetLocation() Location { return p.Location }
func (p *PatternBase) GetName() string       { return "" }

// IdentifierPattern represents an identifier pattern (binds a name)
type IdentifierPattern struct {
	PatternBase
	Name string
}

func (p *IdentifierPattern) patternNode()    {}
func (p *IdentifierPattern) GetName() string { return p.Name }

// LiteralPattern represents a literal pattern (matches a value)
type LiteralPattern struct {
	PatternBase
	Value any
}

func (p *LiteralPattern) patternNode()    {}
func (p *LiteralPattern) GetName() string { return fmt.Sprintf("%v", p.Value) }

type TuplePattern struct {
	PatternBase
	Elements []Pattern
}

func (p *TuplePattern) patternNode()    {}
func (p *TuplePattern) GetName() string { return fmt.Sprintf("(%v)", p.Elements) }

type ArrayPattern struct {
	PatternBase
	Elements []Pattern
}

func (p *ArrayPattern) patternNode()    {}
func (p *ArrayPattern) GetName() string { return fmt.Sprintf("[%v]", p.Elements) }

type StructPattern struct {
	PatternBase
	Fields []StructPatternField
}

func (p *StructPattern) patternNode()    {}
func (p *StructPattern) GetName() string { return fmt.Sprintf("{%v}", p.Fields) }

type StructPatternField struct {
	PatternBase
	Name    string
	Pattern Pattern
}

func (p *StructPatternField) patternNode()    {}
func (p *StructPatternField) GetName() string { return p.Name }

type DataPattern struct {
	PatternBase
	Name    string
	Pattern Pattern
}

func (p *DataPattern) patternNode()    {}
func (p *DataPattern) GetName() string { return p.Name }

type RestPattern struct {
	PatternBase
	Identifier string
}

func (p *RestPattern) patternNode()    {}
func (p *RestPattern) GetName() string { return fmt.Sprintf("...%s", p.Identifier) }

type RangePattern struct {
	PatternBase
	Start       Expression
	End         Expression
	EndOperator string
}

func (p *RangePattern) patternNode() {}
func (p *RangePattern) GetName() string {
	start, end := "", ""
	if p.Start != nil {
		start = p.Start.GetName()
	}
	if p.End != nil {
		end = p.End.GetName()
	}
	return fmt.Sprintf("%s..%s%s", start, end, p.EndOperator)
}

type WildcardPattern struct {
	PatternBase
}

func (p *WildcardPattern) patternNode()    {}
func (p *WildcardPattern) GetName() string { return "_" }

// RegexPattern matches a string by regex language membership. Used only in
// match arms whose scrutinee is `string`. The Pattern field is the raw regex
// body with the surrounding `r/` and `/` delimiters stripped, matching the
// convention of RegexLiteralExpr.
type RegexPattern struct {
	PatternBase
	Pattern string
}

func (p *RegexPattern) patternNode()    {}
func (p *RegexPattern) GetName() string { return fmt.Sprintf("r/%s/", p.Pattern) }

// BindingPattern binds a name to the whole matched value while also matching
// an inner pattern. Written `name @ pattern` — equivalent to Rust's @ bindings
// or Haskell's as-patterns. Both `name` and the variables inside `pattern` are
// bound in the arm/body.
type BindingPattern struct {
	PatternBase
	Name    string
	Pattern Pattern
}

func (p *BindingPattern) patternNode()    {}
func (p *BindingPattern) GetName() string { return fmt.Sprintf("%s @ %s", p.Name, p.Pattern.GetName()) }
