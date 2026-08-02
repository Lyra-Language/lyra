package ast

import (
	"fmt"
	"strconv"
)

// Pattern is the interface for all pattern AST nodes
type Pattern interface {
	AstNode
	patternNode()
	GetName() string
}

// PatternBase is embedded in all pattern types
type PatternBase struct {
	AstBase
}

func (p *PatternBase) patternNode()    {}
func (p *PatternBase) GetName() string { return "" }

// IdentifierPattern represents an identifier pattern (binds a name)
type IdentifierPattern struct {
	PatternBase
	Name string
}

func (p *IdentifierPattern) patternNode()    {}
func (p *IdentifierPattern) GetName() string { return p.Name }

// RunePatternValue is the decoded code point of a character-literal match
// pattern (`'a' => …`). It is stored in LiteralPattern.Value so a rune pattern is
// distinguishable from the raw source text a numeric/string/bool literal pattern
// stores (those keep a string), while its Stringer renders it back as a quoted
// character in diagnostics (so `%s`/`%v` on LiteralPattern.Value stay readable).
type RunePatternValue rune

func (r RunePatternValue) String() string { return strconv.QuoteRune(rune(r)) }

// LiteralPattern represents a literal pattern (matches a value). Value holds the
// raw source text (a string) for a numeric/string/bool literal, or a
// RunePatternValue for a character literal.
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

// StructPattern destructures a struct value by field. Name is the struct type it
// names ("" for the anonymous/brace-only form `{ x, y }`); the named form
// `Pt { x, y }` is produced by the collector's reclassifyStructPatterns pass,
// which rewrites a DataPattern whose name is a struct type into this node (so
// struct patterns and data-constructor patterns are distinct AST nodes even
// though `Pt { … }` and `Node { … }` are syntactically identical).
type StructPattern struct {
	PatternBase
	Name   string
	Fields []StructPatternField
}

func (p *StructPattern) patternNode() {}
func (p *StructPattern) GetName() string {
	if p.Name != "" {
		return fmt.Sprintf("%s{%v}", p.Name, p.Fields)
	}
	return fmt.Sprintf("{%v}", p.Fields)
}

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

// RangePattern is `0..=9`, `-128..<0`, and the open forms `0..` and `..<0`.
//
// Exactly one of Start/End may be nil — a bare `..` does not parse — and a nil
// one means an *open* bound, i.e. the scrutinee type's own limit, not a missing
// one. EndOperator is "" only when End is nil; an end bound without an operator
// is rejected at collection (lyra-E032) rather than defaulting to inclusive.
type RangePattern struct {
	PatternBase
	Start       Expression
	End         Expression
	EndOperator string
}

func (p *RangePattern) patternNode() {}

// GetName renders the pattern back to its source form.
//
// The operator belongs *before* the end bound: this printed `0..9=` for `0..=9`
// until 08/01, putting the operator after the bound it qualifies. It reaches
// users — GetName is what diagnostics interpolate — though not golden files,
// which is why it survived.
func (p *RangePattern) GetName() string {
	start, end := "", ""
	if p.Start != nil {
		start = p.Start.GetName()
	}
	if p.End != nil {
		end = p.End.GetName()
	}
	return fmt.Sprintf("%s..%s%s", start, p.EndOperator, end)
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
