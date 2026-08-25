package llvm

import (
	"strings"
	"testing"
)

// A generic newtype over a **managed** base — the case the tests beside this one could not
// reach, because a scalar base needs no drop glue.
//
// `resolveInstantiation` is the choke point that normalizes a `ParameterizedType` into the
// concrete shape it denotes, so that construction, field access, match, layout and the
// retain/drop glue can all keep switching on NamedStructType / TupleType / DataType. It had
// arms for those three and none for `*ConstrainedType` — which is what a parameterized
// newtype expands to — so `newtype Sorted<t> = []t` checked clean and failed the build with
// *"\"Sorted\" is not a generic type that can be instantiated"*. Rule 5 inverted: the front
// end accepting a form the backend cannot build.
//
// **`Boxed<i64>` hid it**, which is why these are all managed: a scalar base generates no
// drop glue, so nothing ever asks for the instantiation. The moment the base owns something,
// the glue asks.
//
// A newtype has no layout of its own — it *is* its base at run time — so the fix resolves to
// the base before the placeholder struct is declared. Declaring it first and deleting the map
// entry afterwards is not equivalent: the name is registered with the module, and the orphan
// `%Sorted$i64 = type {}` reaches the IR, where clang rejects it as a redefinition.
func TestExec_GenericNewtypeOverAManagedBase(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ name, src, want string }{
		{"dynamic array base", `newtype Sorted<t> = []t
let main = () -> void => {
  let s: Sorted<i64> = [1, 2, 3]
  println(i64(s.len()))
}`, "3"},
		{"array of strings", `newtype Names<t> = []t
let main = () -> void => {
  var n: Names<string> = ["a", "b"]
  println(n[0] ++ n[1])
}`, "ab"},
		// A newtype whose base is another newtype: the arm recurses through StripNewtype
		// rather than reading `.Type` once.
		{"newtype over newtype", `newtype Inner<t> = []t
newtype Outer<t> = Inner<t>
let main = () -> void => {
  let o: Outer<i64> = [4, 5]
  println(i64(o.len()))
}`, "2"},
		// Two managed instantiations of one generic newtype must not collide on the
		// mangled name.
		{"two managed instantiations", `newtype Bag<t> = []t
let main = () -> void => {
  var a: Bag<i64> = [1, 2]
  var b: Bag<string> = ["p", "q"]
  println(i64(a.len()) + i64(b.len()))
}`, "4"},
		// Nested in a struct, so layout and the drop glue both have to reach it.
		{"nested in a struct", `newtype Bag<t> = []t
struct Holder { b: Bag<i64>, k: i64 }
let main = () -> void => {
  var h = Holder { b: [1, 2, 3], k: 4 }
  println(i64(h.b.len()) + h.k)
}`, "7"},
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
