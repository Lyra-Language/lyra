package types

import "fmt"

// FixedPointType represents a fixed-point numeric type with a given number of
// integer bits and fractional bits, e.g. fixed<16,16>.
type FixedPointType struct {
	IntegerBits    int
	FractionalBits int
}

func (FixedPointType) typeNode() {}

func (f FixedPointType) GetName() string {
	return fmt.Sprintf("fixed<%d,%d>", f.IntegerBits, f.FractionalBits)
}

func (f FixedPointType) String() string {
	return f.GetName()
}
