package llvm

import (
	"os/exec"
	"strings"
	"testing"
)

// A `type` alias is transparent, so by the time codegen runs there should be nothing
// left of it. These are the exec cases that prove the alias reaches the same machine
// code the spelled-out type does.
func TestExec_TypeAliases(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
		want int
	}{
		{
			// The case aliases exist for: a function type, where the double parens (a
			// single *tuple* parameter) cannot be spelled away, only named.
			"alias of a function type",
			`alias Op = ((i64, i64)) -> i64
			 let apply = (g: Op, p: (i64, i64)) -> i64 => g(p)
			 let main = () -> u8 => u8(apply(((a, b)) => a * b, (3, 4)))`,
			12,
		},
		{
			"alias of a primitive",
			`alias Id = i64
			 let double = (n: Id) -> Id => n * 2
			 let main = () -> u8 => u8(double(21))`,
			42,
		},
		{
			// An alias holds the aliased type *itself*, so a struct alias is the case
			// where the backend would declare and define Pt's LLVM struct a second
			// time under the name Point if lowerTypeDecl did not skip aliases.
			"alias of a struct",
			`struct Pt { x: i64, y: i64 }
			 alias Point = Pt
			 let sum = (p: Point) -> i64 => p.x + p.y
			 let main = () -> u8 => {
			   let p = Pt { x: 3, y: 4 }
			   u8(sum(p))
			 }`,
			7,
		},
		{
			"alias in return position",
			`alias Op = (i64) -> i64
			 let mk = (n: i64) -> Op => (x: i64) -> i64 => x + n
			 let main = () -> u8 => {
			   let f = mk(1)
			   u8(f(6))
			 }`,
			7,
		},
		{
			// A chain: the backend expands one hop and recurses, since the alias's own
			// recorded type comes back as another named type.
			"alias chain",
			`struct Pt { x: i64 }
			 alias A = Pt
			 alias B = A
			 let get = (p: B) -> i64 => p.x
			 let main = () -> u8 => {
			   let p = Pt { x: 9 }
			   u8(get(p))
			 }`,
			9,
		},
		{
			// A field typed through an alias of a function type is callable. The backend
			// asked whether the field was a LambdaType of the alias's *name* and refused
			// `h.f(4)` as "unsupported method call" until stripNewtype saw aliases (10/07).
			"function-typed struct field via an alias",
			`alias Op = (i64) -> i64
			 struct H { f: Op }
			 let main = () -> u8 => {
			   let k = 3
			   let h = H { f: (x: i64) -> i64 => x + k }
			   u8(h.f(4))
			 }`,
			7,
		},
		{
			// A method on an alias receiver, overloaded beside another receiver: the
			// member is emitted under its written head and reached through the
			// typechecker's resolved callee (10/07, both spellings were refused).
			"method call on an alias receiver",
			`alias A = []i64
			 let total = (self: A) -> i64 => self[0] + self[1]
			 let total = (self: string) -> i64 => self.len()
			 let main = () -> u8 => {
			   let ys: A = [20, 22]
			   let zs: []i64 = [1, 2]
			   u8(ys.total() + zs.total() - "abc".total())
			 }`,
			42,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := buildAndRun(t, c.src); got != c.want {
				t.Errorf("%s: exited %d; want %d", c.name, got, c.want)
			}
		})
	}
}

// TestEmit_StructAliasEmitsOneType: an alias must not duplicate the type it names.
// A struct alias holds the very NamedStructType the struct's own declaration holds,
// so without the IsAlias skip in lowerTypeDecl/lowerTypeDef the module would carry
// two definitions of the same layout — which llir would emit under two names, and
// which would then disagree at any boundary that crossed them.
func TestEmit_StructAliasEmitsOneType(t *testing.T) {
	t.Parallel()
	got, err := emitSource(t, `struct Pt { x: i64, y: i64 }
	 alias Point = Pt
	 let sum = (p: Point) -> i64 => p.x + p.y
	 let main = () -> u8 => {
	   let p = Pt { x: 1, y: 2 }
	   u8(sum(p))
	 }`)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(got, "%Pt = type"); n != 1 {
		t.Errorf("want exactly 1 definition of %%Pt, got %d:\n%s", n, got)
	}
	if strings.Contains(got, "%Point = type") {
		t.Errorf("the alias emitted a type of its own:\n%s", got)
	}
}

