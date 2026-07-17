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
		if got := buildAndRun(t, c.src); got != c.want {
			t.Errorf("%s: exited %d; want %d", c.name, got, c.want)
		}
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

// TestEmit_DataSharedPayloadConstruction_Deferred: constructing a variant with a
// `shared` payload field (a recursive `Cons`) needs ref-counted-box allocation,
// which isn't lowered yet, so it errors loudly.
func TestEmit_DataSharedPayloadConstruction_Deferred(t *testing.T) {
	src := `data List = Nil | Cons(i64, shared List)
	 let main = () -> u8 => {
	   let c = Cons(1, Nil)
	   0
	 }`
	_, err := emitSource(t, src)
	if err == nil {
		t.Fatal("expected an error: `shared` payload construction is not implemented yet")
	}
	if !strings.Contains(err.Error(), "shared") {
		t.Errorf("expected a `shared` payload error, got: %v", err)
	}
}

// TestEmit_DataByValueNamedPayload: a variant payload that references another
// named type *by value* is sized by resolving the reference through the symbol
// table (resolveForLayout) — `Wrap(P)` with P = { i64 } → an 8-byte payload,
// blob [1 x i64]. (A recursive reference must instead be `shared` per lyra-E014,
// which is pointer-sized — see the "recursive shared" case above.)
func TestEmit_DataByValueNamedPayload(t *testing.T) {
	src := "struct P {\n  x: i64,\n}\ndata W = Empty | Wrap(P)\nlet main = () -> u8 => 0\n"
	got, err := emitSource(t, src)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "%W = type { i8, [1 x i64] }") {
		t.Errorf("by-value named payload should size to [1 x i64]:\n%s", got)
	}
}
