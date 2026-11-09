package types

import "math/big"

// IntegerRange answers the inclusive range of a concrete integer type, at any width, 128 bits
// included. It answers false for anything else, an untyped literal among them: a literal has
// no range until a context has settled its width.
func IntegerRange(name PrimitiveTypeName) (lo, hi *big.Int, ok bool) {
	bits, signed, ok := integerShape(name)
	if !ok {
		return nil, nil, false
	}
	return bitRange(bits, signed)
}

func integerShape(name PrimitiveTypeName) (bits uint, signed bool, ok bool) {
	switch name {
	case Int8:
		return 8, true, true
	case Int16:
		return 16, true, true
	case Int32:
		return 32, true, true
	case Int64:
		return 64, true, true
	case Int128:
		return 128, true, true
	case UInt8:
		return 8, false, true
	case UInt16:
		return 16, false, true
	case UInt32:
		return 32, false, true
	case UInt64:
		return 64, false, true
	case UInt128:
		return 128, false, true
	}
	return 0, false, false
}

func bitRange(bits uint, signed bool) (lo, hi *big.Int, ok bool) {
	one := big.NewInt(1)
	if signed {
		half := new(big.Int).Lsh(one, bits-1)
		return new(big.Int).Neg(half), new(big.Int).Sub(half, one), true
	}
	return big.NewInt(0), new(big.Int).Sub(new(big.Int).Lsh(one, bits), one), true
}
