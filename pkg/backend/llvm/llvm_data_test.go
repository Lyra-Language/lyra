package llvm

import (
	"strings"
	"testing"
)

// `data` (sum) type declarations lower to a tagged union `%T = { iTAG, blob }`
// (DATA_LAYOUT.md): a tag sized to the variant count, then a payload blob sized to
// the largest variant. This is only the type *shape* — construction and `match`
// are separate later steps — so these assert the emitted type def, plus a
// clang-validity check that the module is well-formed.

func TestEmit_DataTypeLayout(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
		want string
	}{
		// All-nullary (an enum): just the tag, no payload blob.
		{
			"enum",
			`data Color = Red | Green | Blue
			 let main = () -> u8 => 0`,
			"%Color = type { i8 }",
		},
		// Positional payloads: the blob is sized to the largest variant
		// (Rect's two i64s = 16 bytes → [2 x i64]).
		{
			"positional payloads",
			`data Shape = Circle(i64) | Rect(i64, i64)
			 let main = () -> u8 => 0`,
			"%Shape = type { i8, [2 x i64] }",
		},
		// Mixed variant shapes: nullary, a narrow payload, and the widest — the
		// blob follows the widest (C's two i64s), not B's u8.
		{
			"mixed variants",
			`data Mix = A | B(u8) | C(i64, i64)
			 let main = () -> u8 => 0`,
			"%Mix = type { i8, [2 x i64] }",
		},
		// A bool payload is one byte → [1 x i8].
		{
			"bool payload",
			`data Opt = No | Yes(bool)
			 let main = () -> u8 => 0`,
			"%Opt = type { i8, [1 x i8] }",
		},
		// Recursive type, made finite by the `shared` recursive field (lyra-E014):
		// `shared List` is a pointer, so Cons's payload is { i64, ptr } = 16 bytes.
		{
			"recursive shared",
			`data List = Nil | Cons(i64, shared List)
			 let main = () -> u8 => 0`,
			"%List = type { i8, [2 x i64] }",
		},
	}
	for _, c := range cases {
		got, err := emitSource(t, c.src)
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if !strings.Contains(got, c.want) {
			t.Errorf("%s: emitted IR missing %q:\n%s", c.name, c.want, got)
		}
	}
}

// TestExec_DataModuleIsValid compiles + runs a module carrying data declarations,
// proving the emitted tagged-union type defs are valid IR clang accepts (a
// malformed type is rejected at compile). The data type is unused by main today
// (no construction yet), so main just returns its value.
func TestExec_DataModuleIsValid(t *testing.T) {
	t.Parallel()
	src := "data Shape = Circle(i64) | Rect(i64, i64)\ndata Color = Red | Green | Blue\nlet main = () -> u8 => 42\n"
	if got := buildAndRun(t, src); got != 42 {
		t.Errorf("module with data decls exited %d; want 42", got)
	}
}

// Data value construction materializes the tagged union: alloca the union, store
// the variant's tag, and (for a payload variant) store the payload struct into
// the blob. Without `match` a payload can't be read back yet, so these verify the
// module is valid IR that runs (buildAndRun) with the construction present, and
// pin the construction instruction shape (TestEmit_DataConstructionIR).
func TestExec_DataConstruction(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
		want int
	}{
		// Nullary variant of an enum: just the tag.
		{
			"enum variant",
			`data Color = Red | Green | Blue
			 let main = () -> u8 => {
			   let c = Green
			   7
			 }`,
			7,
		},
		// Positional payload.
		{
			"positional payload",
			`data Shape = Circle(i64) | Rect(i64, i64)
			 let main = () -> u8 => {
			   let s = Rect(3, 4)
			   1
			 }`,
			1,
		},
		// Narrow payload field: the u8 field's literal takes the field width
		// (typechecker propagation), so the payload store is correctly typed.
		{
			"narrow payload field",
			`data Byte = Zero | Wrap(u8)
			 let main = () -> u8 => {
			   let b = Wrap(200)
			   2
			 }`,
			2,
		},
		// A by-value struct payload: the nested struct is constructed and stored
		// into the variant payload.
		{
			"struct payload",
			`struct Pt {
			   x: i64,
			 }
			 data Box = Empty | Full(Pt)
			 let main = () -> u8 => {
			   let box = Full(Pt { x: 5 })
			   3
			 }`,
			3,
		},
		// A nullary variant of a recursive type (Nil) — no payload; the union is
		// still the { tag, blob } sized for Cons.
		{
			"nullary of recursive type",
			`data List = Nil | Cons(i64, shared List)
			 let main = () -> u8 => {
			   let n = Nil
			   4
			 }`,
			4,
		},
	}
	for _, c := range cases {
		t.Run("", func(t *testing.T) {
			t.Parallel()
			if got := buildAndRun(t, c.src); got != c.want {
				t.Errorf("%s: exited %d; want %d", c.name, got, c.want)
			}
		})
	}
}

