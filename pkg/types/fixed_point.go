package types

import (
	"fmt"
	"math/big"
	"strconv"
	"strings"
)

// A fixed-point type is **a primitive whose name is its format**, `fixed<8,8>`: its value is
// a scaled signed integer, a scalar like any other, so everything that asks "is this a
// scalar", "does it own anything", "how large is it" or "are these the same type" is right
// with no case of its own. What differs is arithmetic, and that asks AsFixed.

// FixedFormat is what `fixed<I, F>` was written with: I integer bits, the sign among them,
// and F fractional bits.
type FixedFormat struct {
	Integer, Fraction int
}

const fixedPrefix = "fixed<"

// Fixed is the primitive type of a format.
func Fixed(format FixedFormat) PrimitiveType {
	return PrimitiveType{Name: PrimitiveTypeName(fmt.Sprintf("%s%d,%d>", fixedPrefix, format.Integer, format.Fraction))}
}

// AsFixed answers the format of a fixed-point type.
func AsFixed(t Type) (FixedFormat, bool) {
	p, ok := t.(PrimitiveType)
	if !ok {
		return FixedFormat{}, false
	}
	return FixedFormatOf(p.Name)
}

// FixedFormatOf answers the format a primitive's name spells.
func FixedFormatOf(name PrimitiveTypeName) (FixedFormat, bool) {
	s := string(name)
	if !strings.HasPrefix(s, fixedPrefix) || !strings.HasSuffix(s, ">") {
		return FixedFormat{}, false
	}
	integer, fraction, ok := strings.Cut(s[len(fixedPrefix):len(s)-1], ",")
	if !ok {
		return FixedFormat{}, false
	}
	i, err := strconv.Atoi(integer)
	if err != nil {
		return FixedFormat{}, false
	}
	f, err := strconv.Atoi(fraction)
	if err != nil {
		return FixedFormat{}, false
	}
	return FixedFormat{Integer: i, Fraction: f}, true
}

// Bits is the format's width: its storage integer's.
func (f FixedFormat) Bits() int { return f.Integer + f.Fraction }

// Valid reports whether the format can exist: a width Lyra has an integer for, with at least
// a sign bit above the point and at least one bit below it.
func (f FixedFormat) Valid() bool {
	if f.Integer < 1 || f.Fraction < 1 {
		return false
	}
	switch f.Bits() {
	case 8, 16, 32, 64:
		return true
	}
	return false
}

// Storage is the signed integer holding the format's bits, what `floor` answers and
// `to_bits` reads out.
func (f FixedFormat) Storage() PrimitiveTypeName {
	switch f.Bits() {
	case 8:
		return Int8
	case 16:
		return Int16
	case 32:
		return Int32
	}
	return Int64
}

// RawRange is the inclusive range of the format's stored integer.
func (f FixedFormat) RawRange() (lo, hi *big.Int) {
	lo, hi, _ = bitRange(uint(f.Bits()), true)
	return lo, hi
}

// HoldsEvery reports whether every value of other is exactly a value of f.
func (f FixedFormat) HoldsEvery(other FixedFormat) bool {
	return f.Integer >= other.Integer && f.Fraction >= other.Fraction
}

// LiteralRaw answers the stored integer closest to a literal's value, ties upward, and
// whether the format can hold it. A float literal is exactly its float64, so past 52
// fractional bits the low ones are that float64's rather than the decimal's.
func (f FixedFormat) LiteralRaw(value *big.Rat) (*big.Int, bool) {
	scaled := new(big.Rat).Mul(value, new(big.Rat).SetInt(new(big.Int).Lsh(big.NewInt(1), uint(f.Fraction))))
	scaled.Add(scaled, big.NewRat(1, 2))
	raw := new(big.Int).Div(scaled.Num(), scaled.Denom()) // Euclidean: a floor, as Denom > 0
	lo, hi := f.RawRange()
	return raw, raw.Cmp(lo) >= 0 && raw.Cmp(hi) <= 0
}
