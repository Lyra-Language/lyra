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

// TestEmit_DataByValueNamedPayload_Deferred: a variant payload that references
// another named type *by value* isn't sizeable yet (the param is an
// UnresolvedType), so it errors loudly rather than emitting a wrong layout. (A
// recursive reference must be `shared` per lyra-E014, which *is* sizeable — see
// the "recursive shared" case above.)
func TestEmit_DataByValueNamedPayload_Deferred(t *testing.T) {
	src := "struct P {\n  x: i64,\n}\ndata W = Wrap(P)\nlet main = () -> u8 => 0\n"
	_, err := emitSource(t, src)
	if err == nil {
		t.Fatal("expected an error: a by-value named-type payload isn't sizeable yet")
	}
	if !strings.Contains(err.Error(), "cannot lay out data type") {
		t.Errorf("expected a data-layout error, got: %v", err)
	}
}
