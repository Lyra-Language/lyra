package typechecker_test

import "testing"

// A trait's own type parameter is bound through the impl to the receiver: for
// `trait Get<e> { get: (Self) -> e }` and `impl Get<t> for Box<t>`, calling
// `get()` on `Box<i64>` returns `i64` (e → t → i64). Observed via a typed local.
func TestTraitParam_ReturnsElement_Substituted(t *testing.T) {
	res := parseCollectAndCheck(t, `
struct Box<t> { value: t }
trait Get<e> { get: (Self) -> e }
impl Get<t> for Box<t> {
    get = (self) => self.value
}
let f = (b: Box<i64>) -> i64 => {
    let s: string = b.get()
    0
}`, false)
	assertErrorsAre(t, res, "s: cannot assign i64 to string")
}

// ...and the substituted return type is usable where that concrete type is
// expected: `get()` on `Box<i64>` returns i64, so returning it from an
// i64-returning function is clean.
func TestTraitParam_ReturnsElement_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
struct Box<t> { value: t }
trait Get<e> { get: (Self) -> e }
impl Get<t> for Box<t> {
    get = (self) => self.value
}
let f = (b: Box<i64>) -> i64 => {
    b.get()
}`, false)
	assertNoErrors(t, res)
}

// A trait parameter in argument position is bound too: `set: (Self, e) -> string`
// with e → i64 means `set` takes an i64, so an i64 argument is accepted.
func TestTraitParam_ArgumentPosition_Ok(t *testing.T) {
	res := parseCollectAndCheck(t, `
struct Box<t> { value: t }
trait Setter<e> { set: (Self, e) -> string }
impl Setter<t> for Box<t> {
    set = (self, x) => "ok"
}
let f = (b: Box<i64>) -> string => {
    b.set(5)
}`, false)
	assertNoErrors(t, res)
}

// ...and a mismatched argument is rejected against the bound parameter type: e is
// i64 here, so a string argument is an error.
func TestTraitParam_ArgumentPosition_Mismatch(t *testing.T) {
	res := parseCollectAndCheck(t, `
struct Box<t> { value: t }
trait Setter<e> { set: (Self, e) -> string }
impl Setter<t> for Box<t> {
    set = (self, x) => "ok"
}
let f = (b: Box<i64>) -> string => {
    b.set("wrong")
}`, false)
	assertErrorsAre(t, res, "set: argument 1: cannot assign string to i64")
}

// A concrete trait argument works the same way: `impl Get<i64> for Widget` binds
// e directly to i64, so `get()` on a Widget returns i64.
func TestTraitParam_ConcreteTraitArg(t *testing.T) {
	res := parseCollectAndCheck(t, `
struct Widget { id: i64 }
trait Get<e> { get: (Self) -> e }
impl Get<i64> for Widget {
    get = (self) => 0
}
let f = (w: Widget) -> i64 => {
    let s: string = w.get()
    0
}`, false)
	assertErrorsAre(t, res, "s: cannot assign i64 to string")
}
