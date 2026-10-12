package llvm

import (
	"strings"
	"testing"
)

// A function returning a large aggregate returns it through the caller's slot (sret.go).
// These run every shape that reaches one, each answering its own exit code: an array and a
// struct holding one; a generic specialization and a trait method; a named function used as
// a value, through its thunk, and a closure (which keeps returning by value); an early
// `return`, a recursive call returned directly, a by-value parameter returned; reassignment,
// self-assignment, and a result handed straight to a `ref` parameter.
func TestExec_LargeAggregatesReturnThroughMemory(t *testing.T) {
	t.Parallel()
	src := `struct Art { width: i64, pixels: [600]u32 }

trait Paint { pure paint: (Self, u32) -> [300]u32 }
struct Brush { step: u32 }
impl Paint for Brush {
  paint = (self, base) => {
    var out: [300]u32 = #[0; 300]
    for i in 0..<300 { out[i] = base + u32(i) * self.step }
    out
  }
}

let ramp = pure (base: u32) -> [1024]u32 => {
  var out: [1024]u32 = #[0; 1024]
  for i in 0..<1024 { out[i] = base + u32(i) }
  out
}

let art = pure (w: i64) -> Art => {
  var pixels: [600]u32 = #[0; 600]
  pixels[599] = 42
  Art { width: w, pixels: pixels }
}

let filled<t> = pure (v: t) -> [500]t => #[v; 500]

let early = pure (stop: bool) -> [512]u32 => {
  var out: [512]u32 = #[1; 512]
  if stop { return out }
  out[0] = 9
  out
}

let down = pure (n: i64) -> [256]u32 => if n == 0 { ramp(0).first_256() } else { down(n - 1) }

impl [1024]u32 {
  let first_256 = pure (self) -> [256]u32 => {
    var out: [256]u32 = #[0; 256]
    for i in 0..<256 { out[i] = self[i] }
    out
  }
}

let same = pure (xs: [300]u32) -> [300]u32 => xs

let pick<const N: i64> = pure (xs: ref [N]u32, i: i64) -> u32 => xs[i]

let call_it = pure (f: () -> [1024]u32) -> u32 => f()[1000]

let zero_ramp = pure () -> [1024]u32 => ramp(0)

let main = () -> u8 => {
  if ramp(5)[1000] != 1005 { return 1 }
  let a = art(3)
  if a.width != 3 || a.pixels[599] != 42 || a.pixels[0] != 0 { return 2 }
  let sevens: [500]u32 = filled(u32(7))
  if sevens[499] != 7 { return 3 }
  if (Brush { step: 2 }).paint(1)[299] != 599 { return 4 }
  if early(true)[0] != 1 || early(false)[0] != 9 || early(false)[511] != 1 { return 5 }
  if down(3)[255] != 255 { return 6 }
  if same(Brush { step: 1 }.paint(0))[200] != 200 { return 7 }
  if pick(ramp(10), 3) != 13 { return 8 }
  if call_it(zero_ramp) != 1000 { return 9 }
  let closure = () -> [1024]u32 => ramp(2)
  if call_it(closure) != 1002 { return 10 }
  var r = ramp(1)
  r = ramp(100)
  if r[0] != 100 { return 11 }
  r = r
  if r[1023] != 1123 { return 12 }
  0
}
`
	ir, err := emitSource(t, src)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"define void @lyra.ramp([1024 x i32]* sret([1024 x i32])",
		"define void @lyra.art(%Art* sret(%Art)",
	} {
		if !strings.Contains(ir, want) {
			t.Errorf("the IR lacks %q", want)
		}
	}
	if strings.Contains(ir, "ret [1024 x i32]") && !strings.Contains(ir, ".closure") {
		t.Errorf("a [1024]u32 is returned by value outside a closure")
	}
	if got := buildAndRun(t, src); got != 0 {
		t.Errorf("case %d failed", got)
	}
}

// A large array of strings returned through memory keeps its references straight: each
// string is retained once into the result and released once with it — under ASan, which
// fails a double release or a leak (on Linux) rather than passing quietly.
func TestExec_ALargeArrayOfStringsReturnsThroughMemory(t *testing.T) {
	t.Parallel()
	src := `let names = (n: i64) -> [64]string => {
  var out: [64]string = #[""; 64]
  for i in 0..<64 { out[i] = "name ${i + n}" }
  out
}

let main = () -> u8 => {
  let a = names(1)
  var b = names(2)
  b = names(3)
  if a[63] != "name 64" || b[0] != "name 3" { return 1 }
  0
}
`
	if got := buildAndRunASanWithPrelude(t, src); got != 0 {
		t.Errorf("exit code %d", got)
	}
}

