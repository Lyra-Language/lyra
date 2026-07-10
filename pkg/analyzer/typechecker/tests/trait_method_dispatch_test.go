package typechecker_test

import "testing"

// TestTraitDispatch_DotCall: `n.show()` resolves to Show's impl for i64 and
// type-checks its return type.
func TestTraitDispatch_DotCall(t *testing.T) {
	source := `
trait Show {
    show: (Self) -> string
}
impl Show for i64 {
    show = (n) => "x"
}
let f = (n: i64) -> string => {
    n.show()
}`
	res := parseCollectAndCheck(t, source, false)
	assertNoErrors(t, res)
}

// TestTraitDispatch_QualifiedCall: `Show::show(n)` resolves the same impl as
// the dot-call form, with the receiver as an ordinary first argument.
func TestTraitDispatch_QualifiedCall(t *testing.T) {
	source := `
trait Show {
    show: (Self) -> string
}
impl Show for i64 {
    show = (n) => "x"
}
let f = (n: i64) -> string => {
    Show::show(n)
}`
	res := parseCollectAndCheck(t, source, false)
	assertNoErrors(t, res)
}

// TestTraitDispatch_ReturnTypeFlowsThrough: a dispatched method's declared
// return type is used for further type-checking (here, a mismatch against
// the enclosing function's declared return type).
func TestTraitDispatch_ReturnTypeFlowsThrough(t *testing.T) {
	source := `
trait Show {
    show: (Self) -> string
}
impl Show for i64 {
    show = (n) => "x"
}
let f = (n: i64) -> i64 => {
    n.show()
}`
	res := parseCollectAndCheck(t, source, false)
	assertErrorsAre(t, res, "f: return type mismatch: expected i64, got string")
}

// TestTraitDispatch_WrongArgCount_DotCall: the receiver is implicit in a
// `.`-call, so arity is checked against the signature minus Self.
func TestTraitDispatch_WrongArgCount_DotCall(t *testing.T) {
	source := `
trait Show {
    show: (Self) -> string
}
impl Show for i64 {
    show = (n) => "x"
}
let f = (n: i64) -> string => {
    n.show(42)
}`
	res := parseCollectAndCheck(t, source, false)
	assertErrorsAre(t, res, "show: expected 0 argument(s), got 1")
}

// TestTraitDispatch_NoMatchingMethod_Error: no impl provides this method for
// this type.
func TestTraitDispatch_NoMatchingMethod_Error(t *testing.T) {
	source := `
trait Show {
    show: (Self) -> string
}
let f = (n: i64) -> string => {
    n.show()
}`
	res := parseCollectAndCheck(t, source, false)
	assertErrorsAre(t, res, `i64 has no method "show"`)
}

// TestTraitDispatch_UnknownTraitInQualifiedCall_Error: TraitName::method
// names a trait that doesn't exist.
func TestTraitDispatch_UnknownTraitInQualifiedCall_Error(t *testing.T) {
	source := `
let f = (n: i64) -> string => {
    Ghost::show(n)
}`
	res := parseCollectAndCheck(t, source, false)
	assertErrorsAre(t, res, `unknown trait "Ghost"`)
}

// TestTraitDispatch_AmbiguousDotCall_Error: two different traits implement a
// same-named method for the same type, so a plain `.`-call can't pick one.
func TestTraitDispatch_AmbiguousDotCall_Error(t *testing.T) {
	source := `
trait Pilot {
    fly: (Self) -> string
}
trait Wizard {
    fly: (Self) -> string
}
impl Pilot for i64 {
    fly = (n) => "piloting"
}
impl Wizard for i64 {
    fly = (n) => "casting"
}
let f = (n: i64) -> string => {
    n.fly()
}`
	res := parseCollectAndCheck(t, source, false)
	assertErrorsAre(t, res, `call to "fly" is ambiguous between traits Pilot, Wizard; use TraitName::fly(...) to disambiguate`)
}

// TestTraitDispatch_AmbiguityResolvedByQualifiedCall: the same ambiguous
// scenario as above, but the qualified form picks one trait unambiguously.
func TestTraitDispatch_AmbiguityResolvedByQualifiedCall(t *testing.T) {
	source := `
trait Pilot {
    fly: (Self) -> string
}
trait Wizard {
    fly: (Self) -> string
}
impl Pilot for i64 {
    fly = (n) => "piloting"
}
impl Wizard for i64 {
    fly = (n) => "casting"
}
let f = (n: i64) -> string => {
    Wizard::fly(n)
}`
	res := parseCollectAndCheck(t, source, false)
	assertNoErrors(t, res)
}

// TestTraitDispatch_StructFieldShadowsMethod: a struct field of the same
// name as a trait method takes priority, matching ordinary method-shadowing
// semantics.
func TestTraitDispatch_StructFieldShadowsMethod(t *testing.T) {
	source := `
trait Show {
    show: (Self) -> string
}
struct Box {
    show: string
}
impl Show for Box {
    show = (b) => "from trait"
}
let f = (b: Box) -> string => {
    b.show()
}`
	res := parseCollectAndCheck(t, source, false)
	assertErrorsAre(t, res, `member "show" is not callable (type string)`)
}

// TestTraitDispatch_ArgumentTypeChecked_DotCall: a non-receiver argument is
// checked against the method's declared parameter type.
func TestTraitDispatch_ArgumentTypeChecked_DotCall(t *testing.T) {
	source := `
trait Adder {
    add: (Self, i64) -> i64
}
impl Adder for i64 {
    add = (n, m) => n + m
}
let f = (n: i64) -> i64 => {
    n.add("not a number")
}`
	res := parseCollectAndCheck(t, source, false)
	assertErrorsAre(t, res, "add: argument 1: cannot assign string to i64")
}
