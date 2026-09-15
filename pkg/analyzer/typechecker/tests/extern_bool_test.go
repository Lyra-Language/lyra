package typechecker_test

import "testing"

// `bool` crosses an extern boundary as C's `_Bool` (09/16); in a callback's signature it
// stays refused, since the thunk C calls carries no ABI attributes.
func TestExternBool(t *testing.T) {
	t.Parallel()
	assertNoErrors(t, parseCollectAndCheck(t, `
unsafe extern is_ready: (fd: i32, strict: bool) -> bool
let go = (fd: i32) -> bool => unsafe { is_ready(fd, true) }`, false))
	assertHasErrorContaining(t, parseCollectAndCheck(t, `
unsafe extern on_each: (cb: (i32) -> bool) -> void
let main = () -> void => {}`, false), "callback whose return type is boolean")
}
