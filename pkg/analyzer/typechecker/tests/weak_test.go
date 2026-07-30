package typechecker_test

import "testing"

// `weak T` — a non-owning reference to a `shared T`. Created by `x.weak()` and read
// only through `if let s = w { … }`, which upgrades it to a real `shared T` when the
// referent is still alive. The type existed before this; what was missing was any
// way to make one or read one.

func TestWeak_DowngradeFromShared(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
struct Node { value: i64 }
let main = () -> u8 => {
  let n: shared Node = Node { value: 7 }
  let w = n.weak()
  var out = 0
  if let s = w { out = s.value }
  u8(out)
}
`, false))
}

// The upgrade binds a `shared T`, not the `weak T` being tested — that is the whole
// point of the form, and it is why the branch can read the value at all.
func TestWeak_UpgradeBindsShared(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
struct Node { value: i64 }
let takesShared = (n: shared Node) -> i64 => n.value
let main = () -> u8 => {
  let n: shared Node = Node { value: 7 }
  let w = n.weak()
  var out = 0
  if let s = w { out = takesShared(s) }
  u8(out)
}
`, false))
}

// A weak reference has no fields: reading one without upgrading is exactly the
// dangling read the design exists to prevent, so it cannot be written.
func TestWeak_NoDirectFieldAccess(t *testing.T) {
	res := parseCollectAndCheck(t, `
struct Node { value: i64 }
let main = () -> u8 => {
  let n: shared Node = Node { value: 7 }
  let w = n.weak()
  u8(w.value)
}
`, false)
	assertErrorsAre(t, res, "member access on non-struct type weak Node")
}

// `weak()` needs a `shared` receiver: a weak reference is a reference to a
// ref-counted box, and a stack value has no box to point at.
func TestWeak_DowngradeNeedsShared(t *testing.T) {
	res := parseCollectAndCheck(t, `
struct Node { value: i64 }
let main = () -> u8 => {
  let n = Node { value: 7 }
  let w = n.weak()
  u8(n.value)
}
`, false)
	assertErrorsAre(t, res, `Node has no field or method "weak"`)
}

// Upgrading binds a plain name. A destructuring pattern would conflate "the
// referent is gone" with "it didn't match" — two failures a reader has to be able
// to tell apart, so the nested form is required instead.
func TestWeak_UpgradePatternMustBeAName(t *testing.T) {
	res := parseCollectAndCheck(t, `
struct Node { value: i64 }
let main = () -> u8 => {
  let n: shared Node = Node { value: 7 }
  let w = n.weak()
  var out = 0
  if let { value } = w { out = value }
  u8(out)
}
`, false)
	assertErrorsAre(t, res,
		"upgrading a `weak` reference binds a plain name (`if let s = w`), not a pattern",
		// The names the pattern would have bound stay unbound, as with any rejected
		// destructuring — the body's use of them is reported too.
		`undefined identifier "value"`)
}
