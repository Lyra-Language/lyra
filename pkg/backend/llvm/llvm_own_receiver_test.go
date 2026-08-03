package llvm

import (
	"os/exec"
	"testing"
)

// `own` on a trait method's parameter, including the receiver. Rejected until 08/03
// (lyra-E030) because the ownership pass analyzed no method body: a transferred value was
// dropped by the callee and still used by the caller. The rejection cited a measured
// heap-use-after-free from `take: (Self, own string) -> string`, which is the first case
// below — run here, under the harness that actually instruments the module.
//
// These run rather than inspect. A missing release leaks and a doubled one aborts, and
// neither shows up in the IR at a glance; ASan is what tells them apart.

const ownTransfer = `struct Holder { tag: string }
trait Consume {
  take: (Self, own string) -> string,
  swallow: (Self, own string) -> i64
}
impl Consume for Holder {
  take = (self, s) => s,
  swallow = (self, s) => 7
}
`

var ownCases = []struct {
	name string
	src  string
	want int
}{
	{
		// The ASan report's own program: the callee returns the value it was given,
		// so ownership passes through rather than being dropped.
		"own parameter returned (transferred onward)",
		ownTransfer + `let main = () -> u8 => {
  let h = Holder { tag: "h" }
  let msg = "hello"
  println(h.take(msg))
  u8(35)
}
`,
		35,
	},
	{
		// …and the other half: the callee drops it instead. Exactly one release must
		// happen, and it must happen in the callee.
		"own parameter dropped by the callee",
		ownTransfer + `let main = () -> u8 => {
  let h = Holder { tag: "h" }
  let msg = "hello"
  u8(h.swallow(msg))
}
`,
		7,
	},
	{
		// An `own Self` receiver on a managed value: the method consumes the receiver
		// and returns a field out of it, so the field must outlive the box it came from.
		"own receiver, managed",
		`struct Node { tag: string }
trait Into { into_tag: (own Self) -> string }
impl Into for Node { into_tag = (self) => self.tag }
let main = () -> u8 => {
  let n: shared Node = Node { tag: "hello" }
  println(n.into_tag())
  u8(21)
}
`,
		21,
	},
	{
		// A generic impl with an `own` parameter — the two features together, since the
		// body is now both monomorphized and analyzed per specialization.
		"own parameter in a generic impl",
		`data Opt<t> = Nil | Just t
trait Or<e> { or_else: (Self, own e) -> e }
impl Or<t> for Opt<t> {
  or_else = (self, fallback) => match self {
    Just v => v,
    Nil => fallback,
  }
}
let main = () -> u8 => {
  let s: Opt<string> = Nil
  println(s.or_else("fallback"))
  let n: Opt<i64> = Just 14
  u8(n.or_else(0))
}
`,
		14,
	},
}

func TestExec_OwnTraitParameter(t *testing.T) {
	t.Parallel()
	for _, c := range ownCases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := buildAndRun(t, c.src); got != c.want {
				t.Errorf("%s: exited %d; want %d", c.name, got, c.want)
			}
		})
	}
}

// The same programs under AddressSanitizer, which is the point: the restriction existed
// because this combination aborted here.
func TestExec_OwnTraitParameterASan(t *testing.T) {
	t.Parallel()
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Skip("clang not found on PATH")
	}
	if !asanAvailable(t, clang) {
		t.Skip("AddressSanitizer not available in this toolchain")
	}
	for _, c := range ownCases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := buildAndRunASan(t, clang, c.src); got != c.want {
				t.Errorf("%s (asan): exited %d; want %d", c.name, got, c.want)
			}
		})
	}
}