// TestExec_DataNarrowTuplePayload constructs a data value whose payload is a
// *tuple* of narrow ints (`Wrapped((20, 22))` with `Wrapped((u8, u8))`), matches
// on it, and sums the fields — exercising the full round-trip end-to-end. The
// tuple literal's element leaves (`20`, `22`) must narrow to the declared u8
// width (typechecker propagation now recurses into a tuple-literal payload), so
// the payload store is `{ i8, i8 }`; before the fix the leaves stayed i64 and
// the backend panicked building the payload (`insertvalue elem type mismatch,
// expected i8, got i64`). Exit code 42 == 20 + 22.
func TestExec_DataNarrowTuplePayload(t *testing.T) {
	t.Parallel()
	src := `data Wrap = Wrapped((u8, u8))
	 let f = (w: Wrap) -> u8 => match w { Wrapped((a, b)) => a + b }
	 let main = () -> u8 => f(Wrapped((20, 22)))`
	if got := buildAndRun(t, src); got != 42 {
		t.Errorf("narrow tuple payload: exited %d; want 42", got)
	}
}

// TestEmit_DataConstructionIR pins the construction shape: alloca the union,
// store the tag at field 0, and (payload variant) insertvalue the fields and
// store the payload struct into field 1.
func TestEmit_DataConstructionIR(t *testing.T) {
	t.Parallel()
	got, err := emitSource(t, `data Shape = Circle(i64) | Rect(i64, i64)
	 let main = () -> u8 => {
	   let s = Rect(3, 4)
	   0
	 }`)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"alloca %Shape", // materialize the union
		"getelementptr", // address the tag / payload fields
		"store i8 1",    // Rect's tag (declaration index 1)
		"insertvalue",   // build the payload struct
		"load %Shape",   // read back the first-class value
	} {
		if !strings.Contains(got, want) {
			t.Errorf("construction IR missing %q:\n%s", want, got)
		}
	}
}

// TestEmit_DataSharedPayloadConstruction: constructing a variant with a `shared`
// payload field (a recursive `Cons`) heap-allocates the nested value in a
// ref-counted box (the `shared List` field is a pointer to that box).
func TestEmit_DataSharedPayloadConstruction(t *testing.T) {
	t.Parallel()
	got, err := emitSource(t, `data List = Nil | Cons(i64, shared List)
	 let main = () -> u8 => {
	   let c = Cons(1, Nil)
	   0
	 }`)
	if err != nil {
		t.Fatalf("shared payload construction should lower now: %v", err)
	}
	// The nested Nil is boxed for the `shared List` field.
	if !strings.Contains(got, "call i8* @lyra_rc_alloc") {
		t.Errorf("expected a box allocation for the shared payload:\n%s", got)
	}
}

// TestEmit_DataByValueNamedPayload: a variant payload that references another
// named type *by value* is sized by resolving the reference through the symbol
// table (resolveForLayout) — `Wrap(P)` with P = { i64 } → an 8-byte payload,
// blob [1 x i64]. (A recursive reference must instead be `shared` per lyra-E014,
// which is pointer-sized — see the "recursive shared" case above.)
func TestEmit_DataByValueNamedPayload(t *testing.T) {
	t.Parallel()
	src := "struct P {\n  x: i64,\n}\ndata W = Empty | Wrap(P)\nlet main = () -> u8 => 0\n"
	got, err := emitSource(t, src)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "%W = type { i8, [1 x i64] }") {
		t.Errorf("by-value named payload should size to [1 x i64]:\n%s", got)
	}
}

// TestExec_ScreamingCaseConstructors: an all-caps / single-capital data
// constructor works end to end in expression position — the collector
// reclassifies the const_identifier form into the same nodes a PascalCase
// constructor produces, so construction, match, and applied payloads all lower.
func TestExec_ScreamingCaseConstructors(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		src  string
		want int
	}{
		{
			// Nullary enum: construct N, match it back.
			"nullary enum",
			`data Dir = N | S | E | W
			 let toNum = (d: Dir) -> u8 => match d { N => 0, S => 1, E => 2, W => 3 }
			 let main = () -> u8 => toNum(E)`,
			2,
		},
		{
			// Applied constructor: FOO(7), then match the payload out.
			"applied constructor",
			`data Wrap = FOO(i64)
			 let unwrap = (w: Wrap) -> i64 => match w { FOO(n) => n }
			 let main = () -> u8 => u8(unwrap(FOO(7)))`,
			7,
		},
	}
	for _, c := range cases {
		t.Run("", func(t *testing.T) {
			t.Parallel()
			if got := buildAndRun(t, c.src); got != c.want {
				t.Errorf("%s: exited %d; want %d", c.name, got, c.want)
			}
		})
	}
}

