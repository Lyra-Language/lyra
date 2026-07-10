package checker_test

import (
	"testing"

	"github.com/Lyra-Language/lyra/pkg/analyzer/checker"
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector"
	"github.com/Lyra-Language/lyra/pkg/parser"
)

func parseCollectAndCheckTry(t *testing.T, source string) []checker.TryOutsideResultError {
	t.Helper()
	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	c := collector.NewCollector([]byte(source))
	program, symTable, _, _ := c.Collect(tree.RootNode())
	return checker.CheckTryOutsideResult(program, symTable)
}

func assertNoTryErrors(t *testing.T, errs []checker.TryOutsideResultError) {
	t.Helper()
	if len(errs) > 0 {
		t.Errorf("expected no errors, got %d: %v", len(errs), errs)
	}
}

func assertTryErrorCount(t *testing.T, errs []checker.TryOutsideResultError, count int) {
	t.Helper()
	if len(errs) != count {
		t.Errorf("expected %d error(s), got %d: %v", count, len(errs), errs)
	}
}

// TestTry_NoDiag_InsideResultFn verifies that `?` inside a Result-returning
// function is valid.
func TestTry_NoDiag_InsideResultFn(t *testing.T) {
	source := `
let parseBoth = (a: string, b: string) -> Result<i64, ParseError> => {
    let x = parse(a)?
    let y = parse(b)?
    Ok(x)
}`
	assertNoTryErrors(t, parseCollectAndCheckTry(t, source))
}

// TestTry_NoDiag_InsideMaybeFn verifies that `?` inside a Maybe-returning
// function is valid.
func TestTry_NoDiag_InsideMaybeFn(t *testing.T) {
	source := `
let firstField = (s: string) -> Maybe<i64> => {
    let x = lookup(s)?
    Some(x)
}`
	assertNoTryErrors(t, parseCollectAndCheckTry(t, source))
}

// TestTry_Diag_TopLevel verifies that a top-level `?` is rejected.
func TestTry_Diag_TopLevel(t *testing.T) {
	source := `let x = parse(s)?`
	errs := parseCollectAndCheckTry(t, source)
	assertTryErrorCount(t, errs, 1)
	want := "`?` can only be used inside a function returning Result or Maybe"
	if errs[0].Message != want {
		t.Errorf("message = %q, want %q", errs[0].Message, want)
	}
}

// TestTry_Diag_InsidePlainFn verifies that `?` inside a function returning a
// non-Result/Maybe type is rejected.
func TestTry_Diag_InsidePlainFn(t *testing.T) {
	source := `
let f = (s: string) -> i64 => {
    let x = parse(s)?
    x
}`
	assertTryErrorCount(t, parseCollectAndCheckTry(t, source), 1)
}

// TestTry_Diag_InsideNestedPlainFn verifies that `?` in a plain lambda nested
// inside a Result-returning function is rejected (nearest enclosing wins).
func TestTry_Diag_InsideNestedPlainFn(t *testing.T) {
	source := `
let outer = (s: string) -> Result<i64, E> => {
    let inner = (t: string) -> i64 => {
        parse(t)?
    }
    Ok(0)
}`
	assertTryErrorCount(t, parseCollectAndCheckTry(t, source), 1)
}

// TestTry_NoDiag_ResultFnNestedInsidePlainFn verifies that a Result-returning
// lambda nested inside a plain function can use `?`.
func TestTry_NoDiag_ResultFnNestedInsidePlainFn(t *testing.T) {
	source := `
let f = (s: string) -> i64 => {
    let inner = (t: string) -> Result<i64, E> => {
        parse(t)?
    }
    0
}`
	assertNoTryErrors(t, parseCollectAndCheckTry(t, source))
}

// TestTry_Diag_InsideNonCanonicalResultFn verifies the context check uses
// canonical identity, not the bare name: a function returning a same-named but
// differently-shaped `data Result` (Foo/Bar, not Ok/Err) is not a valid `?`
// context, so `?` inside it is rejected — matching the typechecker's operand
// check rather than being silently accepted on the name alone.
func TestTry_Diag_InsideNonCanonicalResultFn(t *testing.T) {
	source := `
data Result<a, b> = Foo a | Bar b
let f = (n: i64) -> Result<i64, i64> => {
    parse(n)?
}`
	assertTryErrorCount(t, parseCollectAndCheckTry(t, source), 1)
}

// TestTry_NoDiag_InsideBuiltinMarkedFn verifies the context check honors an
// `@builtin(Result)` marker on a differently-named type: `?` inside an
// Either-returning function is valid.
func TestTry_NoDiag_InsideBuiltinMarkedFn(t *testing.T) {
	source := `
@builtin(Result)
data Either<t, e> = Ok t | Err e
let f = (s: string) -> Either<i64, i64> => {
    parse(s)?
}`
	assertNoTryErrors(t, parseCollectAndCheckTry(t, source))
}

// TestTry_Diag_MultipleOutside verifies that multiple bad `?` operators each
// produce an error.
func TestTry_Diag_MultipleOutside(t *testing.T) {
	source := `
let a = parse(x)?
let b = parse(y)?`
	assertTryErrorCount(t, parseCollectAndCheckTry(t, source), 2)
}
