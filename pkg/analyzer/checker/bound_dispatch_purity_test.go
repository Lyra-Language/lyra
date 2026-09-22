package checker_test

import (
	"strings"
	"testing"
)

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

// **What the trait declares is what a bound call gets.** The join above is the right
// answer only when the trait says nothing: the impls are then all the evidence there is.
// A trait that declares `pure show` has made a contract every impl is separately held
// to, so a `pure` function dispatching through the bound may rely on it — and an impl
// that breaks it is reported at the impl, which is where the mistake is.
//
// Before 09/22 the join ran regardless, which reported one mistake twice and put the
// second report in the wrong file: an impure impl in *your* module made a library's
// `pure` generic fail to compile, with the diagnostic on the library's line, even when
// the generic was never instantiated at your type. That is a composability failure, not
// a purity violation — the library was correct and had no way to stay correct.
func TestBoundPurity_ADeclaredBoundIsBelieved(t *testing.T) {
	src := `
struct Box<t> { value: t }
trait Show { pure show: (Self) -> string }
impl Show for i64 {
    show = (n) => {
        println(n)
        "i"
    }
}
impl Show<t> for Box<t> where t: Show {
    show = pure (self) => self.value.show()
}`
	// Exactly one: the impl that broke the contract, not the generic that trusted it.
	diags := checkPurity(t, src)
	assertPurityCount(t, diags, 1)
	if !strings.Contains(diags[0].Message, `impure function "println"`) {
		t.Errorf("the one error should be the impl's broken promise; got %q", diags[0].Message)
	}
}

// The mirror, so the test above cannot pass by making bound calls unchecked: with no
// bound on the trait there is no contract to believe, and the join still runs. This is
// the same program as TestBoundPurity_ImpureImpl_Flagged with the promise removed, which
// is the entire difference between the two.
func TestBoundPurity_WithoutADeclaredBoundTheJoinStillRuns(t *testing.T) {
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

// A `det` bound is believed on the same terms, since the subtraction is per effect bit
// rather than a special case for `pure`: the trait promises determinism, the impl that
// reads input is refused, and the `det` method dispatching through the bound is not.
func TestBoundPurity_ADeclaredDetBoundIsBelieved(t *testing.T) {
	src := `
struct Box<t> { value: t }
trait Show { det show: (Self) -> string }
impl Show for i64 {
    show = (n) => {
        let got = read_line() ?? "x"
        got
    }
}
impl Show<t> for Box<t> where t: Show {
    show = det (self) => self.value.show()
}`
	assertPurityCount(t, checkPurity(t, src), 1)
}