// A generic data type instantiated at a **tuple** — `Maybe<(i64, i64)>` — is
// constructible, returnable and destructurable.
//
// It was none of those. The collector packs a *declared* positional payload into one
// anonymous tuple (`Rect(i64, i64)` → `Params = [TupleType{i64, i64}]`, and `Circle(i64)`
// likewise), and `FieldTypes` unwrapped any lone anonymous-tuple param back into that
// list. After substitution `Some t`[t := (i64, i64)] is byte-for-byte the same shape, so
// it was unwrapped too: the backend read `Some` as taking two arguments while the
// typechecker read it as taking one, and **no spelling satisfied both** — `Some((7, 2))`
// was rejected by the backend, `Some(7, 2)` by the typechecker. The type simply could not
// be used.
//
// Only the declaration can tell the two apart, so the collector records it
// (`DataTypeConstructor.Packed`) and FieldTypes unwraps only a packed param.
func TestExec_GenericDataInstantiatedAtATuple(t *testing.T) {
	t.Parallel()
	const src = `
module main
data Opt<t> = Nil | One t
let mk = (n: i64) -> Opt<(i64, i64)> => One((n, n * 2))
let fst = (o: Opt<(i64, i64)>) -> i64 => match o { Nil => -1, One((a, _)) => a }
let snd = (o: Opt<(i64, i64)>) -> i64 => match o { Nil => -1, One((_, b)) => b }
let main = () -> void => {
  println("${fst(mk(7))} ${snd(mk(7))} ${fst(Nil)}");
}
`
	out, _ := buildAndRunCapture(t, src)
	if got := strings.TrimSpace(out); got != "7 14 -1" {
		t.Errorf("generic data at a tuple: got %q, want \"7 14 -1\"", got)
	}
}

// The other half of that fix, and the one it could plausibly have broken: a **declared**
// positional payload must still flatten. `Rect(i64, i64)` is constructed with two
// arguments and matched with two sub-patterns, and `Circle(i64)` — packed even though it
// has one field — with one. If `Packed` were not set, or not carried through the
// backend's rebuild of a constructor in `resolveForLayout`, these break instead.
func TestExec_DeclaredPositionalPayloadStillFlattens(t *testing.T) {
	t.Parallel()
	const src = `
module main
data Shape = Rect(i64, i64) | Circle(i64) | Empty
let area = (s: Shape) -> i64 => match s {
  Rect(w, h) => w * h,
  Circle(r) => r * r * 3,
  Empty => 0,
}
let main = () -> void => {
  println("${area(Rect(3, 4))} ${area(Circle(5))} ${area(Empty)}");
}
`
	out, _ := buildAndRunCapture(t, src)
	if got := strings.TrimSpace(out); got != "12 75 0" {
		t.Errorf("declared positional payload: got %q, want \"12 75 0\"", got)
	}
}

// A generic data type instantiated at a **named** tuple, returned across a function
// boundary and from a trait method.
//
// `TupleType.String()` rendered a named tuple with its elements (`Pos(i64, i64)`), and
// that string is mangled into a specialization's symbol (`typetable.TypeSymbol`). So one
// instantiation was reachable under two names — `Maybe$Pos` where `Pos` was still an
// UnresolvedType, `Opt$Pos_i64__i64_` where it had been resolved — and a function
// returning one emitted `ret %Opt$Pos_i64__i64_` against a declared `%"Opt$Pos"`
// result, which clang rejects outright.
//
// A named tuple is nominal (TypesEqual's TupleType case says so), so its String is now
// its name. **Return position is what the test has to exercise**: building one at a call
// site or in a `let` worked throughout, because neither crosses a boundary where the
// declared type and the value's type are mangled independently.
func TestExec_GenericDataInstantiatedAtANamedTuple(t *testing.T) {
	t.Parallel()
	const src = `
module main
tuple Pos(i64, i64)
data Opt<t> = Nil | One t
trait Locate { at: (Self) -> Opt<Pos> }
impl Locate for i64 { at = (self) => One(Pos(self, self + 1)) }
let mk = (n: i64) -> Opt<Pos> => One(Pos(n, n * 2))
let first = (m: Opt<Pos>) -> i64 => match m { Nil => -1, One(p) => p.0 }
let viaBound<t> where t: Locate = (v: t) -> i64 => match v.at() { Nil => -1, One(p) => p.1 }
let main = () -> void => {
  println("${first(mk(7))} ${first(Nil)} ${viaBound(9)}");
}
`
	out, _ := buildAndRunCapture(t, src)
	if got := strings.TrimSpace(out); got != "7 -1 10" {
		t.Errorf("generic data at a named tuple: got %q, want \"7 -1 10\"", got)
	}
}

