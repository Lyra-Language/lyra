package llvm

import (
	"strings"
	"testing"
)

// `contains`, `any` and `all` on `[]t` (10/19, asked for by Vega's `project.is_taken`): by
// `==`, so structurally on a struct, and beside `string.contains` without a clash — the two
// are a receiver overload.
func TestExec_ArrayContainsAnyAll(t *testing.T) {
	t.Parallel()
	const src = `
module main
struct Pt { x: i64, y: i64 }
let main = () -> void => {
  let xs = [3, -1, 4]
  let pts = [Pt { x: 1, y: 2 }]
  let none: []i64 = []
  print("${xs.contains(4)} ${xs.contains(5)} ${["a", "b"].contains("b")} ${pts.contains(Pt { x: 1, y: 2 })} ")
  print("${xs.any((n) => n < 0)} ${xs.all((n) => n < 10)} ${xs.all((n) => n > 0)} ")
  print("${none.any((n) => n == n)} ${none.all((n) => n != n)} ${"hello".contains("ell")}")
}
`
	want := "true false true true true true false false true true"
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != want {
		t.Errorf("got %q; want %q", got, want)
	}
}

// `index_of` on `[]t` (10/23, asked for by ui's menus, which find an opener in the chain of
// open ones): the first place equal by `==`, structurally on a struct, `None` where there is
// none — an empty array too.
func TestExec_ArrayIndexOf(t *testing.T) {
	t.Parallel()
	const src = `
module main
struct Pt { x: i64, y: i64 }
let main = () -> void => {
  let xs = [3, -1, 4, -1]
  let pts = [Pt { x: 0, y: 0 }, Pt { x: 1, y: 2 }]
  let none: []string = []
  print("${xs.index_of(-1) ?? -9} ${xs.index_of(5) ?? -9} ${["a", "b"].index_of("b") ?? -9} ")
  print("${pts.index_of(Pt { x: 1, y: 2 }) ?? -9} ${none.index_of("a") ?? -9}")
}
`
	want := "1 -9 1 1 -9"
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != want {
		t.Errorf("got %q; want %q", got, want)
	}
}

// A float's IEEE bits and back (10/19, asked for by Vega's Furnace reader, which assembled
// an `f32` from its bytes by hand): exact patterns, checked against Python's `struct`.
func TestExec_FloatBits(t *testing.T) {
	t.Parallel()
	const src = `
module main
let main = () -> void => {
  let one: f32 = 1.0
  let neg: f64 = -2.5
  let pi_bits: u32 = 0x40490FDB
  let back = one.to_bits().f32_from_bits()
  print("${one.to_bits()} ${neg.to_bits()} ${f64(pi_bits.f32_from_bits())} ${neg.to_bits().f64_from_bits()} ${back.to_bits() == one.to_bits()}")
}
`
	want := "1065353216 13836183955189006336 3.1415927410125732 -2.5 true"
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != want {
		t.Errorf("got %q; want %q", got, want)
	}
}

// **A local named like a prelude receiver method shadows nothing a method call meets**, so
// lyra-W001 is silent for it (10/19): `xs.any(p)` beside a local `any` still reaches the
// method. A prelude free function still warns, since its call is the free one, and so does a
// module's own receiver method, which is code its author is writing.
func TestShadowing_APreludeReceiverMethodIsNotShadowed(t *testing.T) {
	t.Parallel()
	diags := checkWithPreludeDiagnostics(t, `
module main
struct Box { n: i64 }
let peek = (self: Box) -> i64 => self.n
let main = () -> void => {
  let xs = [1, 2]
  let any = xs.any((n) => n > 1)
  let index = "abc".index("b")
  let program_args = 3
  let peek = 4
  print("${any} ${index ?? 0} ${program_args} ${peek}")
}
`)
	var shadowed []string
	for _, d := range diags {
		if strings.HasSuffix(d, "shadows a name declared in an outer scope") {
			shadowed = append(shadowed, strings.Fields(d)[0])
		}
	}
	if strings.Join(shadowed, ",") != "program_args,peek" {
		t.Errorf("W001 on %v; want exactly program_args and peek", shadowed)
	}
}
