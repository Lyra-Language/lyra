package driver_test

import (
	"strings"
	"testing"

	"github.com/Lyra-Language/lyra/pkg/driver"
)

// diagsMatching returns the messages of every diagnostic whose text contains sub.
func diagsMatching(res *driver.Result, sub string) []string {
	var out []string
	for _, d := range res.Diagnostics {
		if strings.Contains(d.Message, sub) {
			out = append(out, d.Message)
		}
	}
	return out
}

// `?` inside a trait-impl method.
//
// The structural half of the try checks reads the *nearest enclosing lambda's* declared
// return type. An impl binds patterns — `parse = (self) => …` — so its lambda carries no
// return type at all: the signature lives on the trait. Every `?` in an impl method was
// therefore reported as being outside a Result/Maybe-returning function, which made the
// operator unusable in the half of the language written as trait impls.
//
// The trait is resolved through LookupTraitFrom at the impl's own location, so a second
// module declaring a trait of the same name cannot redirect the answer (rule 4).
func TestTryOperator_IsAllowedInATraitImplMethod(t *testing.T) {
	res := driver.Analyze([]byte(`
trait Parse { parse: (Self) -> Maybe<i64> }
struct Num { s: string }
impl Parse for Num {
  parse = (self) => {
    let n = self.s.parse_i64()?
    Some(n * 2)
  }
}
let main = () -> void => println("hi")
`))
	if got := diagsMatching(res, "`?` can only be used inside"); len(got) != 0 {
		t.Errorf("`?` rejected in a method whose trait returns Maybe: %v", got)
	}
}

// The other direction: an impl method whose trait method returns a plain type is still not
// a `?` context. Reading the return type from the trait must not become "assume Result".
func TestTryOperator_StillRejectedWhenTheTraitMethodReturnsAPlainType(t *testing.T) {
	res := driver.Analyze([]byte(`
trait Plain { get: (Self) -> i64 }
struct Num { s: string }
impl Plain for Num {
  get = (self) => {
    let n = self.s.parse_i64()?
    n
  }
}
let main = () -> void => println("hi")
`))
	if got := diagsMatching(res, "`?` can only be used inside"); len(got) != 1 {
		t.Errorf("want exactly one rejection for `?` in an i64-returning method, got %d: %v",
			len(got), got)
	}
}

// lyra-E024 is reported once per write, not once per enclosing lambda that captured the name.
//
// A name an inner closure captures is transitively captured by every enclosing one — the
// read walk does not stop at a lambda boundary — and the reporting walk did not stop there
// either, so one write inside a doubly-nested closure produced two identical diagnostics at
// one location. The call site's comment already said writes belong to the lambda that
// performs them; only the walk disagreed.
func TestCapturedAssignment_ReportedOncePerWrite(t *testing.T) {
	res := driver.Analyze([]byte(`
let main = () -> void => {
  var count = 0
  let outer = () -> void => {
    let inner = () -> void => {
      count = count + 1
    }
    inner()
  }
  outer()
}
`))
	if got := diagsMatching(res, `cannot assign to "count"`); len(got) != 1 {
		t.Errorf("want 1 diagnostic for one write in a nested closure, got %d: %v", len(got), got)
	}
}

// A closure that only **writes** a captured binding, never reading it.
//
// `VarReassignmentStmt` keeps its target in a plain `Name string` field rather than an
// IdentifierExpr, so the capture pass's read walk never saw a node for it — and a bare
// `name = value` was the one assignment form with no expression node anywhere (`n += 1` has
// an IdentifierExpr in Left, `p.x = v` an expression path).
//
// The consequence was not a missing diagnostic but a **compiler crash**: with the name
// absent from the capture set the closure environment had no slot for it, and the backend
// dereferenced nil in lowerVarReassignment. Now the name is recorded, so E024 fires and the
// program is rejected for the reason it should be — the write would reach only the closure's
// own copy.
func TestCapturedAssignment_WriteOnlyCaptureIsReportedNotCrashed(t *testing.T) {
	res := driver.Analyze([]byte(`
let main = () -> void => {
  var count = 0
  let bump = () -> void => {
    count = 99
  }
  bump()
  println(count)
}
`))
	if got := diagsMatching(res, `cannot assign to "count"`); len(got) != 1 {
		t.Errorf("want 1 diagnostic for a write-only captured binding, got %d: %v", len(got), got)
	}
}

// A lambda writing to its **own** local of the same name is untouched — the write-only
// recording above must not turn every local reassignment into a capture.
func TestCapturedAssignment_LocalReassignmentIsNotACapture(t *testing.T) {
	res := driver.Analyze([]byte(`
let main = () -> void => {
  let f = () -> i64 => {
    var count = 0
    count = 99
    count
  }
  println(f())
}
`))
	if got := diagsMatching(res, "cannot assign to"); len(got) != 0 {
		t.Errorf("a lambda's own local was reported as a captured assignment: %v", got)
	}
}
