package checker_test

import (
	"testing"

	"github.com/Lyra-Language/lyra/pkg/analyzer/checker"
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/parser"
)

func parseCollectAndCheckAwait(t *testing.T, source string) []diag.Diagnostic {
	t.Helper()
	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	c := collector.NewCollector([]byte(source))
	program, _, _, _ := c.Collect(tree.RootNode())
	return checker.CheckAwaitOutsideAsync(program)
}

func assertNoAwaitErrors(t *testing.T, errs []diag.Diagnostic) {
	t.Helper()
	if len(errs) > 0 {
		t.Errorf("expected no errors, got %d: %v", len(errs), errs)
	}
}

func assertAwaitErrorCount(t *testing.T, errs []diag.Diagnostic, count int) {
	t.Helper()
	if len(errs) != count {
		t.Errorf("expected %d error(s), got %d: %v", count, len(errs), errs)
	}
}

// TestAwait_NoDiag_InsideAsync verifies that await inside an async function is valid.
func TestAwait_NoDiag_InsideAsync(t *testing.T) {
	source := `
let fetchData = async (url: string) -> string => {
    let resp = await fetch(url)
    resp
}`
	assertNoAwaitErrors(t, parseCollectAndCheckAwait(t, source))
}

// TestAwait_Diag_TopLevel verifies that a top-level await is rejected.
func TestAwait_Diag_TopLevel(t *testing.T) {
	source := `let x = await somePromise`
	errs := parseCollectAndCheckAwait(t, source)
	assertAwaitErrorCount(t, errs, 1)
	want := "await expression outside of an async function"
	if errs[0].Message != want {
		t.Errorf("message = %q, want %q", errs[0].Message, want)
	}
}

// TestAwait_Diag_InsideNonAsyncFunction verifies that await inside a regular
// (non-async) function is rejected.
func TestAwait_Diag_InsideNonAsyncFunction(t *testing.T) {
	source := `
let f = (url: string) -> string => {
    let resp = await fetch(url)
    resp
}`
	assertAwaitErrorCount(t, parseCollectAndCheckAwait(t, source), 1)
}

// TestAwait_Diag_InsideNestedNonAsync verifies that await inside a non-async
// lambda nested inside an async function is rejected.
func TestAwait_Diag_InsideNestedNonAsync(t *testing.T) {
	source := `
let outer = async () -> string => {
    let inner = (url: string) -> string => {
        await fetch(url)
    }
    await fetch("https://example.com")
}`
	assertAwaitErrorCount(t, parseCollectAndCheckAwait(t, source), 1)
}

// TestAwait_NoDiag_AsyncNestedInsideNonAsync verifies that an async lambda
// nested inside a regular function can use await.
func TestAwait_NoDiag_AsyncNestedInsideNonAsync(t *testing.T) {
	source := `
let f = () -> string => {
    let inner = async (url: string) -> string => {
        await fetch(url)
    }
    "done"
}`
	assertNoAwaitErrors(t, parseCollectAndCheckAwait(t, source))
}

// TestAwait_Diag_MultipleAwaitsOutside verifies that multiple bad awaits each produce an error.
func TestAwait_Diag_MultipleAwaitsOutside(t *testing.T) {
	source := `
let a = await promiseA
let b = await promiseB`
	assertAwaitErrorCount(t, parseCollectAndCheckAwait(t, source), 2)
}
