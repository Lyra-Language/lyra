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

// **A more specific impl wins** (09/23). `impl Show for Box<i64>` beside
// `impl Show<t> for Box<t>` is not an ambiguity: `Box<t>` matches `Box<i64>` and
// `Box<i64>` matches `Box<t>` not at all, so the concrete one subsumes the general one and
// the call takes it. Ranking runs in resolveTraitMethodNamed, so operator dispatch,
// `.method()` and `Trait::method` all get the same answer.
func TestTraitDispatch_AMoreSpecificImplWins(t *testing.T) {
	source := `
struct Box<t> {
    value: t
}
trait Show {
    show: (Self) -> string
}
impl Show<t> for Box<t> {
    show = (self) => "generic"
}
impl Show for Box<i64> {
    show = (self) => "specific"
}
let f = (b: Box<i64>) -> string => {
    b.show()
}`
	res := parseCollectAndCheck(t, source, false)
	assertNoErrors(t, res)
}

// ...and the general impl still applies where nothing is more specific, which is what
// makes the rule a *ranking* rather than the concrete impl shadowing the generic one.
func TestTraitDispatch_TheGeneralImplStillAppliesElsewhere(t *testing.T) {
	source := `
struct Box<t> {
    value: t
}
trait Show {
    show: (Self) -> string
}
impl Show<t> for Box<t> {
    show = (self) => "generic"
}
impl Show for Box<i64> {
    show = (self) => "specific"
}
let f = (b: Box<string>) -> string => {
    b.show()
}`
	res := parseCollectAndCheck(t, source, false)
	assertNoErrors(t, res)
}

// **Two impls of one trait is a different ambiguity, and the message has to say so.**
// The test above is two *traits*, where `Trait::fly(...)` settles it because the traits
// differ. Same-trait targets printed that same sentence until 09/22: "ambiguous between
// traits Show, Show", naming one trait twice and advising a qualifier that cannot choose
// between two impls of that trait.
//
// Since ranking landed (09/23) this is the case ranking cannot settle: `Pair<i64, b>` and
// `Pair<a, i64>` are **incomparable** — each covers values the other does not, so neither
// subsumes the other and no rule about specificity applies. `Box<t>` beside `Box<i64>` no
// longer reaches here at all, which is the point of the two tests above.
func TestTraitDispatch_IncomparableImplsOfOneTraitNameTheirTargets(t *testing.T) {
	source := `
struct Pair<a, b> {
    x: a,
    y: b
}
trait Show {
    show: (Self) -> string
}
impl Show<b> for Pair<i64, b> {
    show = (self) => "left"
}
impl Show<a> for Pair<a, i64> {
    show = (self) => "right"
}
let f = (p: Pair<i64, i64>) -> string => {
    p.show()
}`
	res := parseCollectAndCheck(t, source, false)
	assertErrorsAre(t, res, `call to "show" matches 2 impls of trait Show, for Pair<i64, b>, Pair<a, i64> `+
		`— neither target is more specific than the other, so nothing at the call site can `+
		`choose between them. Narrow one impl's target, or merge them`)
}

// **Ranking compares impls of one trait, never across traits.** `impl Pilot<t> for Box<t>`
// beside `impl Wizard for Box<i64>` has comparable *targets* — `Box<t>` subsumes
// `Box<i64>` — so a specificity rule that ignored the trait would quietly hand the call to
// Wizard. But the traits differ, which makes this the choice the author has to state
// (`Pilot::fly(b)`), not a question about which target is narrower.
//
// The earlier two-trait test cannot catch this: its targets are identical (`Bird` and
// `Bird`), so neither subsumes the other whether the guard is there or not. Removing the
// guard failed nothing until this case existed.
func TestTraitDispatch_RankingDoesNotReachAcrossTraits(t *testing.T) {
	source := `
struct Box<t> {
    value: t
}
trait Pilot {
    fly: (Self) -> i64
}
trait Wizard {
    fly: (Self) -> i64
}
impl Pilot<t> for Box<t> {
    fly = (self) => 1
}
impl Wizard for Box<i64> {
    fly = (self) => 2
}
let f = (b: Box<i64>) -> i64 => {
    b.fly()
}`
	res := parseCollectAndCheck(t, source, false)
	assertErrorsAre(t, res,
		`call to "fly" is ambiguous between traits Pilot, Wizard; use TraitName::fly(...) to disambiguate`)
}
