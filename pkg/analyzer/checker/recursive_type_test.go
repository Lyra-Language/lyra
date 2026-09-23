package checker_test

import (
	"testing"

	"github.com/Lyra-Language/lyra/pkg/analyzer/checker"
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/parser"
)

func checkRecursiveTypes(t *testing.T, source string) []diag.Diagnostic {
	t.Helper()
	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	c := collector.NewCollector([]byte(source))
	program, _, _, errs := c.Collect(tree.RootNode())
	if len(errs) > 0 {
		t.Fatalf("collector errors: %v", errs)
	}
	return checker.CheckRecursiveTypes(program)
}

func assertNoRecursiveTypeErrors(t *testing.T, source string) {
	t.Helper()
	errs := checkRecursiveTypes(t, source)
	if len(errs) != 0 {
		t.Errorf("expected no recursive-type errors, got %d:", len(errs))
		for _, e := range errs {
			t.Errorf("  %s", e.Message)
		}
	}
}

func assertRecursiveTypeError(t *testing.T, source string, wantTypeName string) {
	t.Helper()
	errs := checkRecursiveTypes(t, source)
	if len(errs) == 0 {
		t.Fatalf("expected a recursive-type error for %q, got none", wantTypeName)
	}
	for _, e := range errs {
		if e.Code != "lyra-E014" {
			t.Errorf("wrong code: got %q, want \"lyra-E014\"", e.Code)
		}
	}
	// Verify the expected type name appears in at least one error message.
	found := false
	for _, e := range errs {
		if containsString(e.Message, wantTypeName) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected error mentioning %q; got: %v", wantTypeName, errs)
	}
}

func containsString(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		func() bool {
			for i := 0; i+len(sub) <= len(s); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}())
}

// --- Direct self-reference ---

func TestRecursiveType_DirectSelfRef_Struct_Error(t *testing.T) {
	assertRecursiveTypeError(t, `
		struct Tree {
			value: i64,
			left: Tree,
		}
	`, "Tree")
}

func TestRecursiveType_DirectSelfRef_Data_Error(t *testing.T) {
	assertRecursiveTypeError(t, `
		data List = Nil | Cons(i64, List)
	`, "List")
}

func TestRecursiveType_DirectSelfRef_DataInlineRecord_Error(t *testing.T) {
	assertRecursiveTypeError(t, `
		data Tree = Nil | Node { left: Tree, right: Tree, value: i64 }
	`, "Tree")
}

// --- Shared field annotation breaks the cycle ---
//
// A recursive cycle is broken only by a `shared` field (there is no
// declaration-level allocation flavor): the field holds a pointer, so the type
// has finite by-value size.

func TestRecursiveType_SharedFieldAnnotation_Ok(t *testing.T) {
	assertNoRecursiveTypeErrors(t, `
		struct Tree {
			value: i64,
			left: shared Tree,
		}
	`)
}

func TestRecursiveType_SharedParamAnnotation_Ok(t *testing.T) {
	assertNoRecursiveTypeErrors(t, `
		data List = Nil | Cons(i64, shared List)
	`)
}

// --- Weak field breaks the cycle too ---
//
// A `weak` reference is a non-owning pointer (pointer-sized), so it breaks a
// recursive size cycle exactly like `shared` — `weak` is the intended answer to
// reference-cycle *leaks* among shared values (it also happens to break the size
// cycle).

func TestRecursiveType_WeakFieldAnnotation_Ok(t *testing.T) {
	assertNoRecursiveTypeErrors(t, `
		struct Node {
			value: i64,
			parent: weak Node,
		}
	`)
}

func TestRecursiveType_WeakParamAnnotation_Ok(t *testing.T) {
	assertNoRecursiveTypeErrors(t, `
		data List = Nil | Cons(i64, weak List)
	`)
}

func TestRecursiveType_MutualRecursion_OneWeak_Ok(t *testing.T) {
	assertNoRecursiveTypeErrors(t, `
		struct Foo {
			bar: weak Bar,
		}
		struct Bar {
			foo: Foo,
		}
	`)
}

// --- Mutual recursion ---

func TestRecursiveType_MutualRecursion_Error(t *testing.T) {
	errs := checkRecursiveTypes(t, `
		struct Foo {
			bar: Bar,
		}
		struct Bar {
			foo: Foo,
		}
	`)
	if len(errs) == 0 {
		t.Fatal("expected recursive-type error for mutual recursion, got none")
	}
}

func TestRecursiveType_MutualRecursion_OneShared_Ok(t *testing.T) {
	// The Foo→Bar edge is a `shared` field, so Foo holds a pointer to Bar and
	// the by-value cycle is broken — no size-bound issue.
	assertNoRecursiveTypeErrors(t, `
		struct Foo {
			bar: shared Bar,
		}
		struct Bar {
			foo: Foo,
		}
	`)
}

