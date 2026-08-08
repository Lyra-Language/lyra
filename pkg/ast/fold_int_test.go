package ast

import (
	"math"
	"math/big"
	"testing"
)

func lit(v int64) *IntegerLiteralExpr { return &IntegerLiteralExpr{Value: v, Base: IntegerBase10} }

func mul(a, b Expression) Expression {
	return &MathBinaryOpExpr{Left: a, Operator: MathBinaryOpMul, Right: b}
}

// Folding is **arbitrary precision**, and `FoldIntExpr` narrows at the end rather than
// folding through an int64.
//
// That ordering is the contract, and it is what this pins: a product that does not fit
// an int64 must make FoldIntExpr answer ok=false — never a wrapped value — while
// FoldBigExpr answers with the true magnitude, which is what the range check reports.
//
// It replaces a test on the old `checkedMulInt64` helper, whose whole subject was
// detecting int64 overflow during the walk. Arbitrary precision removes that failure
// mode rather than fixing it, so the test moved to the property that survives.
func TestFold_NarrowsOnlyWhenItFits(t *testing.T) {
	tests := []struct {
		name    string
		expr    Expression
		want    string // the true value, big.Int text
		fitsI64 bool
	}{
		{"simple", mul(lit(6), lit(7)), "42", true},
		{"negative", mul(lit(-6), lit(7)), "-42", true},
		{"max times one", mul(lit(math.MaxInt64), lit(1)), "9223372036854775807", true},
		{"max times two", mul(lit(math.MaxInt64), lit(2)), "18446744073709551614", false},
		{"min times two", mul(lit(math.MinInt64), lit(2)), "-18446744073709551616", false},
		// The edge the old helper existed for: the wrapped product of MinInt64 * -1 is
		// MinInt64 again, and MinInt64 / -1 == MinInt64 in two's complement, so a
		// division-based overflow check reported *no* overflow. At arbitrary precision
		// the true value is 2^63, which simply does not fit — no special case needed.
		{"min times neg one", mul(lit(math.MinInt64), lit(-1)), "9223372036854775808", false},
		{"max times neg one", mul(lit(math.MaxInt64), lit(-1)), "-9223372036854775807", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotBig, ok := FoldBigExpr(tt.expr, nil)
			if !ok {
				t.Fatalf("FoldBigExpr declined; want %s", tt.want)
			}
			want, _ := new(big.Int).SetString(tt.want, 10)
			if gotBig.Cmp(want) != 0 {
				t.Errorf("FoldBigExpr = %s, want %s", gotBig, tt.want)
			}
			got64, ok64 := FoldIntExpr(tt.expr)
			if ok64 != tt.fitsI64 {
				t.Fatalf("FoldIntExpr ok = %v, want %v", ok64, tt.fitsI64)
			}
			if ok64 && big.NewInt(got64).Cmp(want) != 0 {
				t.Errorf("FoldIntExpr = %d, want %s", got64, tt.want)
			}
		})
	}
}

// The guard against a pathological chain: nothing a program can write approaches 2^512,
// so exceeding it means refusing to fold rather than doing arbitrary-precision work in
// the middle of a compile. Refusing loses a diagnostic, never correctness.
func TestFold_RefusesAnAbsurdMagnitude(t *testing.T) {
	e := Expression(lit(math.MaxInt64))
	for i := 0; i < 10; i++ {
		e = mul(e, lit(math.MaxInt64))
	}
	if _, ok := FoldBigExpr(e, nil); ok {
		t.Errorf("expected a magnitude past the fold limit to be refused")
	}
}
