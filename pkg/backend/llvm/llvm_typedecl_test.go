package llvm

import (
	"strings"
	"testing"
)

// Type-declaration lowering (lowerTypeDeclarations/lowerTypeDefinitions) emits a
// named LLVM struct type for each top-level tuple/struct decl. There is no way to
// *use* such a value in a runnable program yet (no construction, field access, or
// struct-returning main — main must return u8), so these tests assert on the
// emitted IR text, the same level as the other TestEmit_* cases. TestExec_
// TypeDeclModuleIsValid additionally runs the module through clang to prove the
// type defs are well-formed IR, not just the right text.

// TestEmit_TupleTypeDecl: a named tuple lowers to an LLVM struct of its element
// types, named after the tuple (not TupleType.GetName()'s full "Point(i32, i32)"
// rendering — the key is the bare declared name).
func TestEmit_TupleTypeDecl(t *testing.T) {
	got, err := emitSource(t, "tuple Point(i32, i32)\nlet main = () -> u8 => 0\n")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "%Point = type { i32, i32 }") {
		t.Errorf("missing tuple type def:\n%s", got)
	}
}

// TestEmit_StructTypeDecl: a struct lowers to an LLVM struct of its field types,
// in declaration order. Covers the scalar lowerType cases beyond plain ints —
// bool → i1 and f64 → double — so a wrong primitive mapping shows up here.
func TestEmit_StructTypeDecl(t *testing.T) {
	src := `
struct Node {
  value: i64,
  flag: bool,
  ratio: f64,
}
let main = () -> u8 => 0
`
	got, err := emitSource(t, src)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "%Node = type { i64, i1, double }") {
		t.Errorf("missing/incorrect struct type def:\n%s", got)
	}
}

// TestEmit_MultipleTypeDecls: every top-level type decl is emitted, tuples and
// structs alike, each keyed by its own name.
func TestEmit_MultipleTypeDecls(t *testing.T) {
	src := `
tuple Pair(u8, bool)
struct S {
  a: i16,
  b: f32,
}
let main = () -> u8 => 0
`
	got, err := emitSource(t, src)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"%Pair = type { i8, i1 }",
		"%S = type { i16, float }",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q:\n%s", want, got)
		}
	}
}

// TestEmit_ComposedTypeDecl: a struct whose field is another named type lowers
// that field to a reference to the field type's struct (%Point), not by inlining
// or erroring. This is the payoff of declaring every named type (an opaque
// placeholder) before defining any body — the field's UnresolvedType resolves
// against structTypes during the definition pass.
func TestEmit_ComposedTypeDecl(t *testing.T) {
	src := `
tuple Point(i32, i32)
struct Line {
  a: Point,
  b: Point,
}
let main = () -> u8 => 0
`
	got, err := emitSource(t, src)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "%Line = type { %Point, %Point }") {
		t.Errorf("composed type should reference %%Point:\n%s", got)
	}
}

// TestEmit_ForwardReferencedTypeDecl: a type used by an earlier declaration than
// its own (Line references Point, but Point is declared *after* Line in source)
// still lowers. The two-pass declare-all-then-define ordering makes declaration
// order irrelevant — declaring by name comes first, so Point exists by the time
// Line's fields are lowered.
func TestEmit_ForwardReferencedTypeDecl(t *testing.T) {
	src := `
struct Line {
  a: Point,
  b: Point,
}
tuple Point(i32, i32)
let main = () -> u8 => 0
`
	got, err := emitSource(t, src)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"%Line = type { %Point, %Point }",
		"%Point = type { i32, i32 }",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q:\n%s", want, got)
		}
	}
}

// TestEmit_UnsupportedTypeDecl_Error: a type-decl kind the backend doesn't lower
// yet (here a `data` sum type) fails loudly rather than being silently skipped —
// the whole build errors so no half-lowered module escapes.
func TestEmit_UnsupportedTypeDecl_Error(t *testing.T) {
	_, err := emitSource(t, "data Color = Red | Green | Blue\nlet main = () -> u8 => 0\n")
	if err == nil {
		t.Fatal("expected an error for an unsupported (data) type declaration")
	}
	if !strings.Contains(err.Error(), "unsupported type") {
		t.Errorf("expected an 'unsupported type' error, got: %v", err)
	}
}

// TestExec_TypeDeclModuleIsValid compiles a module that carries type
// declarations and runs it, proving the emitted type defs are valid IR clang
// accepts (a malformed type def would be rejected at compile, not just look
// wrong in a string match). The type is unused by main today — construction and
// field access aren't lowered yet — so this pins "the type defs don't break the
// module" while main still returns its value.
func TestExec_TypeDeclModuleIsValid(t *testing.T) {
	src := `
tuple Point(i32, i32)
struct Node {
  value: i64,
  flag: bool,
}
let main = () -> u8 => 42
`
	if got := buildAndRun(t, src); got != 42 {
		t.Errorf("module with type decls exited %d; want 42", got)
	}
}
