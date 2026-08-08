package checker_test

import "testing"

// An overloaded operator is a **call**, and the effect ladders have to charge it as
// one. `a + b` on a type whose `(_+_)` prints is exactly as impure as writing the call
// out, and a `pure` function using it must be refused.
//
// This closes a hole `Eq`/`Ord` opened first: the comparison operators have dispatched
// to an impl since 08/07 and none of the three ladders looked, so a `pure` function
// comparing two values through an `Ord::compare` that printed type-checked clean.
// Arithmetic would have been a second instance of the same miss, which is why the fix
// keys on the *resolution* rather than on which operator it is (operatorImplEffect).

func TestOperatorPurity_ImpureArithmeticImpl_Flagged(t *testing.T) {
	src := `
struct Vec2 { x: i64 }
trait Add { (_+_): (Self, Self) -> Self }
impl Add for Vec2 {
    (_+_) = (self, o) => {
        println("side effect")
        Vec2 { x: self.x + o.x }
    }
}
let sum = pure (a: Vec2, b: Vec2) -> Vec2 => a + b`
	assertBoundError(t, checkPurity(t, src), "lyra-E007")
}

// A pure impl is fine — the operator is only as impure as what it calls.
func TestOperatorPurity_PureArithmeticImpl_Ok(t *testing.T) {
	src := `
struct Vec2 { x: i64 }
trait Add { (_+_): (Self, Self) -> Self }
impl Add for Vec2 { (_+_) = (self, o) => Vec2 { x: self.x + o.x } }
let sum = pure (a: Vec2, b: Vec2) -> Vec2 => a + b`
	assertPurityCount(t, checkPurity(t, src), 0)
}

// The prefix form goes through the same resolution, so it is charged the same way.
func TestOperatorPurity_ImpurePrefixImpl_Flagged(t *testing.T) {
	src := `
struct Vec2 { x: i64 }
trait Neg { (-_): (Self) -> Self }
impl Neg for Vec2 {
    (-_) = (self) => {
        println("side effect")
        self
    }
}
let flip = pure (a: Vec2) -> Vec2 => -a`
	assertBoundError(t, checkPurity(t, src), "lyra-E007")
}

// A compound assignment calls the impl too, so it carries the impl's effects.
func TestOperatorPurity_ImpureCompoundAssignment_Flagged(t *testing.T) {
	src := `
struct Vec2 { x: i64 }
trait Add { (_+_): (Self, Self) -> Self }
impl Add for Vec2 {
    (_+_) = (self, o) => {
        println("side effect")
        Vec2 { x: self.x + o.x }
    }
}
let sum = pure (b: Vec2) -> Vec2 => {
    var a = Vec2 { x: 1 }
    a += b
    a
}`
	assertBoundError(t, checkPurity(t, src), "lyra-E007")
}

// The pre-existing half of the same hole: a comparison operator dispatching to an
// impure `Ord::compare`. It was invisible until operatorImplEffect existed, and it is
// the reason that helper keys on the resolution rather than on the operator.
func TestOperatorPurity_ImpureOrdCompare_Flagged(t *testing.T) {
	src := `
data Ordering = Less | Equal | Greater
struct Ver { v: i64 }
trait Ord { compare: (Self, Self) -> Ordering }
impl Ord for Ver {
    compare = (self, other) => {
        println("comparing")
        self.v <=> other.v
    }
}
let less = pure (a: Ver, b: Ver) -> bool => a < b`
	assertBoundError(t, checkPurity(t, src), "lyra-E007")
}

// An operator resolved through a `where` bound names no single impl, so its effect is the
// join over every impl of that trait method — the rule a bound *call* already followed.
//
// This did not work when the bound dispatch first landed: the impl groups the join reads
// were built from **identifier-named** methods only, a filter written when nothing
// dispatched to an operator method. The join therefore ran over an empty group and
// answered "pure", so a `pure` function using a bound operator whose impl printed
// type-checked clean.
func TestOperatorPurity_ImpureImplBehindABound_Flagged(t *testing.T) {
	src := `
struct Vec2 { x: i64 }
trait Add { (_+_): (Self, Self) -> Self }
impl Add for Vec2 {
    (_+_) = (self, o) => {
        println("side effect")
        Vec2 { x: self.x + o.x }
    }
}
let total<t> where t: Add = pure (a: t, b: t) -> t => a + b`
	assertBoundError(t, checkPurity(t, src), "lyra-E007")
}

func TestOperatorPurity_PureImplBehindABound_Ok(t *testing.T) {
	src := `
struct Vec2 { x: i64 }
trait Add { (_+_): (Self, Self) -> Self }
impl Add for Vec2 { (_+_) = (self, o) => Vec2 { x: self.x + o.x } }
let total<t> where t: Add = pure (a: t, b: t) -> t => a + b`
	assertPurityCount(t, checkPurity(t, src), 0)
}
