package typechecker_test

import (
	"strings"
	"testing"
)

// **A bound on a generic type's parameter is enforced at instantiation** (09/16, lyra-E036).
//
// `struct Bx<t: Tag>` parsed, stored the bound on `GenericParam.Constraints`, and nothing
// ever read it — `Bx<NoTag>` compiled and ran. The same spelling was already enforced on a
// *function*, which is why this enforces rather than refusing the syntax: refusing would
// make `<t: Tag>` mean something on a `let` and be illegal on a `struct`, for a bound the
// language already knows how to check.
func TestGenericType_ABoundOnItsParameterIsEnforced(t *testing.T) {
	res := parseCollectAndCheck(t, `trait Tag { tag: (Self) -> string }
struct Bx<t: Tag> { item: t }
struct NoTag { n: i64 }
let b: Bx<NoTag> = Bx { item: NoTag { n: 7 } }`, false)
	assertHasErrorContaining(t, res, "Bx: t is instantiated at NoTag, which does not implement Tag")
}

// One mistake, one diagnostic. A written instantiation is re-resolved at every position that
// asks about the value, not only where it was written, so the report is keyed by the
// instantiation rather than by the location — keyed by location it fired once per use.
func TestGenericType_AnUnsatisfiedBoundIsReportedOnce(t *testing.T) {
	res := parseCollectAndCheck(t, `trait Tag { tag: (Self) -> string }
struct Bx<t: Tag> { item: t }
struct NoTag { n: i64 }
let b: Bx<NoTag> = Bx { item: NoTag { n: 7 } }
let n: i64 = b.item.n
let m: i64 = b.item.n`, false)
	count := 0
	for _, e := range res.errors {
		if strings.Contains(e.Message, "does not implement Tag") {
			count++
		}
	}
	if count != 1 {
		t.Errorf("want exactly one bound diagnostic, got %d: %v", count, res.errors)
	}
}

// A type that satisfies the bound is accepted.
func TestGenericType_ASatisfiedBoundIsAccepted(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `trait Tag { tag: (Self) -> string }
struct Bx<t: Tag> { item: t }
struct Yes { n: i64 }
impl Tag for Yes { tag = pure (self) => "yes" }
let b: Bx<Yes> = Bx { item: Yes { n: 7 } }`, false))
}

// **A type-variable argument is skipped**, not reported. Inside `Wrap<t>` the question is
// whether `Wrap`'s own `t` carries the bound, which belongs to the enclosing declaration and
// is not in hand where a type is resolved. Erring toward silence is the right direction for a
// check added to a language that already has programs in it.
func TestGenericType_ATypeVariableArgumentIsNotReported(t *testing.T) {
	assertNoErrors(t, parseCollectAndCheck(t, `trait Tag { tag: (Self) -> string }
struct Bx<t: Tag> { item: t }
struct Wrap<t> { b: Bx<t> }`, false))
}
