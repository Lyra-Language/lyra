package checker_test

import (
	"strings"
	"testing"

	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/driver"
)

// A closure captures by value, so a write to a captured binding could only ever
// change the closure's own copy — the enclosing binding would be untouched. That
// is rejected (lyra-E024) rather than compiled into a write that silently
// vanishes, which is the same failure a by-value `mut` parameter had.
//
// The check runs on the capture table, so these go through the whole driver
// pipeline rather than the typechecker alone.
func capturedAssignmentErrors(t *testing.T, src string) []diag.Diagnostic {
	t.Helper()
	res := driver.Analyze([]byte(src))
	var out []diag.Diagnostic
	for _, d := range res.Diagnostics {
		if d.Code == diag.CodeCapturedAssignment {
			out = append(out, d)
		}
	}
	return out
}

func assertCapturedAssignment(t *testing.T, src, wantName string) {
	t.Helper()
	got := capturedAssignmentErrors(t, src)
	if len(got) != 1 {
		t.Fatalf("expected exactly 1 captured-assignment error, got %d: %v", len(got), got)
	}
	if !strings.Contains(got[0].Message, `cannot assign to "`+wantName+`"`) {
		t.Errorf("message should name the binding %q: %q", wantName, got[0].Message)
	}
}

func TestCapturedAssignment_PlainAssignment(t *testing.T) {
	assertCapturedAssignment(t, `
let main = () -> u8 => {
  var n = 5
  let bump = () -> i64 => { n = n + 1  n }
  u8(bump())
}
`, "n")
}

func TestCapturedAssignment_CompoundAssignment(t *testing.T) {
	assertCapturedAssignment(t, `
let main = () -> u8 => {
  var n = 5
  let bump = () -> i64 => { n += 1  n }
  u8(bump())
}
`, "n")
}

// A write through a captured *path* is the same lost write, so the check walks
// the assignment target down to the binding it is rooted at.
func TestCapturedAssignment_ThroughAPath(t *testing.T) {
	assertCapturedAssignment(t, `
struct Counter { n: i64 }
let main = () -> u8 => {
  var c = Counter { n: 0 }
  let bump = () -> i64 => { c.n = 1  c.n }
  u8(bump())
}
`, "c")
}

func assertNoCapturedAssignment(t *testing.T, src string) {
	t.Helper()
	if got := capturedAssignmentErrors(t, src); len(got) != 0 {
		t.Errorf("expected no captured-assignment error, got: %v", got)
	}
}

// A lambda writing to its own local is untouched — a local is not a capture.
func TestCapturedAssignment_OwnLocalIsFine(t *testing.T) {
	assertNoCapturedAssignment(t, `
let main = () -> u8 => {
  let f = () -> i64 => {
    var total = 0
    for var i = 0; i < 3; i += 1 {
      total = total + i
    }
    total
  }
  u8(f())
}
`)
}

// Nor is a write to a lambda's own parameter.
func TestCapturedAssignment_OwnParameterIsFine(t *testing.T) {
	assertNoCapturedAssignment(t, `
let main = () -> u8 => {
  let f = (n: i64) -> i64 => { n = n + 1  n }
  u8(f(1))
}
`)
}

// Reading a captured binding is the whole point of capturing one, so only writes
// are reported.
func TestCapturedAssignment_ReadingIsFine(t *testing.T) {
	assertNoCapturedAssignment(t, `
let main = () -> u8 => {
  let n = 5
  let f = (x: i64) -> i64 => x + n
  u8(f(3))
}
`)
}
