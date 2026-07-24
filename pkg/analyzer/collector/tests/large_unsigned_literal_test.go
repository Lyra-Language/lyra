package collector_test

import (
	"testing"

	"github.com/Lyra-Language/lyra/pkg/ast"
)

// A literal in (int64max, u64max] is collected as an Unsigned IntegerLiteralExpr
// carrying the value's true magnitude, in every integer base — the collector's
// int64-overflow fallback parses as unsigned base-agnostically (decimal, hex,
// octal, binary). Value holds the int64 bit pattern (reads negative);
// UnsignedValue recovers the magnitude.
func TestCollect_LargeUnsignedLiteral_AllBases(t *testing.T) {
	const u64max = uint64(18446744073709551615) // == 0xFFFFFFFFFFFFFFFF

	cases := []struct {
		name string
		src  string
		want uint64
	}{
		{"decimal", "let x = 18446744073709551615", u64max},
		{"hex", "let x = 0xFFFFFFFFFFFFFFFF", u64max},
		{"octal", "let x = 0o1777777777777777777777", u64max},
		{"binary", "let x = 0b1111111111111111111111111111111111111111111111111111111111111111", u64max},
		{"hex mid-range", "let x = 0xDEADBEEFCAFEBABE", 0xDEADBEEFCAFEBABE},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			program, _, _, errs := parseAndCollect(t, c.src)
			if len(errs) > 0 {
				t.Fatalf("unexpected collector errors: %v", errs)
			}
			decl, ok := program.Statements[0].(*ast.VarDeclStmt)
			if !ok {
				t.Fatalf("expected a VarDeclStmt, got %T", program.Statements[0])
			}
			lit, ok := decl.Value.(*ast.IntegerLiteralExpr)
			if !ok {
				t.Fatalf("expected an IntegerLiteralExpr, got %T", decl.Value)
			}
			if !lit.Unsigned {
				t.Errorf("expected Unsigned=true for a value above int64 max")
			}
			if lit.UnsignedValue() != c.want {
				t.Errorf("UnsignedValue() = %d; want %d", lit.UnsignedValue(), c.want)
			}
		})
	}
}

// A value that fits int64 is an ordinary (non-Unsigned) literal, in any base.
func TestCollect_InRangeLiteral_NotUnsigned(t *testing.T) {
	for _, src := range []string{"let x = 255", "let x = 0xFF", "let x = 0o377", "let x = 0b11111111"} {
		program, _, _, errs := parseAndCollect(t, src)
		if len(errs) > 0 {
			t.Fatalf("%s: unexpected errors: %v", src, errs)
		}
		lit := program.Statements[0].(*ast.VarDeclStmt).Value.(*ast.IntegerLiteralExpr)
		if lit.Unsigned {
			t.Errorf("%s: expected Unsigned=false for an in-range value", src)
		}
		if lit.Value != 255 {
			t.Errorf("%s: Value = %d; want 255", src, lit.Value)
		}
	}
}
