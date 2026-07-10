package typechecker_test

import "testing"

// An impl method body whose value doesn't match the trait's declared return type
// is now an error (previously the body was inferred but never checked).
func TestImplReturn_Mismatch(t *testing.T) {
	res := parseCollectAndCheck(t, `
trait Show { show: (Self) -> string }
impl Show for i64 {
    show = (n) => n
}`, false)
	assertErrorsAre(t, res, "show: return type mismatch: expected string, got i64")
}

// A conforming body produces no error.
func TestImplReturn_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
trait Show { show: (Self) -> string }
impl Show for i64 {
    show = (n) => "x"
}`, false)
	assertNoErrors(t, res)
}

// A block-bodied impl method is checked against its implicit return (the last
// expression): here the block yields i64 but string is required.
func TestImplReturn_BlockBody_Mismatch(t *testing.T) {
	res := parseCollectAndCheck(t, `
trait Show { show: (Self) -> string }
impl Show for i64 {
    show = (n) => {
        let x = 5
        x
    }
}`, false)
	assertErrorsAre(t, res, "show: return type mismatch: expected string, got i64")
}

// The return type is understood in the impl's own variables: for `Get<e>` with
// `impl Get<t> for Box<t>`, the return `e` is bound to `t`, so a body returning
// `self.value` (type `t`) conforms — no false mismatch.
func TestImplReturn_GenericElement_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
struct Box<t> { value: t }
trait Get<e> { get: (Self) -> e }
impl Get<t> for Box<t> {
    get = (self) => self.value
}`, false)
	assertNoErrors(t, res)
}

// ...but a body returning the element type where the trait declares a concrete
// return is still caught: Show.show must return string, not the element `t`.
func TestImplReturn_GenericElement_Mismatch(t *testing.T) {
	res := parseCollectAndCheck(t, `
struct Box<t> { value: t }
trait Show { show: (Self) -> string }
impl Show<t> for Box<t> {
    show = (self) => self.value
}`, false)
	assertErrorsAre(t, res, "show: return type mismatch: expected string, got t")
}

// A Self-returning method conforms when the body returns the receiver.
func TestImplReturn_SelfReturn_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
struct Box<t> { value: t }
trait Dup { dup: (Self) -> Self }
impl Dup<t> for Box<t> {
    dup = (self) => self
}`, false)
	assertNoErrors(t, res)
}