// A struct with padding inside — a `Result` is a one-byte tag and an 8-aligned payload — is
// copied whole. Its copies were sized by summing field sizes, 7 bytes short of the struct, so
// a value returned through memory and copied into a binding lost the top of its last field:
// `c.pad[39]` read garbage (10/11, found by Vega crashing in its own drop glue).
func TestExec_APaddedStructIsCopiedWhole(t *testing.T) {
	t.Parallel()
	src := `struct Big {
  name: string,
  result: Result<[40]i64, string>,
  pad: [40]i64,
}

let make = pure (n: i64) -> Big =>
  Big { name: "item", result: if n % 2 == 0 { Ok(#[n; 40]) } else { Err("odd") }, pad: #[n; 40] }

impl Big {
  let renamed = pure (self, s: string) -> Big => Big { self | name: s }
}

let main = () -> u8 => {
  var c = make(7)
  c = c.renamed("again")
  if c.pad[39] != 7 || c.name != "again" { return 1 }
  match c.result {
    Ok(_) => { return 2 },
    Err(e) => { if e != "odd" { return 3 } },
  }
  let d = make(8)
  match d.result {
    Ok(xs) => { if xs[39] != 8 { return 4 } },
    Err(_) => { return 5 },
  }
  0
}
`
	if got := buildAndRunASanWithPrelude(t, src); got != 0 {
		t.Errorf("case %d failed", got)
	}
}

// A large aggregate passed by value is a pointer to a copy the caller makes for the call
// (sret.go). Still by value: the callee sees the argument as it was when the call began even
// while the caller's array is written through a `mut` beside it. Every shape that passes one —
// a plain call, an inherent and a trait method's receiver, `==` through an `Eq` impl, a generic
// specialization, a function value through its thunk, recursion, a destructured parameter.
// (`==` through an `Eq` impl needs the prelude: TestExec_ALargeOwnedParameterIsReleasedOnce.)
func TestExec_LargeAggregatesPassByCopy(t *testing.T) {
	t.Parallel()
	src := `struct Grid { cells: [400]i64, tag: i64 }

trait Sum { pure sum: (Self) -> i64 }
impl Sum for Grid {
  sum = (self) => {
    var s = 0
    for c in self.cells { s += c }
    s
  }
}

impl Grid {
  let last = pure (self) -> i64 => self.cells[399]
}

let grid = pure (v: i64) -> Grid => Grid { cells: #[v; 400], tag: v }

let first = pure (xs: [400]i64) -> i64 => xs[0]

let snapshot = (xs: [400]i64, ys: mut [400]i64) -> i64 => {
  ys[0] = 99
  xs[0]
}

let generic_last<t> = pure (xs: [400]t) -> t => xs[399]

let total = pure (g: Grid, n: i64) -> i64 => if n == 0 { g.cells[0] } else { total(g, n - 1) + 1 }

let tagged = pure ({ tag, cells }: Grid) -> i64 => tag + cells[1]

let apply = pure (f: ([400]i64) -> i64, xs: [400]i64) -> i64 => f(xs)

let main = () -> u8 => {
  let g = grid(3)
  if first(g.cells) != 3 { return 1 }
  var a: [400]i64 = #[5; 400]
  if snapshot(a, a) != 5 || a[0] != 99 { return 2 }
  if g.last() != 3 || g.sum() != 1200 { return 3 }
  if generic_last(g.cells) != 3 { return 5 }
  if total(g, 10) != 13 { return 6 }
  if tagged(g) != 6 { return 7 }
  if apply(first, g.cells) != 3 { return 8 }
  0
}
`
	ir, err := emitSource(t, src)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ir, "define i64 @lyra.first([400 x i64]* %p.xs)") {
		t.Errorf("a [400]i64 by value is not passed as a pointer to a copy")
	}
	if got := buildAndRun(t, src); got != 0 {
		t.Errorf("case %d failed", got)
	}
}

// An `own` parameter of a large array of strings, passed by copy, is released once by the
// callee — and an unowned one not at all — under ASan; and `==` on a large struct through its
// `Eq` impl passes both operands by copy.
func TestExec_ALargeOwnedParameterIsReleasedOnce(t *testing.T) {
	t.Parallel()
	src := `let names = (n: i64) -> [64]string => {
  var out: [64]string = #[""; 64]
  for i in 0..<64 { out[i] = "name ${i + n}" }
  out
}

let consume = (xs: own [64]string) -> i64 => xs[63].len()

struct Grid { cells: [400]i64, tag: i64 }

impl Eq for Grid {
  eq = (self, other) => self.tag == other.tag && self.cells[399] == other.cells[399]
}

let grid = pure (v: i64) -> Grid => Grid { cells: #[v; 400], tag: v }
let read = (xs: [64]string) -> i64 => xs[0].len()

let main = () -> u8 => {
  let kept = names(1)
  if read(kept) != 6 { return 1 }
  if consume(names(2)) != 7 { return 2 }
  if kept[63] != "name 64" { return 3 }
  if !(grid(3) == grid(3)) || grid(3) == grid(4) { return 4 }
  0
}
`
	if got := buildAndRunASanWithPrelude(t, src); got != 0 {
		t.Errorf("case %d failed", got)
	}
}

// A by-copy argument is copied as it is made, so the copy is a `memcpy` from where it was
// loaded even when a later argument's code calls something. Copied at the call instead, it was
// a store of the whole value, and `at(table, seven())` on a `[8192]u32` built in 2.3 seconds.
func TestEmit_AByCopyArgumentIsCopiedAsItIsMade(t *testing.T) {
	t.Parallel()
	ir, err := emitSource(t, `var table: [4096]u32 = #[0; 4096]
let at = pure (xs: [4096]u32, i: i64) -> u32 => xs[i]
let seven = () -> i64 => {
  table[0] = 1
  7
}
let main = () -> u8 => u8(at(table, seven()) % 256)
`)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(ir, "store [4096 x i32]") {
		t.Errorf("the argument's copy stored the whole array:\n%s", ir)
	}
}
