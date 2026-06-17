package typechecker

import (
	"math"
	"testing"
)

// TestCheckedMulInt64 covers the int64 overflow detection directly, including
// the MinInt64 * -1 edge case that the product/b division check alone misses.
func TestCheckedMulInt64(t *testing.T) {
	tests := []struct {
		name   string
		a, b   int64
		want   int64
		wantOK bool
	}{
		{"zero left", 0, 5, 0, true},
		{"zero right", 5, 0, 0, true},
		{"simple", 6, 7, 42, true},
		{"negative", -6, 7, -42, true},
		{"max no overflow", math.MaxInt64, 1, math.MaxInt64, true},
		{"overflow positive", math.MaxInt64, 2, 0, false},
		{"overflow negative", math.MinInt64, 2, 0, false},
		{"min times neg one", math.MinInt64, -1, 0, false},
		{"neg one times min", -1, math.MinInt64, 0, false},
		{"max times neg one ok", math.MaxInt64, -1, -math.MaxInt64, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := checkedMulInt64(tt.a, tt.b)
			if ok != tt.wantOK {
				t.Fatalf("checkedMulInt64(%d, %d) ok = %v, want %v", tt.a, tt.b, ok, tt.wantOK)
			}
			if ok && got != tt.want {
				t.Errorf("checkedMulInt64(%d, %d) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
		})
	}
}
