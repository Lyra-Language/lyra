package typechecker_test

import "testing"

// Bounded polymorphism in a method body: with `where t: Show`, a call on a
// value of type `t` (`self.value.show()`) dispatches through the bound's trait
// signature — even though there is no concrete impl for `t` at this point.
func TestBoundDispatch_MethodOnGenericField(t *testing.T) {
	res := parseCollectAndCheck(t, `
struct Box<t> { value: t }
trait Show { show: (Self) -> string }
impl Show for i64 { show = (n) => "i" }
impl Show<t> for Box<t> where t: Show {
    show = (self) => self.value.show()
}`, false)
	assertNoErrors(t, res)
}

// Without a bound, calling a trait method on a bare type parameter is an error
// with actionable guidance — the value's type `t` provides no such method.
func TestBoundDispatch_NoBound_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
struct Box<t> { value: t }
trait Show { show: (Self) -> string }
impl Show<t> for Box<t> {
    show = (self) => self.value.show()
}`, false)
	assertErrorsAre(t, res,
		"type parameter t has no method \"show\"; add a `where t: Trait` bound whose trait declares it")
}

// The bound must actually declare the called method: `t: Show` does not provide
// `compare`, so the call is rejected.
func TestBoundDispatch_WrongMethod_Error(t *testing.T) {
	res := parseCollectAndCheck(t, `
struct Box<t> { value: t }
trait Show { show: (Self) -> string }
trait Ord { compare: (Self, Self) -> i64 }
impl Show<t> for Box<t> where t: Show {
    show = (self) => self.value.compare(self.value)
}`, false)
	assertErrorsAre(t, res,
		"type parameter t has no method \"compare\"; add a `where t: Trait` bound whose trait declares it")
}

// The bound-dispatched call's return type flows through: `show` returns string,
// observed by binding it to a typed local. (Impl method bodies aren't yet
// return-type-checked against the trait signature, so a typed `let` is the
// cleanest place to observe the inferred type.)
func TestBoundDispatch_ReturnTypeFlowsThrough(t *testing.T) {
	res := parseCollectAndCheck(t, `
struct Box<t> { value: t }
trait Show { show: (Self) -> string }
impl Show<t> for Box<t> where t: Show {
    show = (self) => {
        let n: i64 = self.value.show()
        "x"
    }
}`, false)
	assertErrorsAre(t, res, "n: cannot assign string to i64")
}

// A bound whose method returns Self resolves to the type parameter: dup's Self
// return substitutes to `t`, so binding the result to an i64 is a mismatch
// reporting `t` — proving Self resolved to the parameter, not something bogus.
func TestBoundDispatch_MethodReturningSelf(t *testing.T) {
	res := parseCollectAndCheck(t, `
struct Box<t> { value: t }
trait Dup { dup: (Self) -> Self }
impl Dup<t> for Box<t> where t: Dup {
    dup = (self) => {
        let n: i64 = self.value.dup()
        self
    }
}`, false)
	assertErrorsAre(t, res, "n: cannot assign t to i64")
}
