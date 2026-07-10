package typechecker_test

import "testing"

// Inside a generic impl body, `self` has the impl's target type (`Box<t>`), so a
// field access `self.value` resolves to the field's (generic) type `t` — the
// prerequisite for using a generic parameter in a method body.
func TestGenericField_ImplBodyReadsSelfField(t *testing.T) {
	res := parseCollectAndCheck(t, `
struct Box<t> { value: t }
trait Show { show: (Self) -> string }
impl Show<t> for Box<t> {
    show = (self) => {
        let v = self.value
        "box"
    }
}`, false)
	assertNoErrors(t, res)
}

// On a concrete instance, the type argument is substituted into the field type:
// `b.value` on `Box<i64>` is `i64`, so returning it from an i64-returning
// function is fine.
func TestGenericField_ConcreteInstance_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
struct Box<t> { value: t }
let f = (b: Box<i64>) -> i64 => {
    b.value
}`, false)
	assertNoErrors(t, res)
}

// The substitution is real, not just head-name resolution: `b.value` on
// `Box<i64>` is `i64` (not the parameter `t`), so a string-returning function is
// a mismatch reported against the concrete type.
func TestGenericField_ConcreteInstance_Substituted(t *testing.T) {
	res := parseCollectAndCheck(t, `
struct Box<t> { value: t }
let f = (b: Box<i64>) -> string => {
    b.value
}`, false)
	assertErrorsAre(t, res, "f: return type mismatch: expected string, got i64")
}

// Multiple parameters substitute positionally: `Pair<i64, string>` gives
// `first: i64` and `second: string`.
func TestGenericField_MultiParam_Substituted(t *testing.T) {
	res := parseCollectAndCheck(t, `
struct Pair<a, b> { first: a, second: b }
let f = (p: Pair<i64, string>) -> i64 => {
    p.second
}`, false)
	assertErrorsAre(t, res, "f: return type mismatch: expected i64, got string")
}

// Boundary (documents current scope): calling a trait method *on* a generic-typed
// field (`self.value.show()`, needing `t: Show` to dispatch through the bound) is
// not yet supported — dispatching on a type variable via its bound is a separate
// feature. When it lands, update this test.
func TestGenericField_MethodOnGenericField_NotYetSupported(t *testing.T) {
	res := parseCollectAndCheck(t, `
struct Box<t> { value: t }
trait Show { show: (Self) -> string }
impl Show for i64 { show = (n) => "i" }
impl Show<t> for Box<t> where t: Show {
    show = (self) => self.value.show()
}`, false)
	assertErrorsAre(t, res, "member access on non-struct type t")
}
