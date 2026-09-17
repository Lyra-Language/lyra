package typechecker_test

import "testing"

// **A bound on a trait's own generic parameter is enforced at the impl** (09/16, lyra-E036).
//
// `trait Holder<t: Tag>` parsed, stored the bound, and nothing read it:
// `impl Holder<NoTag> for Cell` compiled and ran with no `Tag` impl on `NoTag`. This is the
// sibling of the generic-*type* bound fixed the same day, in the one place that fix cannot
// reach — checkGenericTypeBounds hangs off resolving a type, and a trait is never one.
//
// The impl is where it belongs, because the impl is what binds the parameter, and it sits
// beside the supertrait check for the same reason: the impl and the trait are both in hand.
func TestTraitParam_ABoundIsEnforcedAtTheImpl(t *testing.T) {
	res := parseCollectAndCheck(t, `trait Tag { tag: (Self) -> string }
struct NoTag { n: i64 }
trait Holder<t: Tag> { get: (Self) -> t }
struct Cell { v: NoTag }
impl Holder<NoTag> for Cell { get = pure (self) => self.v }`, false)
	assertHasErrorContaining(t, res, "t is bound at NoTag, which does not implement Tag")
}

// A trait argument that satisfies the bound is accepted.
func TestTraitParam_ASatisfiedBoundIsAccepted(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `trait Tag { tag: (Self) -> string }
struct Yes { n: i64 }
impl Tag for Yes { tag = pure (self) => "yes" }
trait Holder<t: Tag> { get: (Self) -> t }
struct Cell { v: Yes }
impl Holder<Yes> for Cell { get = pure (self) => self.v }`, false))
}

// **A type-variable argument is skipped**, as on the type side: in `impl Holder<t> for Box<t>`
// the question is whether the *impl's* `t` carries the bound, which is its `where` clause's
// business and is checked when that clause is used.
func TestTraitParam_AGenericImplIsNotFalselyReported(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `trait Tag { tag: (Self) -> string }
trait Holder<t: Tag> { get: (Self) -> t }
struct Box<t> { v: t }
impl Holder<t> for Box<t> where t: Tag { get = pure (self) => self.v }`, false))
}
