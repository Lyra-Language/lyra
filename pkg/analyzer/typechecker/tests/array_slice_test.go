package typechecker_test

import "testing"

// `xs.slice(start, end)` is the array twin of the string method: the half-open element
// range, copied into a fresh `[]T`.

// The result is a **dynamic** array whatever the receiver was, since `end - start` is a
// run-time value and no fixed size could be written down.
func TestArraySlice_YieldsADynamicArray(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `
let main = () -> void => {
  var xs: []i64 = [1, 2, 3]
  var a: []i64 = xs.slice(0, 2)
  let fixed = [1, 2, 3]
  var b: []i64 = fixed.slice(0, 2)
  println(a.len() + b.len())
}
`, false))
}
