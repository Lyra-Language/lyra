package llvm

import (
	"strings"
	"testing"
)

// Destructuring in a `for-in` header — `for (k, v) in pairs`.
//
// It is a **desugaring**, entirely in the collector: the element binds to a synthetic
// unforgeable name and the body gains a `let (k, v) = <elem>` at the top, so nothing after
// the collector knows the form exists. That is the same erasure the juxtaposed constructor
// and a match arm's bare jump get, and it is what makes the feature cheap: the typechecker,
// ownership, captures, the range pass and the backend all keep reading one loop shape, and
// the destructuring is the code that already ran for `let (a, b) = e`.
//
// The alternative — a Pattern field beside Key/Value — would have made every one of those
// passes newly wrong about a binder, which is the most expensive family in hazard 8.
func TestExec_ForInDestructuring(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, src, want string }{
		{
			name: "a tuple element",
			src: `
  let xs: [](i64, string) = [(10, "a"), (20, "b")]
  for (n, s) in xs { println("${n}${s}") }`,
			want: "10a\n20b",
		},
		{
			// The two-binding form is `for <index>, <element>`, so the pattern belongs in
			// the *second* slot — the first is the index.
			name: "an index alongside a destructured element",
			src: `
  let xs: [](i64, string) = [(10, "a"), (20, "b")]
  for i, (n, s) in xs { println("${i}:${n}${s}") }`,
			want: "0:10a\n1:20b",
		},
		{
			name: "a wildcard inside the pattern",
			src: `
  let xs: [](i64, string) = [(10, "a"), (20, "b")]
  for (_, s) in xs { println(s) }`,
			want: "a\nb",
		},
		{
			// Each loop pushes its own scope, so the synthetic element name shadows rather
			// than colliding, and an inner loop's bindings do not disturb an outer one's.
			name: "nested destructuring loops",
			src: `
  let xs: [](i64, string) = [(1, "a"), (2, "b")]
  for (n, s) in xs { for (m, t) in xs { println("${n}${s}${m}${t}") } }`,
			want: "1a1a\n1a2b\n2b1a\n2b2b",
		},
		{
			// Over elements of different types. The case above passed while the inner
			// loop's pattern took the *outer* holder's type — the same there: the holder's
			// use spanned the holder, so the lookup that wants a binding ending before it
			// skipped the inner loop's (10/27). Here that was a type error, and with types
			// that happened to fit, IR clang refused.
			name: "nested destructuring loops over different elements",
			src: `
  let outer: [](i64, bool) = [(1, true), (2, false)]
  let inner: [](i32, string) = [(7, "seven")]
  for (n, b) in outer { for (m, s) in inner { if m == 7 && b { println("${n}:${m}${s}") } } }`,
			want: "1:7seven",
		},
		{
			// The element name is unforgeable, so a body binding cannot collide with it —
			// and an ordinary name reused inside the body shadows as it always would.
			name: "a body binding of the same name shadows",
			src: `
  let xs: [](i64, string) = [(1, "a")]
  for (n, s) in xs { let n2 = n + 1; println("${n2}${s}") }`,
			want: "2a",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			src := "let main = () -> void => {" + c.src + "\n}\n"
			if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != c.want {
				t.Errorf("got %q; want %q", got, c.want)
			}
		})
	}
}

// A destructured element holds a **managed** value, released once per iteration.
//
// The desugaring is what makes this correct for free: `let (n, s) = elem` is the binding
// ownership already knows how to account for, so the string in each pair is retained and
// released by the rules that governed it before this form existed. Under ASan because the
// failure mode is a use-after-free or a leak rather than a wrong answer.
func TestExec_ForInDestructuringManagedElementsASan(t *testing.T) {
	t.Parallel()
	src := `
let main = () -> void => {
  var pairs: [](string, string) = []
  for i in 0..<4 { pairs.push(("k${i}", "v${i}")) }
  var acc = ""
  for (k, v) in pairs { acc = acc ++ k ++ "=" ++ v ++ ";" }
  println(acc)
  for i, (k, _) in pairs { println("${i}${k}") }
}
`
	want := "k0=v0;k1=v1;k2=v2;k3=v3;\n0k0\n1k1\n2k2\n3k3"
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != want {
		t.Errorf("got %q; want %q", got, want)
	}
}

// A struct pattern takes the element apart as a tuple pattern does (10/29), in either slot of
// the one form it fits — renamed fields and a managed field included, under ASan since a
// destructured string is retained and released per iteration.
func TestExec_ForInStructPatternASan(t *testing.T) {
	t.Parallel()
	src := `
struct Pt { x: i64, y: i64, tag: string }
let main = () -> u8 => {
  var points: []Pt = []
  for i in 1..<=3 { points.push(Pt { x: i, y: i * 2, tag: "p${i}" }) }
  var t = 0
  for { x, y } in points { t = t + x * 10 + y }
  var tags = ""
  for i, { tag, y: w } in points { tags = tags ++ tag; t = t + i * 100 + w }
  if tags != "p1p2p3" { return 1 }
  u8(t - 200)
}
`
	// 12 + 24 + 36 = 72; then 300 + (2 + 4 + 6) = 312; 384 - 200
	if got := buildAndRunASanWithPrelude(t, src); got != 184 {
		t.Errorf("got %d; want 184", got)
	}
}

// The two-name form's element takes a written type (10/29), settling literal elements at
// it as the one-name form does; the index stays an i64.
func TestExec_ForInTwoNamesTypeTheElement(t *testing.T) {
	t.Parallel()
	out := buildAndRunWithPrelude(t, `module main
let main = () -> void => {
  let step: f32 = 2.0
  var total: f32 = 0.0
  var at = 0
  for i, k: f32 in [0.25, 0.5, 1.0] { total = total + k * step; at = at + i }
  var small: u16 = 0
  for i, b: u8 in [200, 250] { small = small + u16(b) + u16(i) }
  println("${total} ${at} ${small}")
}`, "")
	if got := strings.TrimSpace(out); got != "3.5 3 451" {
		t.Errorf("got %q; want \"3.5 3 451\"", got)
	}
}
