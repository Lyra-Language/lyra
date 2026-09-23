package ast

import (
	"fmt"
	"strconv"
	"strings"
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

func (p *LiteralPattern) patternNode() {}

// GetName renders the literal in source form. Value is `any`, so a plain %v would print
// whatever Go makes of it — for an expression that is the struct, which is how
// `IntegerLiteralExpr(0, Base: 10)` reached users through the range pattern below.
func (p *LiteralPattern) GetName() string {
	if node, ok := p.Value.(AstNode); ok {
		if named, ok := node.(Named); ok {
			return named.GetName()
		}
	}
	return fmt.Sprintf("%v", p.Value)
}

type TuplePattern struct {
	PatternBase
	Elements []Pattern
}

func (p *TuplePattern) patternNode()    {}
func (p *TuplePattern) GetName() string { return "(" + patternNames(p.Elements) + ")" }

type ArrayPattern struct {
	PatternBase
	Elements []Pattern
}

func (p *ArrayPattern) patternNode()    {}
func (p *ArrayPattern) GetName() string { return "[" + patternNames(p.Elements) + "]" }

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

// GetName renders the pattern as source: `Pt { x, y: 0 }`. It formatted the field slice
// with %v until 09/13, which printed Go's view of it — locations and pointers — into
// diagnostics that name a pattern.
func (p *StructPattern) GetName() string {
	parts := make([]string, 0, len(p.Fields))
	for _, f := range p.Fields {
		switch {
		case f.Name == "..." && f.Pattern != nil:
			parts = append(parts, f.Pattern.GetName())
		case f.Pattern == nil:
			parts = append(parts, f.Name)
		default:
			parts = append(parts, f.Name+": "+f.Pattern.GetName())
		}
	}
	body := "{ " + strings.Join(parts, ", ") + " }"
	if p.Name != "" {
		return p.Name + " " + body
	}
	return body
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

func (p *DataPattern) patternNode() {}

// GetName renders the pattern as source — `Some(v)`, `Some v`, `None` — so a diagnostic
// naming it shows what was written rather than only the constructor.
func (p *DataPattern) GetName() string {
	switch inner := p.Pattern.(type) {
	case nil:
		return p.Name
	case *TuplePattern:
		return p.Name + inner.GetName()
	default:
		return p.Name + " " + inner.GetName()
	}
}

type RestPattern struct {
	PatternBase
	Identifier string
}

func (p *RestPattern) patternNode()    {}
func (p *RestPattern) GetName() string { return fmt.Sprintf("...%s", p.Identifier) }

// RangePattern is `0..<=9`, `-128..<0`, and the open forms `0..` and `..<0`.
//
// Exactly one of Start/End may be nil — a bare `..` does not parse — and a nil
// one means an *open* bound, i.e. the scrutinee type's own limit, not a missing
// one. EndOperator is "" only when End is nil; an end bound without an operator
// is rejected at collection (lyra-E032) rather than defaulting to inclusive.
//
// A bound may be written as a `const` (`LOW..<=HIGH`). The typechecker folds such a bound
// to its literal in place, so every later reader of Start/End sees a number; ConstBounds
// keeps the names as written, with their locations, for the passes that ask which names a
// program reads (RangeBoundNames) and the editor features that ask what is at a position.
type RangePattern struct {
	PatternBase
	Start       Expression
	End         Expression
	EndOperator string
	ConstBounds []*IdentifierExpr
}

func (p *RangePattern) patternNode() {}

// GetName renders the pattern back to its source form.
//
// The operator belongs *before* the end bound: this printed `0..9=` for `0..<=9`
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

// OrPattern matches when **any** alternative matches: `1 | 2 | 3`, `"get" | "post"`,
// `'a'..<='f' | 'A'..<='F'`.
//
// **Alternatives are literals and ranges only**, which the grammar enforces and which is a
// language decision rather than a limit of this node. An alternative that *binds* raises a
// question every language with or-patterns answers explicitly — Rust requires every
// alternative to bind the same names at the same types — and nothing needs it yet: what it
// was wanted for is a set of literals sharing one arm. So no alternative binds, which is
// why EachPatternBinding has nothing to do here and why the exhaustiveness matrix can treat
// the node as the union of its rows.
type OrPattern struct {
	PatternBase
	Alternatives []Pattern
}

func (p *OrPattern) patternNode() {}

// GetName renders the pattern back to its source form.
func (p *OrPattern) GetName() string {
	parts := make([]string, len(p.Alternatives))
	for i, alt := range p.Alternatives {
		parts[i] = alt.GetName()
	}
	return strings.Join(parts, " | ")
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

func (p *RegexPattern) patternNode() {}

// `r"…"` — the spelling since 07/29. `r/…/` no longer parses, so a message using it would
// send the reader to write something the grammar rejects.
func (p *RegexPattern) GetName() string { return fmt.Sprintf(`r"%s"`, p.Pattern) }

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

// patternNames renders a pattern list as source: `a, b, c`. Formatting the slice with %v
// prints Go's view of it — a list of pointers — which is never what a diagnostic wants.
func patternNames(elements []Pattern) string {
	parts := make([]string, 0, len(elements))
	for _, e := range elements {
		if e == nil {
			parts = append(parts, "_")
			continue
		}
		parts = append(parts, e.GetName())
	}
	return strings.Join(parts, ", ")
}