// --- Non-recursive types stay clean ---

func TestRecursiveType_NoRecursion_Struct_Ok(t *testing.T) {
	assertNoRecursiveTypeErrors(t, `
		struct Point {
			x: i64,
			y: i64,
		}
		struct Line {
			start: Point,
			end: Point,
		}
	`)
}

func TestRecursiveType_NoRecursion_Data_Ok(t *testing.T) {
	assertNoRecursiveTypeErrors(t, `
		data Color = Red | Green | Blue
		data Shape = Circle(i64) | Square(i64)
	`)
}

func TestRecursiveType_NoRecursion_InlineRecord_Ok(t *testing.T) {
	assertNoRecursiveTypeErrors(t, `
		data Expr = Lit(i64) | Pair { left: i64, right: i64 }
	`)
}

// A **union** containing itself by value has no finite size, exactly as a struct does —
// and worse in one respect: a union's size is computed *from* its members before its
// LLVM body exists, so an unreported cycle is unbounded recursion in `unionSizeAndAlign`
// rather than a diagnostic.
//
// It was found by probing behaviours rather than by reading switches, which is what rule
// 8 prescribes for a new type kind: `union R { x: R, y: u32 }` checked clean and built
// forever.
func TestRecursiveTypes_UnionSelfReferenceIsRefused(t *testing.T) {
	assertRecursiveTypeError(t, `union R { x: R, y: u32 }`, "R")
}

// Through a struct, so the cycle is found across both kinds rather than only within one.
//
// **Which of the two names the diagnostic carries is not pinned**, and asserting one was a
// flaky test: the dependency graph is a map, so the DFS enters the cycle at whichever key
// Go's iteration order hands it first. It reported "U" for weeks and then "S" after an
// unrelated change perturbed the map. Either is correct — both types are in the cycle —
// so the assertion is that a cycle is reported at all.
func TestRecursiveTypes_UnionThroughAStructIsRefused(t *testing.T) {
	errs := checkRecursiveTypes(t, `
union U { s: S, n: u32 }
struct S { u: U }
`)
	if len(errs) == 0 {
		t.Fatalf("expected a recursive-type error for a union/struct cycle")
	}
}

// A union member that is a *pointer* to itself is finite and legal — which is how a C
// linked structure is written, and the shape the FFI actually needs.
func TestRecursiveTypes_UnionThroughAPointerIsFine(t *testing.T) {
	assertNoRecursiveTypeErrors(t, `union U { next: ^U, n: u32 }`)
}

// **A cycle through a generic type's argument is a cycle.** `Maybe<Expr>` holds its
// payload inline, so a `Lambda` reached through one is contained by value exactly as a
// bare field would be — and `collectByValueNames` had no case for a parameterized type at
// all, its closing comment listing the kind among those "bounded by construction".
//
// It was not merely unreported. `ownership.go` states the invariant it rests on — "a
// recursive type's cycle must pass through a `shared` field (lyra-E014), which is managed,
// so the recursion returns before re-entering the cycle" — so a cycle this check misses is
// a pass walking a type it was promised cannot exist. The symptom was a **stack overflow
// out of lyrac**, which also swallowed the diagnostics already produced, since a process
// that dies prints nothing. Found 09/23 by the bootstrap's AST, whose `Lambda` wants an
// optional child.
func TestRecursiveTypes_CycleThroughAGenericArgument(t *testing.T) {
	for _, c := range []struct {
		name   string
		source string
		want   bool // want an E014
	}{
		{
			"a data type reached through Maybe",
			`data Expr = Lit(i64) | Lam(Lambda)
struct Lambda { body: Maybe<Expr> }`,
			true,
		},
		{
			// The spelling the diagnostic tells you to write: `shared` on the argument
			// breaks the cycle there exactly as it does on a bare field.
			"shared on the argument breaks it",
			`data Expr = Lit(i64) | Lam(Lambda)
struct Lambda { body: Maybe<shared Expr> }`,
			false,
		},
		{
			// **A cycle through an array is finite and must stay legal.** `[]Expr` is a
			// box pointer, which is how `std.json`'s `JsonValue` is written — flagging
			// this would refuse the shape the standard library already uses.
			"an array breaks it",
			`data Expr = Lit(i64) | Many([]Expr)`,
			false,
		},
		{
			// A generic instantiation that is not a cycle at all: the check must look at
			// the arguments rather than refusing every parameterized field.
			"a generic field with no cycle",
			`struct P { n: i64 }
struct Holder { p: Maybe<P> }`,
			false,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			diags := checkRecursiveTypes(t, c.source)
			if got := len(diags) > 0; got != c.want {
				t.Errorf("E014 reported = %v, want %v (diagnostics: %v)", got, c.want, diags)
			}
		})
	}
}