// A generic type instantiated at a **type alias** is the same instantiation as one at the
// alias's target, and emits one symbol for both.
//
// `instantiationSymbol` mangled `ParameterizedType.TypeArguments` as written, so a
// declared `Maybe<Idx>` came out `Maybe$Idx` while the value constructed for it came out
// `Maybe$i64` — one instantiation under two names, and the function emitted
// `ret %Maybe$i64` against a declared `%"Maybe$Idx"` result, which clang rejects.
//
// It is the named-tuple rendering bug from the other direction: there a *nominal* type was
// expanded into its elements when it should have stayed its name, here a *transparent* one
// stayed its name when it should have been expanded. Both now ask resolveNamedType, which
// already draws that line, rather than each carrying its own rule.
//
// The last line is the point of the test rather than a bonus: an alias is not a type of
// its own, so `a` and `b` below must resolve to a single emitted layout.
func TestExec_GenericInstantiatedAtATypeAlias(t *testing.T) {
	t.Parallel()
	const src = `
module main
type Idx = i64
data Opt<t> = Nil | One t
let viaAlias = (n: i64) -> Opt<Idx> => One(n)
let viaConcrete = (n: i64) -> Opt<i64> => One(n)
let get = (o: Opt<i64>) -> i64 => match o { Nil => -1, One(v) => v }
let main = () -> void => {
  println("${get(viaAlias(4))} ${get(viaConcrete(5))} ${get(Nil)}");
}
`
	out, _ := buildAndRunCapture(t, src)
	if got := strings.TrimSpace(out); got != "4 5 -1" {
		t.Errorf("generic at a type alias: got %q, want \"4 5 -1\"", got)
	}
}

// A type alias is transparent **everywhere**, not only at the head of a generic
// argument. This shape — an aliased tuple inside a generic, used through a trait method
// with guard-clause returns and a defaulted aliased parameter — is the prelude's
// `Needle` with the names changed, and it tripped four separate raw-annotation readers,
// each of which resolved nothing and so compared `Idx` against `i64`:
//
//   - checkReturnStmt read tc.enclosingRet as *written*, so a nested `return
//     Some((n, 1))` was rejected against `Maybe<(Idx, Len)>` while the identical tail
//     expression passed — the declared/stored asymmetry both set sites share;
//   - solveTypeVars unified the raw `offset: Idx` against a resolved i64 argument and
//     blamed the type variable ("cannot infer t from these arguments");
//   - instantiateSignature rebuilt the checked signature from the raw annotations, so
//     the argument check rejected what inference had just accepted ("argument 3: cannot
//     assign i64 to Idx");
//   - instantiationSymbol mangled the argument as written, so the declared return
//     emitted `%"Opt$..Idx.."` against a value built as `%"Opt$..i64.."` and clang
//     rejected the module — which is why resolveForLayout (recursive, and correct to
//     collapse newtypes: they are transparent to codegen) resolves the argument first.
//
// One test rather than four because the readers fail serially: each fix exposes the
// next, and the composed shape is the only spelling that proves all of them at once.
func TestExec_TypeAliasIsTransparentInGenerics(t *testing.T) {
	t.Parallel()
	const src = `
module main
type Idx = i64
type Len = i64
data Opt<t> = Nil | One t
trait Finder {
  find: (Self, i64) -> Opt<(Idx, Len)>
}
impl Finder for i64 {
  find = (self, limit) => {
    if limit < 0 {
      return Nil
    }
    if self > limit {
      return One((self, 1))
    }
    Nil
  }
}
let locate<t> where t: Finder = (v: t, limit: Idx = 0) -> Opt<(Idx, Len)> => v.find(limit)
let start = (o: Opt<(i64, i64)>) -> i64 => match o { Nil => -1, One((a, _)) => a }
let main = () -> void => {
  let n = 9;
  // defaulted aliased param, explicit arg, guard-clause Nil, and the alias/concrete
  // instantiations unifying (locate returns Opt<(Idx, Len)>, start takes Opt<(i64, i64)>)
  println("${start(locate(n))} ${start(locate(n, 20))} ${start(locate(n, -1))}");
}
`
	out, _ := buildAndRunCapture(t, src)
	if got := strings.TrimSpace(out); got != "9 -1 -1" {
		t.Errorf("alias transparency: got %q, want \"9 -1 -1\"", got)
	}
}
