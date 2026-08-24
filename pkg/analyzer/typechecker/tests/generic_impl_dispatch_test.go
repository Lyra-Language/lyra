package typechecker_test

import "testing"

// A generic impl `impl Show<t> for Box<t>` is dispatched for a concrete
// `Box<i64>` receiver via a dot-call — the impl's type variable `t` unifies with
// the receiver's `i64`.
func TestGenericImplDispatch_DotCall(t *testing.T) {
	source := `
struct Box<t> { value: t }
trait Show {
    show: (Self) -> string
}
impl Show<t> for Box<t> {
    show = (self) => "box"
}
let f = (b: Box<i64>) -> string => {
    b.show()
}`
	res := parseCollectAndCheck(t, source, false)
	assertNoErrors(t, res)
}

// The fully-qualified form resolves the same generic impl.
func TestGenericImplDispatch_QualifiedCall(t *testing.T) {
	source := `
struct Box<t> { value: t }
trait Show {
    show: (Self) -> string
}
impl Show<t> for Box<t> {
    show = (self) => "box"
}
let f = (b: Box<i64>) -> string => {
    Show::show(b)
}`
	res := parseCollectAndCheck(t, source, false)
	assertNoErrors(t, res)
}

// The dispatched method's return type (from the trait signature, Self
// substituted with the concrete receiver) flows through: `show` returns string,
// so using it where i64 is required is a mismatch.
func TestGenericImplDispatch_ReturnTypeFlowsThrough(t *testing.T) {
	source := `
struct Box<t> { value: t }
trait Show {
    show: (Self) -> string
}
impl Show<t> for Box<t> {
    show = (self) => "box"
}
let f = (b: Box<i64>) -> i64 => {
    b.show()
}`
	res := parseCollectAndCheck(t, source, false)
	assertErrorsAre(t, res, "f: return type mismatch: expected i64, got string")
}

// A type variable used twice in the target must bind consistently: `Pair<t, t>`
// matches a receiver whose two arguments are the same type.
func TestGenericImplDispatch_ConsistentBinding_Ok(t *testing.T) {
	source := `
struct Pair<a, b> { first: a, second: b }
trait Show {
    show: (Self) -> string
}
impl Show<t> for Pair<t, t> {
    show = (self) => "pair"
}
let f = (p: Pair<i64, i64>) -> string => {
    p.show()
}`
	res := parseCollectAndCheck(t, source, false)
	assertNoErrors(t, res)
}

// ...and does NOT match when the two arguments differ (`t` can't be both i64 and
// string), so no impl is found.
func TestGenericImplDispatch_InconsistentBinding_Error(t *testing.T) {
	source := `
struct Pair<a, b> { first: a, second: b }
trait Show {
    show: (Self) -> string
}
impl Show<t> for Pair<t, t> {
    show = (self) => "pair"
}
let f = (p: Pair<i64, string>) -> string => {
    Show::show(p)
}`
	res := parseCollectAndCheck(t, source, false)
	assertErrorsAre(t, res, "no implementation of Show::show for Pair<i64, string>")
}

// A generic impl only matches when the head names agree: an impl for `Box<t>`
// does not provide a method for an unrelated `Crate<i64>`.
func TestGenericImplDispatch_HeadMismatch_Error(t *testing.T) {
	source := `
struct Box<t> { value: t }
struct Crate<t> { item: t }
trait Show {
    show: (Self) -> string
}
impl Show<t> for Box<t> {
    show = (self) => "box"
}
let f = (c: Crate<i64>) -> string => {
    Show::show(c)
}`
	res := parseCollectAndCheck(t, source, false)
	assertErrorsAre(t, res, "no implementation of Show::show for Crate<i64>")
}

// The target may be an array: `impl Show<t> for []t` dispatches for `[]i64`.
func TestGenericImplDispatch_ArrayTarget(t *testing.T) {
	source := `
trait Show {
    show: (Self) -> string
}
impl Show<t> for []t {
    show = (self) => "arr"
}
let f = (xs: []i64) -> string => {
    xs.show()
}`
	res := parseCollectAndCheck(t, source, false)
	assertNoErrors(t, res)
}

// **The target may be a raw pointer**, and this is the case two switches disagreed about
// without either naming it. `unifyGenericTarget` has always had a `RawPointerType` arm, so
// `^t` *could* unify — but the walk that reads an impl target's implicit variables was a
// private copy of `types.CollectTypeVars` with no pointer case, so `generics` came back
// empty and `implTargetMatches` returned false before unification was ever attempted. The
// symptom named neither: `member access on non-struct type ^i64`.
//
// It is CollectTypeVars now (CLAUDE.md rule 8's "one answer" table), which is also why
// `weak t`, `{ v: t }`, a range and a constrained target now reach unification — those
// need arms in `unifyGenericTarget` before they dispatch, but they are no longer rejected
// one step earlier for a different reason.
func TestGenericImplDispatch_RawPointerTarget(t *testing.T) {
	source := `
trait Describe {
    describe: (Self) -> string
}
impl Describe for ^t {
    describe = (self) => "ptr"
}
let f = (p: ^i64) -> string => {
    p.describe()
}`
	res := parseCollectAndCheck(t, source, false)
	assertNoErrors(t, res)
}

// A bounded generic impl (`impl Ord<t> for Box<t> where t: Ord`) applies when the
// receiver's element type satisfies the bound — i64 implements Ord, so a call on
// Box<i64> is accepted.
func TestGenericImplDispatch_BoundSatisfied_Ok(t *testing.T) {
	source := `
struct Box<t> { value: t }
trait Ord { compare: (Self, Self) -> i64 }
impl Ord for i64 { compare = (a, b) => 0 }
impl Ord<t> for Box<t> where t: Ord {
    compare = (self, other) => 0
}
let f = (x: Box<i64>, y: Box<i64>) -> i64 => {
    x.compare(y)
}`
	res := parseCollectAndCheck(t, source, false)
	assertNoErrors(t, res)
}

// ...and is rejected when the element type does not satisfy the bound: Widget
// has no Ord impl, so calling compare on Box<Widget> is an unsatisfied-bound
// error rather than a silent dispatch.
func TestGenericImplDispatch_BoundUnsatisfied_Error(t *testing.T) {
	source := `
struct Box<t> { value: t }
struct Widget { id: i64 }
trait Ord { compare: (Self, Self) -> i64 }
impl Ord for i64 { compare = (a, b) => 0 }
impl Ord<t> for Box<t> where t: Ord {
    compare = (self, other) => 0
}
let f = (x: Box<Widget>, y: Box<Widget>) -> i64 => {
    x.compare(y)
}`
	res := parseCollectAndCheck(t, source, false)
	assertErrorsAre(t, res,
		"Widget does not implement Ord, required by this impl's `t: Ord` bound")
}

// The bound is also checked through the fully-qualified call form.
func TestGenericImplDispatch_BoundUnsatisfied_QualifiedError(t *testing.T) {
	source := `
struct Box<t> { value: t }
struct Widget { id: i64 }
trait Ord { compare: (Self, Self) -> i64 }
impl Ord for i64 { compare = (a, b) => 0 }
impl Ord<t> for Box<t> where t: Ord {
    compare = (self, other) => 0
}
let f = (x: Box<Widget>, y: Box<Widget>) -> i64 => {
    Ord::compare(x, y)
}`
	res := parseCollectAndCheck(t, source, false)
	assertErrorsAre(t, res,
		"Widget does not implement Ord, required by this impl's `t: Ord` bound")
}
