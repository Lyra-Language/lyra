package llvm

import "testing"

// A raw pointer bound by a pattern — `let _ = p` — is matched as the value it is. The
// pattern path unboxed its operand by LLVM shape, taking any pointer for a `shared` box,
// so it refused every such binding ("match scrutinee is a pointer to a non-box type"),
// and a pointer to a struct with a box's field count would have been read as a box. The
// three-field struct here is that second shape.
func TestExec_PatternBindingARawPointer(t *testing.T) {
	t.Parallel()
	src := `struct Three { a: i64, b: i64, c: i64 }
let main = () -> u8 => {
  var x: i32 = 5
  var t = Three { a: 1, b: 2, c: 3 }
  let p = unsafe { &x }
  let q = unsafe { &mut t }
  let _ = p
  let _ = q
  unsafe { q^.b = 40 }
  if t.b == 40 && unsafe { p^ } == 5 { 3 } else { 1 }
}
`
	if code := buildAndRun(t, src); code != 3 {
		t.Errorf("expected exit 3, got %d", code)
	}
}
