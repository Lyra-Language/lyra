package llvm

import (
	"fmt"
	"math"
	"math/big"
	"math/rand"
	"strconv"
	"strings"
	"testing"
)

// parse_f64 and parse_f32 against Go's strconv.ParseFloat, bit for bit: hand-picked edges
// (signed zero, the largest and least floats, overflow, underflow, the syntax refused), then
// seeded random cases — decimals across the whole exponent range, every float's shortest and
// 17-digit forms, and the exact halfway points between neighbouring floats, which only a
// correctly rounded parser gets right (ties to even), alone and nudged up by a digit past the
// 800 the parser keeps. Each line prints its f64's bits and its f32's, or None.
func TestExec_ParseFloat(t *testing.T) {
	t.Parallel()
	const src = `
module main
let main = () -> void => {
  var done = false
  while !done {
    match read_line() {
      None => { done = true },
      Some(line) => {
        let a = match line.parse_f64() { Some(f) => "${f.to_bits()}", None => "None" }
        let b = match line.parse_f32() { Some(f) => "${f.to_bits()}", None => "None" }
        println("${a} ${b}")
      },
    }
  }
}
`
	// What Go accepts and Lyra refuses on purpose: infinities, NaN, hexadecimal floats and
	// underscores. Everything else is Go's answer.
	refused := []string{"inf", "-Inf", "Infinity", "nan", "NaN", "0x10", "0x1p-2", "1_000"}
	edges := []string{
		"0", "-0", "+0", "0.0", "-0.0", "00", "0e0", "0e999999999999", "-0e-5",
		"1", "-1", "+1", "1.", ".5", "-.5", "1.5", "37.5", "0.1", "0.3", "1e3", "1E3", "1e+3",
		"1e-3", "123456789", "1234567890123456789", "12345678901234567890123456789",
		"9007199254740992", "9007199254740993", "9007199254740995", // 2^53 and its ties
		"4503599627370496.5", "4503599627370497.5", // ties to even at 2^52
		"1e15", "1e16", "1e22", "1e23", "1e-22", "1e-23", "123e37", "1e308", "1e309",
		"1.7976931348623157e308", "1.7976931348623158e308", "1.7976931348623159e308",
		"179769313486231580793728971405301e276", // halfway to overflow: rounds to infinity
		"2.2250738585072014e-308", "2.2250738585072011e-308", "2.225073858507201136e-308",
		"4.9406564584124654e-324", "5e-324", "2.4703282292062328e-324", "2.4703282292062327e-324",
		"3e-324", "1e-400", "1e-99999999",
		"3.4028234663852886e38", "3.4028235677973366e38", "3.4028236e38", "1e39", "-1e39",
		"1.17549435e-38", "1.4e-45", "7e-46", "7.1e-46", "1e-46",
		"0.000000000000000000000000000000000000000000001",
		"100000000000000016777215", "100000000000000016777216", // f32 ties
		"", "-", "+", ".", "-.", "e5", ".e5", "1e", "1e+", "1e-", "1.2.3", "1..2", "--1", "+-1",
		" 1", "1 ", "1e5x", "x1", "１", "1,5",
	}
	rng := rand.New(rand.NewSource(20261022))
	var generated []string
	digits := func(n int) string {
		var b strings.Builder
		for range n {
			b.WriteByte(byte('0' + rng.Intn(10)))
		}
		return b.String()
	}
	for range 600 {
		// A decimal of up to 25 digits on each side of the point, at any exponent.
		var b strings.Builder
		if rng.Intn(3) == 0 {
			b.WriteString("-")
		}
		whole, frac := digits(rng.Intn(26)), ""
		if rng.Intn(2) == 0 || whole == "" {
			frac = digits(1 + rng.Intn(25))
		}
		b.WriteString(whole)
		if frac != "" {
			b.WriteString("." + frac)
		}
		if rng.Intn(4) != 0 {
			fmt.Fprintf(&b, "e%d", rng.Intn(800)-400)
		}
		generated = append(generated, b.String())
	}
	for range 400 {
		// A float's shortest form and a 17-digit one, normal or subnormal.
		x := math.Float64frombits(rng.Uint64()&^(0x7FF<<52) | uint64(rng.Intn(0x7FF))<<52)
		generated = append(generated, strconv.FormatFloat(x, 'g', -1, 64), strconv.FormatFloat(x, 'e', 16, 64))
		y := math.Float32frombits(rng.Uint32()&^(0xFF<<23) | uint32(rng.Intn(0xFF))<<23)
		generated = append(generated, strconv.FormatFloat(float64(y), 'g', -1, 32))
	}
	for range 200 {
		// Exactly halfway between two neighbouring floats, then a hair above.
		x := math.Abs(math.Float64frombits(rng.Uint64()&^(0x7FF<<52) | uint64(rng.Intn(0x7FE))<<52))
		generated = append(generated, halfway(x, math.Nextafter(x, math.Inf(1)))...)
		y := float64(math.Abs(float64(math.Float32frombits(rng.Uint32()&^(0xFF<<23) | uint32(rng.Intn(0xFE))<<23))))
		generated = append(generated, halfway(y, float64(math.Nextafter32(float32(y), float32(math.Inf(1)))))...)
	}

	var in, want strings.Builder
	var lines []string
	expect := func(s string, bits int) string {
		v, err := strconv.ParseFloat(s, bits)
		if err != nil {
			return "None"
		}
		if bits == 32 {
			return strconv.FormatUint(uint64(math.Float32bits(float32(v))), 10)
		}
		return strconv.FormatUint(math.Float64bits(v), 10)
	}
	for _, s := range refused {
		lines = append(lines, s)
		want.WriteString("None None\n")
	}
	for _, s := range append(edges, generated...) {
		lines = append(lines, s)
		fmt.Fprintf(&want, "%s %s\n", expect(s, 64), expect(s, 32))
	}
	for _, s := range lines {
		in.WriteString(s + "\n")
	}
	got := buildAndRunWithPrelude(t, src, in.String())
	gotLines := strings.Split(got, "\n")
	wantLines := strings.Split(want.String(), "\n")
	if len(gotLines) != len(wantLines) {
		t.Fatalf("%d lines out for %d in:\n%s", len(gotLines)-1, len(wantLines)-1, got)
	}
	misses := 0
	for i := range lines {
		if gotLines[i] != wantLines[i] {
			misses++
			if misses <= 10 {
				t.Errorf("%q: got %s, want %s (f64 bits, f32 bits)", lines[i], gotLines[i], wantLines[i])
			}
		}
	}
	if misses > 10 {
		t.Errorf("…and %d more of %d", misses-10, len(lines))
	}
}

// The decimal exactly halfway between two neighbouring floats a and b, and the same with a 1
// after its last digit — past 800 significant digits for most, so only a parser that
// remembers digits it dropped rounds it up.
func halfway(a, b float64) []string {
	mid := new(big.Float).SetPrec(2200).SetFloat64(a)
	mid.Add(mid, new(big.Float).SetPrec(2200).SetFloat64(b))
	mid.Quo(mid, big.NewFloat(2).SetPrec(2200))
	exact := mid.Text('e', 1100)
	mantissa, exponent, _ := strings.Cut(exact, "e")
	mantissa = strings.TrimRight(mantissa, "0")
	return []string{mantissa + "e" + exponent, mantissa + "1e" + exponent}
}
