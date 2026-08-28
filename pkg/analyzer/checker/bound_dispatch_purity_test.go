package checker_test

import "testing"

// A `pure` method whose body calls a trait method through a `where` bound is
// clean when every impl of that bound method is pure — the bound-dispatched call
// is no longer treated as an unverifiable (impure) external call.
func TestBoundPurity_AllPureImpls_Ok(t *testing.T) {
	src := `
struct Box<t> { value: t }
trait Show { show: (Self) -> string }
impl Show for i64 { show = (n) => "i" }
impl Show<t> for Box<t> where t: Show {
    show = pure (self) => self.value.show()
}`
	assertPurityCount(t, checkPurity(t, src), 0)
}

// ...but flagged when *any* impl of the bound method is impure: the bound admits
// that type, so a pure method dispatching through it can't be pure.
func TestBoundPurity_ImpureImpl_Flagged(t *testing.T) {
	src := `
struct Box<t> { value: t }
trait Show { show: (Self) -> string }
impl Show for i64 {
    show = (n) => {
        println(n)
        "i"
    }
}
impl Show<t> for Box<t> where t: Show {
    show = pure (self) => self.value.show()
}`
	assertBoundError(t, checkPurity(t, src), "lyra-E007")
}

// The effect flows through the inference fixpoint, so bounds beyond `pure` are
// enforced too: a `det` method calling a bound method one of whose impls reads
// external input is a determinism violation.
func TestBoundPurity_DetViolation(t *testing.T) {
	src := `
struct Box<t> { value: t }
trait Show { show: (Self) -> string }
impl Show for i64 {
    show = (n) => {
        let got = read_line() ?? "x"
        got
    }
}
impl Show<t> for Box<t> where t: Show {
    show = det (self) => self.value.show()
}`
	assertBoundError(t, checkPurity(t, src), "lyra-E016")
}

// A bound whose impls are all pure keeps a `det` method clean.
func TestBoundPurity_DetOk(t *testing.T) {
	src := `
struct Box<t> { value: t }
trait Show { show: (Self) -> string }
impl Show for i64 { show = (n) => "i" }
impl Show<t> for Box<t> where t: Show {
    show = det (self) => self.value.show()
}`
	assertPurityCount(t, checkPurity(t, src), 0)
}
