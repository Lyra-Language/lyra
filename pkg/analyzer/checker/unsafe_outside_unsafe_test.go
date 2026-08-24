package checker_test

import (
	"testing"

	"github.com/Lyra-Language/lyra/pkg/analyzer/checker"
	"github.com/Lyra-Language/lyra/pkg/analyzer/collector"
	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/parser"
)

func checkUnsafe(t *testing.T, source string) []diag.Diagnostic {
	t.Helper()
	tree, err := parser.Parse(source)
	if err != nil {
		t.Fatalf("Parse error: %v", err)
	}
	c := collector.NewCollector([]byte(source))
	program, _, _, _ := c.Collect(tree.RootNode())
	return checker.CheckUnsafeOutsideUnsafe(program)
}

func assertUnsafeCount(t *testing.T, errs []diag.Diagnostic, want int) {
	t.Helper()
	if len(errs) != want {
		t.Fatalf("expected %d unsafe error(s), got %d: %v", want, len(errs), errs)
	}
}

// --- raw-pointer operations outside unsafe are reported ---

func TestUnsafe_AddressOf_OutsideUnsafe_Error(t *testing.T) {
	assertUnsafeCount(t, checkUnsafe(t, `
let x = 1
let p = &x
`), 1)
}

func TestUnsafe_Deref_OutsideUnsafe_Error(t *testing.T) {
	assertUnsafeCount(t, checkUnsafe(t, `
let v = ptr^
`), 1)
}

func TestUnsafe_PointerWrite_OutsideUnsafe_Error(t *testing.T) {
	// `ptr^ = 42` at top level: the deref-assignment statement is flagged.
	assertUnsafeCount(t, checkUnsafe(t, `
ptr^ = 42
`), 1)
}

// --- the same operations inside an unsafe block are allowed ---

func TestUnsafe_AddressOf_InsideBlock_Ok(t *testing.T) {
	assertUnsafeCount(t, checkUnsafe(t, `
let x = 1
unsafe {
    let p = &x
}
`), 0)
}

func TestUnsafe_DerefAndWrite_InsideBlock_Ok(t *testing.T) {
	assertUnsafeCount(t, checkUnsafe(t, `
unsafe {
    let v = ptr^
    ptr^ = 42
}
`), 0)
}

// --- inside an unsafe function body the operations are allowed ---

func TestUnsafe_InsideUnsafeFunction_Ok(t *testing.T) {
	assertUnsafeCount(t, checkUnsafe(t, `
let load = unsafe (ptr: i64) -> i64 => {
    ptr^
}
`), 0)
}

// --- unsafe-ness does not leak across a function boundary ---

func TestUnsafe_NonUnsafeLambdaInsideBlock_Error(t *testing.T) {
	// A plain lambda defined inside an unsafe block is its own safe context, so
	// the deref in its body is still reported.
	assertUnsafeCount(t, checkUnsafe(t, `
unsafe {
    let f = (ptr: i64) -> i64 => {
        ptr^
    }
}
`), 1)
}

// --- calling an unsafe function ---
//
// Moved to the typechecker suite (raw_pointer_test.go) on 08/19. This pass is syntactic
// and could only match the callee's *name* against the top-level unsafe functions, which
// is hazard 9: an `extern f` made every `f(…)` in the prelude report as an unsafe call,
// `f` there being an ordinary callback parameter. The typechecker resolves the callee
// before deciding, which is the only place that question has an answer.
