package checker_test

import (
	"strings"
	"testing"

	"github.com/Lyra-Language/lyra/pkg/analyzer/checker"
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/parser"
)

// checkInertModifiers runs just the collector (the pass is AST-only) and returns
// the inert-borrow-modifier warnings.
func checkInertModifiers(t *testing.T, source string) []diag.Diagnostic {
	t.Helper()
	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	program, _, _, collectErrs := collector.NewCollector([]byte(source)).Collect(tree.RootNode())
	if len(collectErrs) > 0 {
		t.Fatalf("collector errors: %v", collectErrs)
	}
	return checker.CheckInertBorrowModifiers(program)
}

func assertInertModifiers(t *testing.T, source string, want int) []diag.Diagnostic {
	t.Helper()
	got := checkInertModifiers(t, source)
	if len(got) != want {
		t.Fatalf("expected %d inert-modifier warning(s), got %d: %v", want, len(got), got)
	}
	for _, d := range got {
		if d.Code != diag.CodeInertBorrowModifier {
			t.Errorf("wrong code: got %q, want %q", d.Code, diag.CodeInertBorrowModifier)
		}
		if d.Severity != diag.SeverityWarning {
			t.Errorf("inert modifier should be a warning, got severity %v", d.Severity)
		}
	}
	return got
}

// --- flagged: a borrow/ownership modifier on a scalar primitive ---

func TestInert_OwnScalar(t *testing.T) {
	got := assertInertModifiers(t, `let f = (n: own i64) -> i64 => n`, 1)
	if !strings.Contains(got[0].Message, "`own`") || !strings.Contains(got[0].Message, "i64") {
		t.Errorf("message should name the modifier and type: %q", got[0].Message)
	}
}

func TestInert_RefScalar(t *testing.T) {
	assertInertModifiers(t, `let f = (b: ref bool) -> bool => b`, 1)
}

func TestInert_MutScalar(t *testing.T) {
	assertInertModifiers(t, `let f = (n: mut i16) -> i16 => n`, 1)
}

// `rune` is a copied scalar, flagged like any other.
func TestInert_MutRune(t *testing.T) {
	assertInertModifiers(t, `let f = (c: mut rune) -> rune => c`, 1)
}

func TestInert_FloatAndUnsigned(t *testing.T) {
	assertInertModifiers(t, `let f = (x: own f64, y: ref u8) -> f64 => x`, 2)
}

// Each inert parameter is flagged independently.
func TestInert_MultipleParams(t *testing.T) {
	assertInertModifiers(t, `let f = (a: own i64, b: i64, c: mut i32) -> i64 => a`, 2)
}

// A nested lambda's parameters are checked too.
func TestInert_NestedLambda(t *testing.T) {
	assertInertModifiers(t, `let outer = () -> i64 => {
  let inner = (n: own i64) -> i64 => n
  inner(5)
}`, 1)
}

// --- not flagged ---

// A bare parameter declares no convention, so there's nothing inert.
func TestInert_BareScalar_Ok(t *testing.T) {
	assertInertModifiers(t, `let f = (n: i64) -> i64 => n`, 0)
}

// `string` is a managed fat pointer, so `own string` is a real move — never flagged.
func TestInert_OwnString_Ok(t *testing.T) {
	assertInertModifiers(t, `let f = (s: own string) -> u8 => 1`, 0)
}

func TestInert_RefString_Ok(t *testing.T) {
	assertInertModifiers(t, `let f = (s: ref string) -> u8 => 2`, 0)
}

// A `shared` struct is a managed reference — `own`/`ref` are meaningful.
func TestInert_OwnSharedStruct_Ok(t *testing.T) {
	assertInertModifiers(t, `struct Person { name: string, }
let f = (p: own shared Person) -> u8 => 1`, 0)
}

// An aggregate can own managed fields, so `own` on it is meaningful even for a
// stack struct.
func TestInert_OwnStackStruct_Ok(t *testing.T) {
	assertInertModifiers(t, `struct Box { v: i64, }
let f = (b: own Box) -> i64 => b.v`, 0)
}

// A generic type parameter is not a scalar — at monomorphization it may be managed,
// and `own` must stay uniform across type variables (Perceus). Never flagged.
func TestInert_GenericParam_Ok(t *testing.T) {
	assertInertModifiers(t, `let id = (x: own t) -> t => x`, 0)
}
