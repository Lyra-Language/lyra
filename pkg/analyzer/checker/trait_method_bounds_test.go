package checker_test

import "testing"

// A `pure`-declared trait method is a contract: an impl whose body is impure is
// flagged, even though the impl method itself carries no `pure` annotation.
func TestTraitBound_Pure_ImpureImpl_Flagged(t *testing.T) {
	src := `
trait Show { pure show: (Self) -> string }
impl Show for i64 {
    show = (n) => {
        println(n)
        "i"
    }
}`
	assertBoundError(t, checkPurity(t, src), "lyra-E007")
}

// A pure impl satisfies the pure contract.
func TestTraitBound_Pure_PureImpl_Ok(t *testing.T) {
	src := `
trait Show { pure show: (Self) -> string }
impl Show for i64 { show = (n) => "i" }`
	assertPurityCount(t, checkPurity(t, src), 0)
}

// A `det`-declared trait method: an impl that reads external input violates the
// determinism contract.
func TestTraitBound_Det_ImpureImpl_Flagged(t *testing.T) {
	src := `
trait Clock { det now: (Self) -> i64 }
impl Clock for i64 {
    now = (self) => {
        let got = read_line() ?? "t"
        got.len()
    }
}`
	assertBoundError(t, checkPurity(t, src), "lyra-E016")
}

// A `noalloc`-declared trait method: an impl that constructs a `shared` value
// violates the no-allocation contract.
func TestTraitBound_NoAlloc_AllocImpl_Flagged(t *testing.T) {
	src := `
struct Node { v: i64 }
trait Make { noalloc make: (Self) -> i64 }
impl Make for i64 {
    make = (self) => {
        let n: shared Node = Node { v: 1 }
        n.v
    }
}`
	assertBoundError(t, checkPurity(t, src), "lyra-E016")
}

// End-to-end: `pure show` on the trait lets a generic impl's `pure` method
// dispatch through the `t: Show` bound and stay pure — every impl is guaranteed
// pure by the contract (and checked to be).
func TestTraitBound_Pure_BoundDispatch_Ok(t *testing.T) {
	src := `
struct Box<t> { value: t }
trait Show { pure show: (Self) -> string }
impl Show for i64 { show = (n) => "i" }
impl Show<t> for Box<t> where t: Show {
    show = pure (self) => self.value.show()
}`
	assertPurityCount(t, checkPurity(t, src), 0)
}
