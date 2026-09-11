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

// ---------------------------------------------------------------------------
// `&mut` on a capture — the same rule one spelling further out
// ---------------------------------------------------------------------------

// A mutable pointer to a capture addresses the **closure's copy**, so whatever is written
// through it — by this program or by a C function handed the pointer — changes the copy
// and nothing else. Until 09/10 this compiled, ran, and lost the write with nothing said.
//
// The four-line program below is the reduction of a real bug: an out-parameter taken
// inside a `with_cstring` lambda had raylib write a buffer length into the copy, so every
// binary file read back empty (`bindings/raylib/files.lyra`).
func assertCapturedAddress(t *testing.T, src, wantName string) {
	t.Helper()
	got := capturedAssignmentErrors(t, src)
	if len(got) != 1 {
		t.Fatalf("expected exactly 1 captured-address error, got %d: %v", len(got), got)
	}
	if !strings.Contains(got[0].Message, "cannot take `&mut` of \""+wantName+"\"") {
		t.Errorf("message should name the binding %q: %q", wantName, got[0].Message)
	}
}

func TestCapturedAddress_MutablePointerToACapture(t *testing.T) {
	assertCapturedAddress(t, `
let fill = (p: ^mut i32) -> void => unsafe { p^ = 42 }
let apply = (f: () -> void) -> void => f()
let main = () -> u8 => {
  var captured: i32 = 0
  apply(() => unsafe { fill(&mut captured) })
  0
}
`, "captured")
}

// Rooted at a capture through a path, which is the same storage `p.x = v` would name —
// and which the assignment half already refuses through the same walk.
func TestCapturedAddress_ThroughAPath(t *testing.T) {
	assertCapturedAddress(t, `
struct Box { n: i32 }
let fill = (p: ^mut i32) -> void => unsafe { p^ = 42 }
let apply = (f: () -> void) -> void => f()
let main = () -> u8 => {
  var b = Box { n: 0 }
  apply(() => unsafe { fill(&mut b.n) })
  0
}
`, "b")
}

// **An immutable `&` is untouched**, and that is the line: it cannot write, so reading
// through it sees exactly what the closure sees. Refusing it would make the safe spelling
// the refused one.
func TestCapturedAddress_ImmutableBorrowIsFine(t *testing.T) {
	src := `
let peek = (p: ^i32) -> i32 => unsafe { p^ }
let apply = (f: () -> void) -> void => f()
let main = () -> u8 => {
  var readable: i32 = 7
  apply(() => println("${unsafe { peek(&readable) }}"))
  0
}
`
	if got := capturedAssignmentErrors(t, src); len(got) != 0 {
		t.Errorf("got %v; want none — `&` cannot write, so there is nothing to lose", got)
	}
}

// A lambda's own local is not a capture, so a pointer to it addresses the only copy there
// is. This is the shape the fix must not break: taking an out-parameter inside a closure
// is fine as long as the storage is the closure's.
func TestCapturedAddress_OwnLocalIsFine(t *testing.T) {
	src := `
let fill = (p: ^mut i32) -> void => unsafe { p^ = 42 }
let apply = (f: () -> void) -> void => f()
let main = () -> u8 => {
  apply(() => { var own: i32 = 0  unsafe { fill(&mut own) }  println("${own}") })
  0
}
`
	if got := capturedAssignmentErrors(t, src); len(got) != 0 {
		t.Errorf("got %v; want none — `own` is the lambda's own local", got)
	}
}

// And outside any lambda there is no capture at all.
func TestCapturedAddress_OutsideALambdaIsFine(t *testing.T) {
	src := `
let fill = (p: ^mut i32) -> void => unsafe { p^ = 42 }
let main = () -> u8 => {
  var plain: i32 = 0
  unsafe { fill(&mut plain) }
  println("${plain}")
  0
}
`
	if got := capturedAssignmentErrors(t, src); len(got) != 0 {
		t.Errorf("got %v; want none — nothing is captured here", got)
	}
}
