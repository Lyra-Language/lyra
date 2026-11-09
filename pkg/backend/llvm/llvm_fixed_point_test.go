package llvm

import (
	"strings"
	"testing"
)

// fixed<I, F>: every width, both signs, and what happens at the edges of the format.

func runFixed(t *testing.T, body string) string {
	t.Helper()
	return strings.TrimSpace(buildAndRunWithPrelude(t, "let main = () -> void => {\n"+body+"\n}\n", ""))
}

func TestExec_FixedPointArithmetic(t *testing.T) {
	cases := []struct{ name, body, want string }{
		// Each width, with a value that needs both halves of the format.
		{"8 bits", `let a: fixed<4, 4> = 1.5
  println(a + 0.25)
  println(a * 2)
  println(a / 2)`, "1.75\n3.0\n0.75"},
		{"16 bits", `let a: fixed<8, 8> = 1.5
  println(a + 0.25)
  println(a * 2)
  println(a / 2)`, "1.75\n3.0\n0.75"},
		{"32 bits", `let a: fixed<16, 16> = 1.5
  println(a + 0.25)
  println(a * 2)
  println(a / 2)`, "1.75\n3.0\n0.75"},
		{"64 bits", `let a: fixed<32, 32> = 1.5
  println(a + 0.25)
  println(a * 2)
  println(a / 2)`, "1.75\n3.0\n0.75"},
		{"almost all fraction", `let a: fixed<1, 7> = 0.5
  println(a * 0.5)
  println(a.to_bits())`, "0.25\n64"},
		{"almost all whole", `let a: fixed<7, 1> = 3.5
  println(a * 2)`, "7.0"},
		{"64 bits almost all fraction", `let a: fixed<2, 62> = 1.5
  println(a * 0.5)
  println(a.to_bits() == 6917529027641081856)`, "0.75\ntrue"},

		// Negatives, in every operation that could get one wrong.
		{"negative multiply", `let a: fixed<8, 8> = -1.5
  println(a * 2)
  println(a * -2)`, "-3.0\n3.0"},
		{"negative divide truncates toward zero", `let a: fixed<8, 8> = -1.0
  let b: fixed<8, 8> = 3.0
  println(a / b)
  println((-a) / b)`, "-0.33203125\n0.33203125"},
		{"negate", `let a: fixed<8, 8> = 1.5
  println(-a)
  println(-(-a))`, "-1.5\n1.5"},
		{"subtract across zero", `let a: fixed<8, 8> = 0.25
  println(a - 0.5)`, "-0.25"},
		{"remainder", `let a: fixed<8, 8> = 5.5
  println(a % 2)
  println((-a) % 2)
  println((-a) %% 2)`, "1.5\n-1.5\n0.5"},

		// A product rounds to the nearest stored value, and a half goes up.
		{"a product rounds down", `let a: fixed<8, 8> = 0.00390625
  println(a * 0.25)`, "0.0"},
		{"a product rounds up", `let a: fixed<8, 8> = 0.00390625
  println(a * 0.75)`, "0.00390625"},
		{"a half rounds up", `let a: fixed<8, 8> = 0.00390625
  println(a * 0.5)`, "0.00390625"},
		{"a negative half rounds up", `let a: fixed<8, 8> = -0.00390625
  println(a * 0.5)`, "0.0"},

		// The edges of the format, reached and not passed.
		{"the top", `let a: fixed<8, 8> = 127.99609375
  println(a)
  println(a.to_bits())`, "127.99609375\n32767"},
		{"the bottom", `let a: fixed<8, 8> = -128
  println(a)
  println(a.to_bits())`, "-128.0\n-32768"},
		{"the bottom as a float", `let a: fixed<8, 8> = -128.0
  println(a)`, "-128.0"},
		{"the 64-bit bottom", `let a: fixed<32, 32> = -2147483648
  println(a.to_bits() == -9223372036854775808)`, "true"},

		// Comparison, and the prelude's trait impls reaching a format.
		{"comparison", `let a: fixed<8, 8> = 1.5
  println(a < 2)
  println(a > 2)
  println(a == 1.5)
  println(a != 1.5)
  println((a <=> 2.0) == Less)`, "true\nfalse\ntrue\nfalse\ntrue"},
		{"min, max and clamp", `let a: fixed<8, 8> = -1.5
  let b: fixed<8, 8> = 2.0
  println(a.min(b))
  println(a.max(b))
  println(a.clamp(0.0, 1.0))`, "-1.5\n2.0\n0.0"},
		{"abs and is_negative", `let a: fixed<8, 8> = -1.5
  println(a.abs())
  println(a.is_negative())`, "1.5\ntrue"},
		{"show", `let a: fixed<8, 8> = 1.5
  println(a.show())`, "1.5"},
		{"compound assignment", `var a: fixed<16, 16> = 1.25
  a *= 2
  a += 0.75
  a /= 2
  a -= 0.25
  println(a)`, "1.375"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := runFixed(t, tc.body); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestExec_FixedPointLiteralsAndPrinting(t *testing.T) {
	cases := []struct{ name, body, want string }{
		{"a literal with no exact value is the nearest", `let a: fixed<8, 8> = 0.1
  println(a.to_bits())
  println(a)`, "26\n0.1015625"},
		{"a tie goes up", `let a: fixed<8, 8> = 0.001953125
  println(a.to_bits())`, "1"},
		{"a negative tie goes up", `let a: fixed<8, 8> = -0.001953125
  println(a.to_bits())`, "0"},
		{"an integer literal is a whole value", `let a: fixed<8, 8> = 3
  println(a.to_bits())`, "768"},
		{"a whole value prints a point", `let a: fixed<8, 8> = 2
  println(a)`, "2.0"},
		{"the smallest step prints in full", `let a: fixed<8, 8> = 0.00390625
  println(a)`, "0.00390625"},
		{"negative under one", `let a: fixed<8, 8> = -0.5
  println(a)`, "-0.5"},
		{"64 bits' smallest step", `let a = fixed<32, 32>.from_bits(1)
  println(a)`, "0.00000000023283064365386962890625"},
		{"64 bits' bottom", `let a = fixed<32, 32>.from_bits(-9223372036854775808)
  println(a)`, "-2147483648.0"},
		{"a fraction of 63 bits", `let a = fixed<1, 63>.from_bits(4611686018427387904)
  println(a)`, "0.5"},
		{"interpolation", `let a: fixed<8, 8> = 1.5
  println("x = ${a}")`, "x = 1.5"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := runFixed(t, tc.body); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestExec_FixedPointConversions(t *testing.T) {
	cases := []struct{ name, body, want string }{
		{"to a wider format", `let a: fixed<8, 8> = -1.5
  println(fixed<24, 8>(a))`, "-1.5"},
		{"to more fraction", `let a: fixed<8, 8> = -1.5
  println(fixed<16, 16>(a))`, "-1.5"},
		{"to less fraction rounds to nearest", `let a: fixed<16, 16> = 1.00390625
  println(fixed<24, 8>(a))
  let b: fixed<16, 16> = 1.001953125
  println(fixed<24, 8>(b))`, "1.00390625\n1.00390625"},
		{"from an integer", `let n: i32 = -7
  println(fixed<16, 16>(n))
  println(fixed<8, 8>(3))`, "-7.0\n3.0"},
		{"from an unsigned integer", `let n: u8 = 200
  println(fixed<16, 16>(n))`, "200.0"},
		{"from a float, to the nearest", `let f: f64 = 2.25
  println(fixed<8, 8>(f))
  let g: f64 = 0.1
  println(fixed<8, 8>(g).to_bits())
  let h: f64 = -0.001953125
  println(fixed<8, 8>(h).to_bits())`, "2.25\n26\n0"},
		{"from an f32", `let f: f32 = 1.5
  println(fixed<8, 8>(f))`, "1.5"},
		{"to a float", `let a: fixed<8, 8> = -1.5
  println(f64(a))
  println(f32(a))`, "-1.5\n-1.5"},
		{"to a float keeps 64 bits' top end", `let a: fixed<32, 32> = 3.5
  println(f64(a))`, "3.5"},

		{"floor, ceil and round", `let a: fixed<8, 8> = 1.5
  println(a.floor())
  println(a.ceil())
  println(a.round())`, "1\n2\n2"},
		{"floor, ceil and round of a negative", `let a: fixed<8, 8> = -1.5
  println(a.floor())
  println(a.ceil())
  println(a.round())`, "-2\n-1\n-1"},
		{"a whole value is its own floor and ceil", `let a: fixed<8, 8> = -2
  println(a.floor())
  println(a.ceil())
  println(a.round())`, "-2\n-2\n-2"},
		{"round down below a half", `let a: fixed<8, 8> = 1.25
  println(a.round())`, "1"},
		{"ceil can't overflow near the top", `let a: fixed<8, 8> = 127.5
  println(a.ceil())
  let b: fixed<1, 7> = 0.5
  println(b.ceil())`, "128\n1"},
		{"floor is the storage integer", `let a: fixed<8, 8> = 1.5
  let b: i16 = a.floor()
  println(b)
  let c: fixed<24, 8> = 1.5
  let d: i32 = c.floor()
  println(d)`, "1\n1"},
		{"bits, there and back", `let a = fixed<8, 8>.from_bits(384)
  println(a)
  println(a.to_bits())`, "1.5\n384"},

		{"narrow of what fits", `let a: fixed<24, 8> = 1.5
  println(a.narrow::<fixed<8, 8>>())`, "1.5"},
		{"saturate above the top", `let a: fixed<24, 8> = 1000.25
  println(a.saturate::<fixed<8, 8>>())`, "127.99609375"},
		{"saturate below the bottom", `let a: fixed<24, 8> = -1000.25
  println(a.saturate::<fixed<8, 8>>())`, "-128.0"},
		{"truncate keeps the low bits", `let a: fixed<24, 8> = 1000.25
  println(a.truncate::<fixed<8, 8>>())`, "-23.75"},
		{"narrowing rescales", `let a: fixed<16, 16> = 1.00390625
  println(a.saturate::<fixed<8, 8>>())
  let b: fixed<16, 16> = 1.00390625
  println(b.narrow::<fixed<12, 4>>())`, "1.00390625\n1.0"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := runFixed(t, tc.body); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestExec_FixedPointOverflowFamilies(t *testing.T) {
	cases := []struct{ name, body, want string }{
		{"saturating add", `let a: fixed<8, 8> = 127.5
  println(a.saturating_add(1.0))`, "127.99609375"},
		{"saturating sub", `let a: fixed<8, 8> = -127.5
  println(a.saturating_sub(1.0))`, "-128.0"},
		{"saturating mul", `let a: fixed<8, 8> = 100
  println(a.saturating_mul(2.0))
  println(a.saturating_mul(-2.0))`, "127.99609375\n-128.0"},
		{"wrapping add", `let a: fixed<8, 8> = 127.5
  println(a.wrapping_add(1.0))`, "-127.5"},
		{"wrapping mul", `let a: fixed<8, 8> = 1.5
  println(a.wrapping_mul(127.0))`, "-65.5"},
		{"checked add", `let a: fixed<8, 8> = 127.5
  println(match a.checked_add(1.0) { Some(v) => "some ${v}", None => "none" })
  println(match a.checked_add(0.25) { Some(v) => "some ${v}", None => "none" })`, "none\nsome 127.75"},
		{"checked mul", `let a: fixed<8, 8> = 100
  println(match a.checked_mul(2.0) { Some(v) => "some ${v}", None => "none" })
  println(match a.checked_mul(0.5) { Some(v) => "some ${v}", None => "none" })`, "none\nsome 50.0"},
		{"checked div", `let a: fixed<8, 8> = 100
  println(match a.checked_div(0.0) { Some(v) => "some ${v}", None => "none" })
  println(match a.checked_div(0.5) { Some(v) => "some ${v}", None => "none" })
  println(match a.checked_div(2.0) { Some(v) => "some ${v}", None => "none" })`, "none\nnone\nsome 50.0"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := runFixed(t, tc.body); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestExec_FixedPointTraps(t *testing.T) {
	for name, tc := range map[string]struct{ body, want string }{
		"add past the top":       {`let a: fixed<8, 8> = 127.5` + "\n" + `println(a + 1)`, overflowTrapMessage},
		"subtract past the end":  {`let a: fixed<8, 8> = -127.5` + "\n" + `println(a - 1)`, overflowTrapMessage},
		"multiply past the top":  {`let a: fixed<8, 8> = 100` + "\n" + `println(a * 2)`, overflowTrapMessage},
		"multiply at 8 bits":     {`let a: fixed<4, 4> = 7` + "\n" + `println(a * 2)`, overflowTrapMessage},
		"multiply at 32 bits":    {`let a: fixed<16, 16> = 30000` + "\n" + `println(a * 2)`, overflowTrapMessage},
		"multiply at 64 bits":    {`let a: fixed<32, 32> = 2000000000` + "\n" + `println(a * 2)`, overflowTrapMessage},
		"rounding up past top":   {`let a: fixed<8, 8> = 127.99609375` + "\n" + `println(a * 1.00390625)`, overflowTrapMessage},
		"divide by zero":         {`let a: fixed<8, 8> = 1.5` + "\n" + `let z = fixed<8, 8>(zero())` + "\n" + `println(a / z)`, divideByZeroTrapMessage},
		"divide past the top":    {`let a: fixed<8, 8> = 100` + "\n" + `println(a / 0.5)`, overflowTrapMessage},
		"negate the bottom":      {`let a: fixed<8, 8> = -128` + "\n" + `println(-a)`, overflowTrapMessage},
		"integer past the top":   {`let n: i32 = 128` + "\n" + `println(fixed<8, 8>(n))`, overflowTrapMessage},
		"integer past the end":   {`let n: i32 = -129` + "\n" + `println(fixed<8, 8>(n))`, overflowTrapMessage},
		"float past the top":     {`let f: f64 = 128` + "\n" + `println(fixed<8, 8>(f))`, overflowTrapMessage},
		"float rounding past":    {`let f: f64 = 127.999` + "\n" + `println(fixed<8, 8>(f))`, overflowTrapMessage},
		"NaN":                    {`let f: f64 = 0.0` + "\n" + `println(fixed<8, 8>(f / f))`, overflowTrapMessage},
		"rescale rounding past":  {`let a: fixed<8, 8> = 127.99609375` + "\n" + `println(fixed<8, 8>(a).narrow::<fixed<12, 4>>())`, ""},
		"narrow past the top":    {`let a: fixed<24, 8> = 1000` + "\n" + `println(a.narrow::<fixed<8, 8>>())`, narrowOverflowTrapMessage},
		"narrow past the bottom": {`let a: fixed<24, 8> = -1000` + "\n" + `println(a.narrow::<fixed<8, 8>>())`, narrowOverflowTrapMessage},
	} {
		if tc.want == "" {
			continue // the rescale above holds its value: covered by the conversions
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			src := "let zero = () -> i64 => 0\nlet main = () -> void => {\n" + indented(tc.body) + "\n}\n"
			out, code := buildAndRunPanicWithPrelude(t, src)
			if code != trapExitCode || !strings.Contains(out, tc.want) {
				t.Errorf("exit %d, output %q, want %q", code, out, tc.want)
			}
		})
	}
}

// A fixed value in every position a value goes: as a type's payload and through a generic,
// closure and array, with ASan on the managed ones.
func TestExec_FixedPointInEveryPosition(t *testing.T) {
	t.Parallel()
	const src = `
struct Body { x: fixed<24, 8>, v: fixed<8, 8> }
struct Box<t> { v: t }
data Kind = Slow(fixed<8, 8>) | Still
const SPEED: fixed<8, 8> = 2.5
const TABLE: [3]fixed<8, 8> = #[1.5, 2.0, 0.25]
var total: fixed<16, 16> = 1.5
let same<t> = pure (x: t) -> t => x
let biggest<t> where t: Ord = pure (a: t, b: t) -> t => a.max(b)
let twice = pure (y: fixed<8, 8>) -> fixed<8, 8> => y * 2
let step = (b: Body) -> Body => Body { b | x: b.x + fixed<24, 8>(b.v) }
let main = () -> u8 => {
  let a: fixed<8, 8> = 1.5
  let xs = [a, a + 1, twice(a)]
  println(xs[2])
  let fixed_array = #[a; 2]
  println(fixed_array[1])
  println(same(a))
  println(biggest(a, 3.5))
  println(Box { v: a }.v)
  println(match Slow(1.25) { Slow(v) => v, Still => 0.0 })
  println(SPEED)
  println(TABLE[2])
  total += 1
  println(total)
  let scaled = (y: fixed<8, 8>) -> fixed<8, 8> => y * a
  println(scaled(2.0))
  println(step(Body { x: 100.5, v: 1.25 }).x)
  println(match Some(a) { Some(v) => v, None => 0.0 })
  let chosen: fixed<8, 8> = if a > 1 { 0.5 } else { -0.5 }
  println(chosen)
  println(xs.map((v) => v * 2)[0])
  0
}
`
	want := "3.0\n1.5\n1.5\n3.5\n1.5\n1.25\n2.5\n0.25\n2.5\n3.0\n101.75\n1.5\n0.5\n3.0\n"
	if got := buildAndRunWithPrelude(t, src, ""); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if code := buildAndRunASanWithPrelude(t, src); code != 0 {
		t.Errorf("under ASan: exit %d", code)
	}
}
