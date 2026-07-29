package captures_test

import (
	"strings"
	"testing"

	"github.com/Lyra-Language/lyra/pkg/analyzer/captures"
	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/driver"
)

// analyzeLambdas returns each lambda in the program paired with its captures, in
// source order, so a test can name them positionally. The entry lambda (main) is
// included — it is a lambda like any other and must capture nothing.
func analyzeLambdas(t *testing.T, src string) [][]captures.Capture {
	t.Helper()
	res := driver.Analyze([]byte(src))
	if res.HasErrors() {
		t.Fatalf("unexpected analysis errors: %v", res.Diagnostics)
	}
	var out [][]captures.Capture
	for _, node := range res.Program.Statements {
		stmt, ok := node.(ast.Statement)
		if !ok {
			continue
		}
		ast.WalkStmt(stmt, nil, func(e ast.Expression) bool {
			if fn, ok := e.(*ast.LambdaExpr); ok {
				out = append(out, res.Captures.Of(fn))
			}
			return true
		})
	}
	return out
}

func names(caps []captures.Capture) string {
	var parts []string
	for _, c := range caps {
		parts = append(parts, c.Name)
	}
	return strings.Join(parts, ",")
}

func assertCaptures(t *testing.T, src string, want ...string) {
	t.Helper()
	got := analyzeLambdas(t, src)
	if len(got) != len(want) {
		t.Fatalf("expected %d lambda(s), found %d: %v", len(want), len(got), got)
	}
	for i := range want {
		if n := names(got[i]); n != want[i] {
			t.Errorf("lambda %d: captures %q; want %q", i, n, want[i])
		}
	}
}

// A top-level function reaches only parameters and globals, so it captures
// nothing — including when it calls another top-level function by name.
func TestCaptures_TopLevelFunctionCapturesNothing(t *testing.T) {
	assertCaptures(t, `
let double = (x: i64) -> i64 => x * 2
let quad = (x: i64) -> i64 => double(double(x))
let main = () -> u8 => u8(quad(2))
`, "", "", "")
}

// The base case: a nested lambda reading an enclosing local captures it.
func TestCaptures_NestedLambdaCapturesLocal(t *testing.T) {
	assertCaptures(t, `
let main = () -> u8 => {
  let n = 5
  let addN = (x: i64) -> i64 => x + n
  u8(addN(3))
}
`, "", "n")
}

// A parameter of the enclosing function is captured the same way — this is the
// shape that makes a returned closure useful.
func TestCaptures_CapturesEnclosingParameter(t *testing.T) {
	assertCaptures(t, `
let makeAdder = (n: i64) -> (i64) -> i64 => (x: i64) -> i64 => x + n
let main = () -> u8 => 0
`, "", "n", "")
}

// A lambda's own parameters and locals are not captures.
func TestCaptures_OwnBindingsAreNotCaptures(t *testing.T) {
	assertCaptures(t, `
let main = () -> u8 => {
  let f = (x: i64) -> i64 => {
    let y = x + 1
    y * 2
  }
  u8(f(3))
}
`, "", "")
}

// Every binder form inside the body counts as local, not a capture — a match arm
// binding, a for-in loop variable, and a destructuring declaration are the three
// the collector does not record in a scope, so they have to be recognized here.
func TestCaptures_BinderFormsAreLocal(t *testing.T) {
	assertCaptures(t, `
data Maybe = Some(i64) | None
let main = () -> u8 => {
  let f = (m: Maybe, xs: [3]i64) -> i64 => {
    var total = match m {
      Some(v) => v,
      None => 0,
    }
    for item in xs {
      total = total + item
    }
    let (a, b) = (1, 2)
    total + a + b
  }
  u8(f(Some(1), [1, 2, 3]))
}
`, "", "")
}

// The captured value's recorded type comes along, since the environment layout
// needs it.
func TestCaptures_RecordsTheCapturedType(t *testing.T) {
	got := analyzeLambdas(t, `
let main = () -> u8 => {
  let n: u8 = 5
  let f = () -> u8 => n
  f()
}
`)
	caps := got[1]
	if len(caps) != 1 {
		t.Fatalf("expected 1 capture, got %v", caps)
	}
	if caps[0].Type == nil || caps[0].Type.GetName() != "u8" {
		t.Errorf("capture type = %v; want u8", caps[0].Type)
	}
}

// A capture reaches through an intermediate lambda: the inner one captures `n`,
// and the outer must capture it too or there is nothing for the inner to copy
// from. Falls out of walking reads across the lambda boundary.
func TestCaptures_TransitiveThroughNestedLambda(t *testing.T) {
	assertCaptures(t, `
let main = () -> u8 => {
  let n = 5
  let outer = () -> (i64) -> i64 => (x: i64) -> i64 => x + n
  let inner = outer()
  u8(inner(1))
}
`, "", "n", "n")
}

// Several captures come out sorted by name, so the environment layout an emitted
// closure uses is deterministic across runs.
func TestCaptures_StableOrder(t *testing.T) {
	assertCaptures(t, `
let main = () -> u8 => {
  let zeta = 1
  let alpha = 2
  let mid = 3
  let f = () -> i64 => zeta + alpha + mid
  u8(f())
}
`, "", "alpha,mid,zeta")
}

// A C-style loop's init declaration is reached by the generic AST walker only as
// an *expression* (it walks Init.Value, never the statement), so the counter has
// to be recognized as a binder explicitly. Missing it made every such loop inside
// a lambda read as a capture — and its `i += 1` as a write to one, which the
// captured-assignment check then rejected. Every existing loop-in-a-lambda test
// in the suite failed at once, which is how it was caught.
func TestCaptures_LoopCounterIsLocal(t *testing.T) {
	assertCaptures(t, `
let main = () -> u8 => {
  let f = (n: i64) -> i64 => {
    var total = 0
    for var i = 0; i < n; i += 1 {
      total = total + i
    }
    total
  }
  u8(f(3))
}
`, "", "")
}

// The compiler-provided free functions are reachable from anywhere, so calling
// one is not a capture.
func TestCaptures_BuiltinsAreNotCaptures(t *testing.T) {
	assertCaptures(t, `
let main = () -> u8 => {
  let f = () -> void => println("hi")
  f()
  0
}
`, "", "")
}

// A top-level function referenced from inside a lambda is a global, not a
// capture — it has no environment to be copied into.
func TestCaptures_TopLevelFunctionReferenceIsNotACapture(t *testing.T) {
	assertCaptures(t, `
let double = (x: i64) -> i64 => x * 2
let main = () -> u8 => {
  let f = (x: i64) -> i64 => double(x) + 1
  u8(f(3))
}
`, "", "", "")
}
