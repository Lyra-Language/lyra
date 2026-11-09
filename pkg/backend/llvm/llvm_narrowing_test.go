package llvm

import (
	"strings"
	"testing"
)

// `truncate`, `saturate` and `narrow`: what each makes of a value its target can and can't
// hold, at every kind of crossing — down, across a sign, up across a sign, 128 bits.

func TestExec_NarrowingMethods(t *testing.T) {
	cases := []struct{ name, setup, expr, want string }{
		{"truncate keeps the low bits", `let x: i64 = 300`, `x.truncate::<u8>()`, "44"},
		{"truncate of a negative is its bits", `let x: i32 = -300`, `x.truncate::<u8>()`, "212"},
		{"truncate into a sign", `let x: i32 = -300`, `x.truncate::<i8>()`, "-44"},
		{"truncate is what the conversion does", `let x: i64 = 70000`, `x.truncate::<u16>() == u16(x)`, "true"},
		{"a same-width sign change keeps the bits", `let x: u32 = 4000000000`, `x.truncate::<i32>()`, "-294967296"},
		{"up across a sign extends first", `let x: i8 = -1`, `x.truncate::<u16>()`, "65535"},

		{"saturate above the top", `let x: i64 = 300`, `x.saturate::<u8>()`, "255"},
		{"saturate below the bottom", `let x: i32 = -300`, `x.saturate::<u8>()`, "0"},
		{"saturate below a signed bottom", `let x: i64 = -300`, `x.saturate::<i8>()`, "-128"},
		{"saturate above a signed top", `let x: i64 = 300`, `x.saturate::<i8>()`, "127"},
		{"saturate at the top is the value", `let x: i64 = 255`, `x.saturate::<u8>()`, "255"},
		{"saturate at the bottom is the value", `let x: i64 = -128`, `x.saturate::<i8>()`, "-128"},
		{"saturate of what fits is the value", `let x: i64 = 7`, `x.saturate::<u8>()`, "7"},
		{"saturate up across a sign", `let x: i8 = -1`, `x.saturate::<u16>()`, "0"},
		{"saturate from unsigned to its signed self", `let x: u64 = 18446744073709551615`, `x.saturate::<i64>()`, "9223372036854775807"},
		{"saturate of 128 bits to 64", `let x: i128 = 170141183460469231731687303715884105727`, `x.saturate::<i64>()`, "9223372036854775807"},
		{"saturate of 128 bits to unsigned", `let x: i128 = -170141183460469231731687303715884105728`, `x.saturate::<u64>()`, "0"},

		{"narrow of what fits", `let x: i64 = 200`, `x.narrow::<u8>()`, "200"},
		{"narrow at the top", `let x: i64 = 255`, `x.narrow::<u8>()`, "255"},
		{"narrow at a signed bottom", `let x: i64 = -128`, `x.narrow::<i8>()`, "-128"},
		{"narrow across a sign", `let x: u32 = 7`, `x.narrow::<i32>()`, "7"},

		{"a literal receiver is an i64", ``, `300.truncate::<u8>()`, "44"},
		{"a newtype receiver reads its base", `type Cents = i64
let c = Cents(300)`, `c.saturate::<u8>()`, "255"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			src := tc.setup + "\nlet main = () -> void => println(" + tc.expr + ")\n"
			if strings.Contains(tc.setup, "let x") || strings.Contains(tc.setup, "let c") {
				src = "let main = () -> void => {\n" + indented(tc.setup) + "\n  println(" + tc.expr + ")\n}\n"
				if strings.HasPrefix(tc.setup, "type") {
					first, rest, _ := strings.Cut(tc.setup, "\n")
					src = first + "\nlet main = () -> void => {\n" + indented(rest) + "\n  println(" + tc.expr + ")\n}\n"
				}
			}
			got := strings.TrimSpace(buildAndRunWithPrelude(t, src, ""))
			if got != tc.want {
				t.Errorf("got %q, want %q\n%s", got, tc.want, src)
			}
		})
	}
}

func indented(s string) string {
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = "  " + lines[i]
	}
	return strings.Join(lines, "\n")
}

// The trap is the point of `narrow`, and a test of what it answers can't see one.
func TestExec_NarrowTraps(t *testing.T) {
	for name, src := range map[string]string{
		"above":           `let x: i64 = 256` + "\n" + `println(x.narrow::<u8>())`,
		"below":           `let x: i64 = -1` + "\n" + `println(x.narrow::<u8>())`,
		"signed above":    `let x: i64 = 128` + "\n" + `println(x.narrow::<i8>())`,
		"signed below":    `let x: i64 = -129` + "\n" + `println(x.narrow::<i8>())`,
		"unsigned top":    `let x: u64 = 18446744073709551615` + "\n" + `println(x.narrow::<i64>())`,
		"up across sign":  `let x: i8 = -1` + "\n" + `println(x.narrow::<u16>())`,
		"same width sign": `let x: u32 = 4000000000` + "\n" + `println(x.narrow::<i32>())`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			src := "let main = () -> void => {\n" + indented(src) + "\n}\n"
			out, code := buildAndRunPanicWithPrelude(t, src)
			if code != trapExitCode || !strings.Contains(out, narrowOverflowTrapMessage) {
				t.Errorf("exit %d, output %q, want a narrowing trap", code, out)
			}
		})
	}
}

// A context settles the target where a turbofish would: a binding, an argument, a return.
func TestExec_NarrowingTakesItsTargetFromContext(t *testing.T) {
	t.Parallel()
	got := buildAndRunWithPrelude(t, `
let pass = pure (b: u8) -> u8 => b
let returned = pure (x: i64) -> u8 => x.saturate()
let main = () -> void => {
  let x: i64 = 300
  let a: u8 = x.truncate()
  println(a)
  println(pass(x.saturate()))
  println(returned(x))
  var s: u8 = 0
  s = x.truncate()
  println(s)
}
`, "")
	if want := "44\n255\n255\n44\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// Range analysis drops the check where the receiver is proven to fit, and keeps it otherwise.
func TestEmit_NarrowDropsItsCheckWhereTheValueIsProven(t *testing.T) {
	t.Parallel()
	proven := emitWithPrelude(t, `
let main = () -> void => {
  let x: i64 = 200
  println(x.narrow::<u8>())
}
`)
	if strings.Contains(proven, "lyra_panic_narrow(") {
		t.Errorf("a proven narrow still calls the trap:\n%s", firstLinesContaining(proven, "lyra_panic_narrow"))
	}
	unproven := emitWithPrelude(t, `
let pick = (n: i64) -> u8 => n.narrow()
let main = () -> void => println(pick(200))
`)
	if !strings.Contains(unproven, "lyra_panic_narrow(") {
		t.Errorf("an unproven narrow has no trap")
	}
}

func firstLinesContaining(s, needle string) string {
	var out []string
	for line := range strings.SplitSeq(s, "\n") {
		if strings.Contains(line, needle) {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}
