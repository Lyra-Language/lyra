package ast

import "fmt"

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
func (p *IdentifierPattern) Print(indent string) {
	fmt.Printf("%sIdentifierPattern(%s)", indent, p.GetName())
}

// LiteralPattern represents a literal pattern (matches a value)
type LiteralPattern struct {
	PatternBase
	Value any
}

func (p *LiteralPattern) patternNode()    {}
func (p *LiteralPattern) GetName() string { return fmt.Sprintf("%v", p.Value) }
func (p *LiteralPattern) Print(indent string) {
	fmt.Printf("%sLiteralPattern(%s)", indent, p.GetName())
}

type TuplePattern struct {
	PatternBase
	Elements []Pattern
}

func (p *TuplePattern) patternNode()    {}
func (p *TuplePattern) GetName() string { return fmt.Sprintf("(%v)", p.Elements) }
func (p *TuplePattern) Print(indent string) {
	fmt.Printf("%sTuplePattern {\n", indent)
	for _, element := range p.Elements {
		element.Print(indent + "\t")
		fmt.Printf(",\n")
	}
	fmt.Printf("%s}\n", indent)
}

type ArrayPattern struct {
	PatternBase
	Elements []Pattern
}

func (p *ArrayPattern) patternNode()    {}
func (p *ArrayPattern) GetName() string { return fmt.Sprintf("[%v]", p.Elements) }
func (p *ArrayPattern) Print(indent string) {
	fmt.Printf("%sArrayPattern {\n", indent)
	for _, element := range p.Elements {
		element.Print(indent + "\t")
		fmt.Printf(",\n")
	}
	fmt.Printf("%s}\n", indent)
}

type StructPattern struct {
	PatternBase
	Fields []StructPatternField
}

func (p *StructPattern) patternNode()    {}
func (p *StructPattern) GetName() string { return fmt.Sprintf("{%v}", p.Fields) }
func (p *StructPattern) Print(indent string) {
	fmt.Printf("%sStructPattern {\n", indent)
	for _, field := range p.Fields {
		field.Print(indent + "\t")
	}
	fmt.Printf("%s}\n", indent)
}

type StructPatternField struct {
	PatternBase
	Name    string
	Pattern Pattern
}

func (p *StructPatternField) patternNode()    {}
func (p *StructPatternField) GetName() string { return p.Name }
func (p *StructPatternField) Print(indent string) {
	fmt.Printf("%sStructPatternField {\n", indent)
	fmt.Printf("%s\tname: %s\n", indent, p.Name)
	if p.Pattern != nil {
		fmt.Printf("%s\tpattern: {\n", indent)
		p.Pattern.Print(indent + "\t\t")
		fmt.Printf("\n%s\t}\n", indent)
	} else {
		fmt.Printf("%s\tpattern: nil\n", indent)
	}
	fmt.Printf("%s}\n", indent)
}

type RestPattern struct {
	PatternBase
	Identifier string
}

func (p *RestPattern) patternNode()    {}
func (p *RestPattern) GetName() string { return fmt.Sprintf("...%s", p.Identifier) }
func (p *RestPattern) Print(indent string) {
	fmt.Printf("%sRestPattern(%s)", indent, p.Identifier)
}

type WildcardPattern struct {
	PatternBase
}

func (p *WildcardPattern) patternNode()    {}
func (p *WildcardPattern) GetName() string { return "_" }
func (p *WildcardPattern) Print(indent string) {
	fmt.Printf("%sWildcardPattern {\n", indent)
}
