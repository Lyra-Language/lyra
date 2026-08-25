package llvm

import (
	"strings"
	"testing"
)

// A newtype over an **array** keeps its wrapper across a call boundary.
//
// `let b: Bag = ["x"]` over `newtype Bag = []string` recorded the binding as
// `DynamicArray<string>`, so returning it was `lyra-E046` — *"cannot use
// DynamicArray<string> as Bag implicitly"* — and passing it to a `(b: Bag)` parameter said
// the same. The value was correct; only its recorded type had lost the name.
//
// The cause was literal propagation: it narrows an array literal's leaves against the
// context type's *base*, then re-records the root with the base's shape. A **scalar** base
// never showed it, because its re-record is guarded by `currentTypeIsUntyped` — false once
// the annotation is recorded — while the array arms re-record unconditionally, deliberately,
// since a return body or an argument has nothing else recording that node.
//
// Keeping the wrapper moved work onto the rungs that read a value's type, which had been
// relying on the overwrite to strip for them: indexing and the method fallback both now
// resolve-and-strip for themselves. That is the same `stripNewtypeResolving` the read-out
// conversion uses, and the generic case is why it resolves *between* strips —
// `newtype Outer<t> = Inner<t>` has a ParameterizedType in the middle, and a plain
// StripNewtype stops one layer short of the array.
func TestExec_NewtypeOverAnArrayCrossesCallBoundaries(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ name, src, want string }{
		{"returned, then indexed", `newtype Bag = []string
let make = () -> Bag => {
  var b: Bag = ["x", "y"]
  b
}
let main = () -> void => println(make()[1])`, "y"},
		{"passed as a parameter", `newtype Nums = []i64
let total = (n: Nums) -> i64 => n[0] + n[1]
let main = () -> void => {
  let n: Nums = [20, 22]
  println(total(n))
}`, "42"},
		{"fixed-array base", `newtype Trio = [3]i64
let make = () -> Trio => {
  let t: Trio = [1, 2, 3]
  t
}
let main = () -> void => println(make()[2])`, "3"},
		// Passed before the fix too, and kept: the element width still has to be pushed
		// down *through* the wrapper, and the propagation that does it is what this change
		// altered. A u8 base must lower u8 leaves, not i64 ones.
		{"narrow element width", `newtype Bytes = []u8
let main = () -> void => {
  var b: Bytes = [1, 2, 3]
  println(i64(b[0]) + i64(b.len()))
}`, "4"},
		// Passed before the fix — but only *because* the wrapper was being clobbered, which
		// made `o` read as the array outright. It is here because keeping the wrapper broke
		// it, and the method fallback now has to resolve between strips to see the array
		// underneath a ParameterizedType. A regression test for the fix's own fallout.
		{"generic newtype chain", `newtype Inner<t> = []t
newtype Outer<t> = Inner<t>
let main = () -> void => {
  let o: Outer<i64> = [4, 5]
  println(i64(o.len()))
}`, "2"},
		// The scalar base that always worked, kept so the fix cannot regress it.
		{"scalar base (control)", `newtype Cents = i64
let make = () -> Cents => {
  let c: Cents = 5
  c
}
let main = () -> void => println(i64(make()))`, "5"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			out, code := buildAndRunCapture(t, c.src)
			if code != 0 || strings.TrimSpace(out) != c.want {
				t.Errorf("exit %d, output %q; want %q", code, strings.TrimSpace(out), c.want)
			}
		})
	}
}
