package llvm

import (
	"strings"
	"testing"
)

// A `const` generic parameter read as a value, on a type, and in arithmetic in a size. ASan
// throughout: a size that disagrees with its array is a write past the end.
func TestExec_ConstGenericValues(t *testing.T) {
	t.Parallel()
	const ring = `
struct Ring<t, const N: i64> { items: [N]t, head: i64 }
impl Ring<t, N> {
  pub let capacity = pure (self: ref) -> i64 => N
  pub let first = (self: ref) -> t => self.items[0]
  pub let set = (self: mut, i: i64, v: t) -> void => { self.items[i % N] = v }
}
trait Doubled { doubled: (Self) -> i64 }
impl Doubled for Ring<t, N> { doubled = (self) => N * 2 }
`
	cases := []struct{ name, src, want string }{
		{"a value in a loop's bound and in arithmetic", `
let f<const N: i64> = (xs: ref [N]i64) -> i64 => {
  var c = 0
  for i in 0..<N { c += xs[i] }
  c + N
}
let main = () -> void => { print("${f(#[1, 2, 3])}") }`, "9"},
		{"it shadows a module `const`", `
const N: i64 = 100
let f<const N: i64> = (xs: ref [N]i64) -> i64 => N
let main = () -> void => { print("${f(#[1, 2])} ${N}") }`, "2 100"},
		{"a local `const` shadows it", `
let f<const K: i64> = (xs: ref [K]i64) -> i64 => {
  const K = 9
  K
}
let main = () -> void => { print("${f(#[1])}") }`, "9"},
		{"a closure reads it without capturing", `
let f<const M: i64> = (xs: ref [M]i64) -> i64 => {
  let g = (x: i64) -> i64 => x + M
  g(1)
}
let main = () -> void => { print("${f(#[1, 2])}") }`, "3"},
		{"it has its declared type", `
let f<const B: u8> = (xs: ref [B]i64) -> u8 => B + 1
let main = () -> void => { print("${f(#[1, 2, 3])}") }`, "4"},
		{"each specialization has its own", `
let f<const N: i64> = (xs: ref [N]i64) -> i64 => N
let main = () -> void => { print("${f(#[1])} ${f(#[1, 2])}") }`, "1 2"},
		{"a generic passes its own on", `
let f<const N: i64> = (xs: ref [N]i64) -> i64 => N
let g<const A: i64> = (xs: ref [A]i64) -> i64 => f(xs) + A
let main = () -> void => { print("${g(#[1, 2])}") }`, "4"},
		{"as a dynamic repeat's count", `
let f<const N: i64> = (xs: ref [N]i64) -> []i64 => [7; N]
let main = () -> void => { print("${f(#[1, 2, 3]).len()}") }`, "3"},
		{"as a fixed repeat's count, managed elements", `
let f<const N: i64> = (xs: ref [N]i64) -> [N]string => #["a" ++ "${1}"; N]
let main = () -> void => { print(f(#[1, 2])[1]) }`, "a1"},
		{"`[N + 1]t` as a return", `
let push<const N: i64> = (xs: ref [N]i64, x: i64) -> [N + 1]i64 => {
  var out: [N + 1]i64 = #[0; N + 1]
  for i in 0..<N { out[i] = xs[i] }
  out[N] = x
  out
}
let main = () -> void => {
  let p = push(#[1, 2], 3)
  print("${p.len()} ${p[2]}")
}`, "3 3"},
		{"`[N + M]t` from two arguments", `
let concat<const N: i64, const M: i64> = (a: ref [N]string, b: ref [M]string) -> [N + M]string => {
  var out = #[""; N + M]
  for i in 0..<N { out[i] = a[i] }
  for i in 0..<M { out[N + i] = b[i] }
  out
}
let main = () -> void => {
  let c = concat(#["a" ++ "${1}", "b"], #["c"])
  print("${c.len()} ${c[0]} ${c[2]}")
}`, "3 a1 c"},
		{"`[N + 1]t` solved from an argument", `
let tail<const N: i64> = (xs: ref [N + 1]i64) -> [N]i64 => {
  var out = #[0; N]
  for i in 0..<N { out[i] = xs[i + 1] }
  out
}
let main = () -> void => {
  let t = tail(#[7, 8, 9])
  print("${t.len()} ${t[0]}")
}`, "2 8"},
		{"`[2 * N]t` solved by division", `
let half<const N: i64> = (xs: ref [2 * N]i64) -> i64 => N
let main = () -> void => { print("${half(#[1, 2, 3, 4])}") }`, "2"},
		{"two unknowns settled by a later argument", `
let f<const N: i64, const M: i64> = (a: ref [N + M]i64, b: ref [N]i64) -> i64 => M
let main = () -> void => { print("${f(#[1, 2, 3], #[1])}") }`, "2"},
		{"a generic calling across an offset", `
let tail<const N: i64> = (xs: ref [N + 1]i64) -> i64 => N
let g<const K: i64> = (xs: ref [K + 2]i64) -> i64 => tail(xs)
let main = () -> void => { print("${g(#[1, 2, 3, 4])}") }`, "3"},
		{"a module `const` in arithmetic", `
const MAX: i64 = 4
let main = () -> void => {
  let xs: [MAX * 2 + 1]i64 = #[0; 9]
  print("${xs.len()}")
}`, "9"},
		{"a struct, its size from a field", ring + `
let main = () -> void => {
  let r = Ring { items: #["a" ++ "${1}", "b"], head: 0 }
  print("${r.capacity()} ${r.first()} ${r.doubled()}")
}`, "2 a1 4"},
		{"a struct, its size written", ring + `
let main = () -> void => {
  var r: Ring<i64, 3> = Ring { items: #[1, 2, 3], head: 0 }
  r.set(4, 9)
  print("${r.items[1]}")
}`, "9"},
		{"a struct, its size a module `const`", ring + `
const CAP: i64 = 4
let make<const N: i64> = (x: i64) -> Ring<i64, N> => Ring { items: #[x; N], head: 0 }
let main = () -> void => {
  let m: Ring<i64, CAP> = make(7)
  print("${m.capacity()} ${m.items[3]}")
}`, "4 7"},
		{"a function over a struct's size", ring + `
let cap_of<t, const N: i64> = (r: ref Ring<t, N>) -> i64 => N
let grow<t, const N: i64> = (r: ref Ring<t, N>, x: t) -> Ring<t, N + 1> => {
  var items = #[x; N + 1]
  for i in 0..<N { items[i] = r.items[i] }
  Ring { items: items, head: r.head }
}
let main = () -> void => {
  let r = Ring { items: #["a" ++ "${1}"], head: 0 }
  let n = grow(r, "b" ++ "${2}")
  print("${cap_of(n)} ${n.items[0]} ${n.items[1]}")
}`, "2 a1 b2"},
		{"a block for one size", `
const CAP: i64 = 2
struct Ring<const N: i64> { items: [N]i64 }
impl Ring<CAP> { pub let two = (self: ref) -> i64 => CAP }
let main = () -> void => {
  let r: Ring<2> = Ring { items: #[1, 2] }
  print("${r.two()}")
}`, "2"},
		{"a named tuple", `
tuple Pair<const N: i64>([N]i64, i64)
let main = () -> void => {
  let p: Pair<2> = Pair(#[1, 2], 3)
  print("${p.0.len()}")
}`, "2"},
		{"a `data` type", `
data Buf<const N: i64> = Full([N]string) | Empty
let count<const N: i64> = (b: Buf<N>) -> i64 => match b {
  Full(xs) => xs.len() + N,
  Empty => 0
}
let main = () -> void => {
  let b: Buf<2> = Full(#["x" ++ "${1}", "y"])
  let e: Buf<2> = Empty
  print("${count(b)} ${count(e)}")
}`, "4 0"},
		{"a `type`", `
type Row<const N: i64> = [N]i64
let row_of<const N: i64> = (xs: [N]i64) -> Row<N> => Row(xs)
let main = () -> void => { print("${row_of(#[1, 2, 3]).len()}") }`, "3"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := strings.TrimSpace(buildAndRunASanStdout(t, c.src)); got != c.want {
				t.Errorf("got %q; want %q", got, c.want)
			}
		})
	}
}

// A generic passing a size on can come to something negative only at specialization, past what
// a call can see. Refused there, in words, rather than as an array of 2^64 - 1.
func TestEmit_NegativeSizeAtSpecialization(t *testing.T) {
	t.Parallel()
	_, err := emitSource(t, `
let less<const N: i64> = (a: ref [N]i64) -> [N - 1]i64 => #[0; N - 1]
let g<const K: i64> = (a: ref [K]i64) -> i64 => less(a).len()
let main = () -> u8 => {
  let e = #[0; 0]
  u8(g(e))
}`)
	if err == nil || !strings.Contains(err.Error(), "comes to -1") {
		t.Fatalf("expected a refusal at the specialization, got %v", err)
	}
}
