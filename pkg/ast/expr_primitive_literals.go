package ast

import (
	"fmt"
	"math/big"
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
	// Wide holds the magnitude of a literal that exceeds **both** int64 and u64 —
	// the 65-to-128-bit range only `i128`/`u128` can hold. It is nil for every
	// literal that fits either, which is nearly all of them, so the reflection
	// printer omits it and existing golden output is unchanged.
	//
	// When it is set it is the **only** true value: `Value` is 0 and means nothing.
	// Read it through BigValue, which answers for all three representations, rather
	// than reaching for `Value` — that field is exact only when Wide is nil, and the
	// accessors below are where that invariant is enforced.
	Wide *big.Int
}

func (i *IntegerLiteralExpr) primitiveLiteralValueNode() {}
func (i *IntegerLiteralExpr) LiteralText() string        { return i.decimalString() }

// UnsignedValue returns the literal's magnitude reinterpreted as unsigned — the
// true value of an Unsigned (large-u64) literal, or just uint64(Value) otherwise.
//
// It is **not** meaningful for a Wide literal, whose magnitude does not fit 64 bits;
// BigValue is the accessor that answers for every case.
func (i *IntegerLiteralExpr) UnsignedValue() uint64 { return uint64(i.Value) }

// BigValue is the literal's true magnitude whatever its representation — the one
// accessor a consumer that must be right for a 128-bit literal should use.
func (i *IntegerLiteralExpr) BigValue() *big.Int {
	if i.Wide != nil {
		return new(big.Int).Set(i.Wide)
	}
	if i.Unsigned {
		return new(big.Int).SetUint64(i.UnsignedValue())
	}
	return big.NewInt(i.Value)
}

// IsWide reports whether the literal exceeds 64 bits, i.e. whether `Value` is
// meaningless for it. The predicate every site that reads `Value` directly should be
// guarded by, in the places where a wrong answer would be silent.
func (i *IntegerLiteralExpr) IsWide() bool { return i.Wide != nil }

// decimalString renders the literal's value, honoring the unsigned bit pattern.
func (i *IntegerLiteralExpr) decimalString() string {
	if i.Wide != nil {
		return i.Wide.String()
	}
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
		return "0x" + strings.ToUpper(i.BigValue().Text(16))
	case IntegerBase8:
		return "0o" + i.BigValue().Text(8)
	case IntegerBase2:
		return "0b" + i.BigValue().Text(2)
	}
	return i.decimalString()
}

func (i *IntegerLiteralExpr) GetType() types.Type {
	// A **wide** literal stays *untyped* while both 128-bit types could hold it, so
	// context picks — `let a: i128 = …` and `let b: u128 = …` are both legal for a
	// magnitude either can represent, exactly as they are for a small one. It is
	// checkIntegerLiteralRange that refuses a narrower target, which is why that check
	// had to learn about big magnitudes at the same time.
	//
	// Above i128's positive range only u128 can hold it, so there it names a concrete
	// type — the rule the large-u64 case already follows, where u64 is the only answer.
	// The `<=` admits exactly 2^127 as untyped so `-170141183460469231731687303715884105728`,
	// i128's minimum, is writable; the same courtesy i64's minimum gets.
	if i.Wide != nil {
		if i.Wide.Cmp(int128Max) <= 0 {
			return types.PrimitiveType{Name: types.UntypedInt}
		}
		return types.PrimitiveType{Name: types.UInt128}
	}
	if i.Unsigned {
		return types.PrimitiveType{Name: types.UInt64}
	}
	return types.PrimitiveType{Name: types.UntypedInt}
}

// int128Max is 2^127, one past i128's true maximum: a *literal* is always
// non-negative (a leading `-` is a separate NegationExpr), so the magnitude that
// negates to i128's minimum has to be admitted here. checkIntegerLiteralRange draws
// the exact line, exactly as it does for i64.
var int128Max = new(big.Int).Lsh(big.NewInt(1), 127)

// Uint128Max is the largest magnitude any integer literal may have.
var Uint128Max = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 128), big.NewInt(1))

// Int64 answers the literal's value when it fits an int64, which every consumer that
// has not been taught about 128-bit literals depends on. **ok=false for a wide one**,
// which is what keeps such a consumer from silently reading 0.
func (i *IntegerLiteralExpr) Int64() (int64, bool) {
	if i.Wide != nil {
		return 0, false
	}
	return i.Value, true
}
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
