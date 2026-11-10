package typechecker_test

import "testing"

// `xs.push_from(p, n)` — n elements read through a `^T`, appended to a `[]T` in one copy
// (11/10). The element type is any with a C layout, the pointer's pointee is the array's
// element, and the call needs `unsafe`, a mutable receiver and an allocating context.

func TestPushFrom_AcceptsCLayoutElements(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
struct Color { r: u8, g: u8, b: u8, a: u8 }
let main = () -> void => {
  var bytes: []u8 = []
  var colors: []Color = []
  var samples: []f32 = []
  var pairs: [](i32, f64) = []
  let p: ^u8 = unsafe { nullptr }
  let c: ^mut Color = unsafe { nullptr }
  let s: ^f32 = unsafe { nullptr }
  let q: ^(i32, f64) = unsafe { nullptr }
  let n: i32 = 0
  unsafe {
    bytes.push_from(p, 0)
    colors.push_from(c, i64(n))
    samples.push_from(s, 0)
    pairs.push_from(q, 0)
  }
}`, false))
}

func TestPushFrom_Refusals(t *testing.T) {
	for _, c := range []struct{ name, src, want string }{
		{"outside unsafe", `let main = () -> void => {
  var xs: []u8 = []
  let p: ^u8 = unsafe { nullptr }
  xs.push_from(p, 0)
}`, "`push_from` reads through a raw pointer, so it requires an `unsafe` block"},
		{"a managed element", `let main = () -> void => {
  var xs: []string = []
  let p: ^string = unsafe { nullptr }
  unsafe { xs.push_from(p, 0) }
}`, "`push_from` copies elements as raw bytes from C memory, so the element type needs a C layout"},
		{"a struct holding a string", `struct Named { id: i32, name: string }
let main = () -> void => {
  var xs: []Named = []
  let p: ^Named = unsafe { nullptr }
  unsafe { xs.push_from(p, 0) }
}`, "needs a C layout"},
		{"a bool element", `let main = () -> void => {
  var xs: []bool = []
  let p: ^bool = unsafe { nullptr }
  unsafe { xs.push_from(p, 0) }
}`, "needs a C layout"},
		{"the wrong pointee", `let main = () -> void => {
  var xs: []u32 = []
  let p: ^u8 = unsafe { nullptr }
  unsafe { xs.push_from(p, 0) }
}`, "^u8"},
		{"an immutable receiver", `let main = () -> void => {
  let xs: []u8 = []
  let p: ^u8 = unsafe { nullptr }
  unsafe { xs.push_from(p, 0) }
}`, "xs"},
		{"an i32 count", `let main = () -> void => {
  var xs: []u8 = []
  let p: ^u8 = unsafe { nullptr }
  let n: i32 = 0
  unsafe { xs.push_from(p, n) }
}`, "cannot assign i32 to i64"},
	} {
		t.Run(c.name, func(t *testing.T) {
			res := parseCollectAndCheck(t, c.src, false)
			if !hasError(res, c.want) {
				t.Errorf("expected an error containing %q; got %v", c.want, res.errors)
			}
		})
	}
}