// An alias of a **managed** type, in every position a value of it can be held. The
// ownership pass asked IsManaged of the alias's *name*, which cannot see through it, and
// then walked the resolved type's components — for `[]i64` the elements, owning nothing.
// So `let ys: A = [1, 2]` was not an owning position: the literal was released as a
// temporary straight after the store and the binding never framed, and `ys.len()` read a
// freed box (10/07). A binding was a use-after-free; a struct field, tuple element or
// `data` payload of the alias was a leak, since the copy and the drop agreed on "nothing".
//
// Each case runs in a helper, not `main`, so LeakSanitizer sees the frame gone — a
// pointer still in main's frame at exit reads as reachable.
func TestExec_ManagedTypeAliasesASan(t *testing.T) {
	t.Parallel()
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Skip("clang not found on PATH; skipping ASan test")
	}
	if !asanAvailable(t, clang) {
		t.Skip("ASan runtime not available; skipping")
	}
	cases := []struct {
		name string
		src  string
		want int
	}{
		{"let of an alias of []i64", `alias A = []i64
let work = () -> i64 => {
  let ys: A = [1, 2]
  ys.len() * 10 + ys[1]
}
let main = () -> u8 => u8(work())`, 22},
		{"let of aliases of []bool and [][]bool", `alias B = []bool
alias G = [][]bool
let work = () -> i64 => {
  let g: G = [[true, false], [false, true]]
  let r: B = g[1]
  if r[1] { g.len() } else { 0 }
}
let main = () -> u8 => u8(work())`, 2},
		{"let of an alias of string holding a heap string", `alias S = string
let work = () -> i64 => {
  let a = "ab"
  let s: S = a ++ "cd"
  s.len()
}
let main = () -> u8 => u8(work())`, 4},
		{"var, reassignment and push", `alias A = []i64
let work = () -> i64 => {
  var ys: A = [1, 2]
  var zs: A = [7]
  zs = [3, 4, 5]
  ys.push(6)
  ys.len() + ys[2] + zs[2]
}
let main = () -> u8 => u8(work())`, 14},
		{"struct field: copy, then field reassignment", `alias A = []i64
struct Pt { xs: A, n: i64 }
let work = () -> i64 => {
  var p = Pt { xs: [1, 2, 3], n: 1 }
  let q = p
  p.xs = [9]
  p.xs.len() + q.xs[2]
}
let main = () -> u8 => u8(work())`, 4},
		{"tuple element and array element", `alias A = []i64
alias T = (A, string)
let work = () -> i64 => {
  let t: T = ([1, 2], "z" ++ "w")
  let u = t
  let rows: []A = [[1], [2, 3]]
  u.0[1] + rows[1][1] + u.1.len()
}
let main = () -> u8 => u8(work())`, 7},
		{"data payload", `alias A = []i64
data Shape = Poly(A) | Dot
let work = () -> i64 => {
  let s = Poly([1, 2, 3])
  let t = s
  match t {
    Poly(xs) => xs[2],
    Dot => 0,
  }
}
let main = () -> u8 => u8(work())`, 3},
		{"return type, own parameter and closure capture", `alias A = []i64
alias S = string
let mk = () -> A => [1, 2, 3]
let take = (xs: own A) -> i64 => xs.len()
let work = () -> i64 => {
  let a: A = mk()
  let s: S = "a" ++ "b"
  let f = () => a[2] + s.len()
  take(mk()) + f()
}
let main = () -> u8 => u8(work())`, 8},
		{"interior writes through an alias of []string", `alias SS = []string
struct W { v: SS, n: i64 }
let work = () -> i64 => {
  let a = "x"
  var w = W { v: [a ++ "1", a ++ "2"], n: 0 }
  w.v = [a ++ "3"]
  var rows: []SS = [[a ++ "4"], [a ++ "5"]]
  rows[0] = [a ++ "6", a ++ "7"]
  var t: (SS, i64) = ([a ++ "8"], 1)
  t.0 = [a ++ "9"]
  w.v.len() + rows[0].len() + t.0[0].len()
}
let main = () -> u8 => u8(work())`, 5},
		{
			// Already right before the fix — a fixed array is not managed itself, so the
			// walk reached its managed elements — and kept beside the others because it
			// is the sibling a fix to the managed case could break.
			"fixed arrays via an alias", `alias F = [2][]i64
alias FS = [2]string
let work = () -> i64 => {
  let f: F = #[[1, 2], [3]]
  var g: F = f
  g[0] = [5, 6, 7]
  let x = "q"
  let h: FS = #[x ++ "1", x ++ "22"]
  f[0][1] + g[0][2] + h[1].len()
}
let main = () -> u8 => u8(work())`, 12},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := buildAndRunASan(t, clang, c.src); got != c.want {
				t.Errorf("%s: exited %d; want %d", c.name, got, c.want)
			}
		})
	}
}

// A prelude generic over the alias: `Maybe<A>` owns what `A` owns only once the alias
// resolves inside the instantiation's payload. Its own test because `Maybe` needs the
// resolving front half that emitSource skips.
func TestExec_ManagedAliasInMaybeASan(t *testing.T) {
	t.Parallel()
	if !asanAvailable(t, lookClang(t)) {
		t.Skip("ASan runtime not available; skipping")
	}
	src := `alias A = []i64
let work = () -> i64 => {
  let m: Maybe<A> = Some([7, 8])
  let n = m
  match n {
    Some(v) => v[1],
    None => 0,
  }
}
let main = () -> u8 => u8(work())`
	if got := buildAndRunASanWithPrelude(t, src); got != 8 {
		t.Errorf("exited %d; want 8", got)
	}
}

// The binding is framed and its initializer kept, which is the fault seen statically: a
// release of the array before its reads, and none at scope exit, was the miscompile.
// Conservation catches both halves without an ASan runtime.
func TestEmit_ManagedAliasBindingIsFramed(t *testing.T) {
	t.Parallel()
	ir, err := emitSource(t, `alias A = []i64
let main = () -> u8 => {
  let ys: A = [1, 2]
  u8(ys.len() + ys[1])
}`)
	if err != nil {
		t.Fatal(err)
	}
	main := ir[strings.Index(ir, "define i32 @main"):]
	main = main[:strings.Index(main, "\n}")]
	read := strings.Index(main, "@lyra_panic_index_out_of_bounds")
	release := strings.LastIndex(main, "@lyra_rc_release")
	if read < 0 || release < 0 || release < read {
		t.Errorf("want the array released after its last read, at scope exit:\n%s", main)
	}
}
