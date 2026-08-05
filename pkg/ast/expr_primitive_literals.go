package ast

import (
	"fmt"
	"strings"

	"github.com/Lyra-Language/lyra/pkg/types"
)

type PrimitiveLiteralValue interface {
	primitiveLiteralValueNode()
	GetName() string
}

type IntegerLiteralExpr struct {
	ExprBase
	Value int64
	Base  IntegerBase
	// Unsigned marks a literal whose magnitude exceeds int64's range but fits u64
	// (e.g. 18446744073709551615). Value then holds the *bit pattern*
	// (int64(uint64value), so it reads as negative), and GetType reports a concrete
	// u64 — the literal's only valid type, so it isn't adaptable like an ordinary
	// untyped literal. The backend lowers Value directly (constant.NewInt gives the
	// right bits). Use UnsignedValue for the true magnitude.
	Unsigned bool
}

func (i *IntegerLiteralExpr) primitiveLiteralValueNode() {}
func (i *IntegerLiteralExpr) LiteralText() string        { return i.decimalString() }

// UnsignedValue returns the literal's magnitude reinterpreted as unsigned — the
// true value of an Unsigned (large-u64) literal, or just uint64(Value) otherwise.
func (i *IntegerLiteralExpr) UnsignedValue() uint64 { return uint64(i.Value) }

// decimalString renders the literal's value, honoring the unsigned bit pattern.
func (i *IntegerLiteralExpr) decimalString() string {
	if i.Unsigned {
		return fmt.Sprintf("%d", i.UnsignedValue())
	}
	return fmt.Sprintf("%d", i.Value)
}

// GetName renders the literal **as it was written**, base and all — `0xFF`, not
// `IntegerLiteralExpr(255, Base: 16)`.
//
// Every GetName on an expression is a source rendering, composed into diagnostics by
// its parents (a match arm builds `match <pattern> { <body> }` out of them). The
// literals were the family that dumped their Go type instead, which produced messages
// like `expected array pattern, got IntegerLiteralExpr(0, Base: 10)..<=
// IntegerLiteralExpr(10, Base: 10)` — a rendering of the compiler's internals handed to
// someone reading their own program.
func (i *IntegerLiteralExpr) GetName() string {
	switch i.Base {
	case IntegerBase16:
		return fmt.Sprintf("0x%X", i.UnsignedValue())
	case IntegerBase8:
		return fmt.Sprintf("0o%o", i.UnsignedValue())
	case IntegerBase2:
		return fmt.Sprintf("0b%b", i.UnsignedValue())
	}
	return i.decimalString()
}

func (i *IntegerLiteralExpr) GetType() types.Type {
	if i.Unsigned {
		return types.PrimitiveType{Name: types.UInt64}
	}
	return types.PrimitiveType{Name: types.UntypedInt}
}
func (i *IntegerLiteralExpr) Int64() (int64, bool)     { return i.Value, true }
func (i *IntegerLiteralExpr) Float64() (float64, bool) { return 0, false }
func (i *IntegerLiteralExpr) ConstraintString() string { return i.decimalString() }

type IntegerBase int // 10, 8, 16, 2

const (
	IntegerBase2  IntegerBase = 2
	IntegerBase8  IntegerBase = 8
	IntegerBase10 IntegerBase = 10
	IntegerBase16 IntegerBase = 16
)

type FloatLiteralExpr struct {
	ExprBase
	Value float64
}

func (f *FloatLiteralExpr) primitiveLiteralValueNode() {}
func (f *FloatLiteralExpr) LiteralText() string        { return fmt.Sprintf("%g", f.Value) }

func (f *FloatLiteralExpr) GetName() string {
	return f.LiteralText() // %g — no trailing zeros
}

func (f *FloatLiteralExpr) GetType() types.Type      { return types.PrimitiveType{Name: types.UntypedFloat} }
func (f *FloatLiteralExpr) Int64() (int64, bool)     { return 0, false }
func (f *FloatLiteralExpr) Float64() (float64, bool) { return f.Value, true }
func (f *FloatLiteralExpr) ConstraintString() string { return fmt.Sprintf("%g", f.Value) }

type StringLiteralExpr struct {
	ExprBase
	Value string
}

func (s *StringLiteralExpr) primitiveLiteralValueNode() {}
func (s *StringLiteralExpr) LiteralText() string        { return fmt.Sprintf("%q", s.Value) }

func (s *StringLiteralExpr) GetType() types.Type { return types.PrimitiveType{Name: types.String} }

// Quoted, so a message naming a string cannot be misread as naming a binding — and so
// an empty or space-only literal is visible at all.
func (s *StringLiteralExpr) GetName() string { return s.LiteralText() }

// InterpolatedStringExpr represents a double-quoted string with one or more
// `${expr}` interpolations. Each segment is either a *StringLiteralExpr holding
// a literal content chunk or an arbitrary Expression. Interpolated strings are
// not compile-time constants, so they do not implement LiteralUnionValue.
type InterpolatedStringExpr struct {
	ExprBase
	Segments []Expression
}

// Rendered with its interpolations back in place: `"a${b}c"`. A literal chunk prints its
// text, anything else prints as `${…}` around its own rendering.
func (s *InterpolatedStringExpr) GetName() string {
	var b strings.Builder
	b.WriteByte('"')
	for _, seg := range s.Segments {
		if lit, ok := seg.(*StringLiteralExpr); ok {
			b.WriteString(lit.Value)
			continue
		}
		b.WriteString("${")
		if seg != nil {
			b.WriteString(seg.GetName())
		}
		b.WriteString("}")
	}
	b.WriteByte('"')
	return b.String()
}

type BooleanLiteralExpr struct {
	ExprBase
	Value bool
}

func (b *BooleanLiteralExpr) primitiveLiteralValueNode() {}
func (b *BooleanLiteralExpr) LiteralText() string        { return fmt.Sprintf("%t", b.Value) }

func (b *BooleanLiteralExpr) GetType() types.Type { return types.PrimitiveType{Name: types.Boolean} }
func (b *BooleanLiteralExpr) GetName() string     { return b.LiteralText() }

type CharacterLiteralExpr struct {
	ExprBase
	Value rune
}

func (c *CharacterLiteralExpr) primitiveLiteralValueNode() {}
func (c *CharacterLiteralExpr) LiteralText() string        { return fmt.Sprintf("%q", c.Value) }

func (c *CharacterLiteralExpr) GetType() types.Type { return types.PrimitiveType{Name: types.Rune} }
func (c *CharacterLiteralExpr) GetName() string     { return c.LiteralText() }

type RegexLiteralExpr struct {
	ExprBase
	Pattern string
}

// `r"…"`, the spelling since 07/29. The old `r/…/` form is gone from the language, so a
// message using it would send the reader to write something that no longer parses.
func (r *RegexLiteralExpr) GetName() string {
	// Verbatim, not %q: a regex is mostly backslashes, and Go quoting would double every
	// one of them — `r"\\d+"` for what the author wrote as `r"\d+"`.
	return fmt.Sprintf(`r"%s"`, r.Pattern)
}
