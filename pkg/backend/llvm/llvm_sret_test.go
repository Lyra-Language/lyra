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
