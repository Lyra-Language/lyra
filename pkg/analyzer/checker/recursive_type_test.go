package checker_test

import (
	"testing"

	"github.com/Lyra-Language/lyra/pkg/analyzer/checker"
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector"
	"github.com/Lyra-Language/lyra/pkg/parser"
)

func checkRecursiveTypes(t *testing.T, source string) []checker.RecursiveTypeError {
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
