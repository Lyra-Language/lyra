package llvm

import (
	"strings"
	"testing"
)

// Arithmetic and bitwise operator overloading, end to end (08/07). `a + b` on a user
// type lowers to a call to the `(_+_)` method its impl provides — the same emitted
// function an identifier-named trait method produces, since an operator method *is* an
// ordinary trait method and only its resolution arrives differently.

func TestExec_OperatorAddOnStruct(t *testing.T) {
	t.Parallel()
	const src = `
module main
struct Vec2 { x: i64, y: i64 }
trait Add { (_+_): (Self, Self) -> Self }
impl Add for Vec2 { (_+_) = (self, o) => Vec2 { x: self.x + o.x, y: self.y + o.y } }
let main = () -> void => {
  let a = Vec2 { x: 1, y: 2 };
  let b = Vec2 { x: 10, y: 20 };
  let c = a + b;
  let d = a + b + a;
  println("${c.x} ${c.y} ${d.x} ${d.y}");
}
`
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "11 22 12 24" {
		t.Errorf("overloaded + = %q; want \"11 22 12 24\"", got)
	}
}

// The right operand is whatever the signature says. `v * 3` is half of what anyone
// wants an overloaded `*` for, and it is also the case an "operands must match" rule
// would have quietly ruled out.
func TestExec_OperatorWithScalarRightOperand(t *testing.T) {
	t.Parallel()
	const src = `
module main
struct Vec2 { x: i64, y: i64 }
trait Scale { (_*_): (Self, i64) -> Self }
impl Scale for Vec2 { (_*_) = (self, k) => Vec2 { x: self.x * k, y: self.y * k } }
let main = () -> void => {
  let v = Vec2 { x: 2, y: 3 } * 4;
  println("${v.x} ${v.y}");
}
`
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "8 12" {
		t.Errorf("overloaded * = %q; want \"8 12\"", got)
	}
}

// Prefix `-` and `~`, which are their own methods — told from the binary spellings by
// kind, not by text.
func TestExec_OperatorPrefixForms(t *testing.T) {
	t.Parallel()
	const src = `
module main
struct Vec2 { x: i64, y: i64 }
trait Neg { (-_): (Self) -> Self }
impl Neg for Vec2 { (-_) = (self) => Vec2 { x: 0 - self.x, y: 0 - self.y } }
trait Comp { (~_): (Self) -> Self }
impl Comp for Vec2 { (~_) = (self) => Vec2 { x: ~self.x, y: ~self.y } }
let main = () -> void => {
  let v = Vec2 { x: 1, y: 2 };
  let n = -v;
  let c = ~v;
  println("${n.x} ${n.y} ${c.x} ${c.y}");
}
`
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "-1 -2 -2 -3" {
		t.Errorf("prefix operators = %q; want \"-1 -2 -2 -3\"", got)
	}
}

// `x += y` is `x = x + y`, so it calls the same impl and stores the answer back. It is
// the form that used to type-check and then fail to lower, before dispatch reached it.
func TestExec_OperatorCompoundAssignment(t *testing.T) {
	t.Parallel()
	const src = `
module main
struct Vec2 { x: i64, y: i64 }
trait Add { (_+_): (Self, Self) -> Self }
impl Add for Vec2 { (_+_) = (self, o) => Vec2 { x: self.x + o.x, y: self.y + o.y } }
let main = () -> void => {
  var a = Vec2 { x: 1, y: 2 };
  a += Vec2 { x: 10, y: 20 };
  a += Vec2 { x: 100, y: 200 };
  println("${a.x} ${a.y}");
}
`
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "111 222" {
		t.Errorf("overloaded += = %q; want \"111 222\"", got)
	}
}

// A receiver carrying a **managed** payload, which is where the reference counting has
// to be right: the impl allocates a fresh string per call and the chained form produces
// an intermediate that nothing keeps. Run under ASan by the harness, so a premature
// release shows up as a fault rather than as wrong text.
func TestExec_OperatorOnManagedPayload(t *testing.T) {
	t.Parallel()
	const src = `
module main
struct Name { s: string }
trait Concat { (_+_): (Self, Self) -> Self }
impl Concat for Name { (_+_) = (self, o) => Name { s: self.s ++ o.s } }
let main = () -> void => {
  let a = Name { s: "he" };
  let b = Name { s: "llo" };
  let one = a + b;
  let two = a + b + a;
  println(one.s);
  println(two.s);
}
`
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "hello\nhellohe" {
		t.Errorf("managed payload = %q; want \"hello\\nhellohe\"", got)
	}
}

// A `data` receiver, whose impl destructures both operands.
func TestExec_OperatorOnDataType(t *testing.T) {
	t.Parallel()
	const src = `
module main
data Money = Cents(i64)
trait Add { (_+_): (Self, Self) -> Self }
impl Add for Money {
  (_+_) = (self, o) => match (self, o) { (Cents a, Cents b) => Cents(a + b) }
}
let main = () -> void => {
  let a = Cents(150);
  let b = Cents(275);
  match a + b { Cents(n) => println("${n}") };
}
`
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "425" {
		t.Errorf("data operator = %q; want \"425\"", got)
	}
}

// **A primitive is never routed through an impl**, and this asserts it the way the
// `Ord` test does: with an impl that is deliberately wrong. If `1 + 2` ever reached it
// the answer would be 999.
func TestExec_OperatorImplCannotChangePrimitiveArithmetic(t *testing.T) {
	t.Parallel()
	const src = `
module main
trait Add { (_+_): (Self, Self) -> Self }
impl Add for i64 { (_+_) = (self, o) => 999 }
let main = () -> void => {
  println("${1 + 2} ${10 * 10}");
}
`
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "3 100" {
		t.Errorf("got %q; want \"3 100\" — an impl must not reach primitive arithmetic", got)
	}
}

// A generic impl, monomorphized per instantiation like any other generic trait method.
func TestExec_OperatorGenericImpl(t *testing.T) {
	t.Parallel()
	const src = `
module main
struct Box<t> { v: t }
trait Add { (_+_): (Self, Self) -> Self }
impl Add for Box<t> { (_+_) = (self, o) => o }
let main = () -> void => {
  let a = Box { v: 1 };
  let b = Box { v: 2 };
  let c = a + b;
  println("${c.v}");
}
`
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "2" {
		t.Errorf("generic impl = %q; want \"2\"", got)
	}
}

// The two forms an overloaded operator makes people write, and which did not parse
// until 08/07 — both grammar gaps rather than dispatch ones, and both found by writing
// the tests above.
//
//   - `Cents(150) + Cents(275)`: a constructor call could not be a math operand.
//     `tuple_literal` lives in `_literal`, which `_math_operand` does not reach, so the
//     parse died at the operator even though `f(1) + f(2)` was fine.
//   - `(a + b).x`: a parenthesized *binary* expression could not be a postfix head. It
//     parses as `group`, which was reachable only from `_math_expr`; `(a).x` worked
//     because a non-math parenthesis is a `parenthesized_expr`, which is a primary.
//
// Both fixes made the parser **smaller** (7730 → 7711 states), because each removed a
// duplicate path rather than adding one.
func TestExec_OperatorOnConstructorCallsAndParenthesizedResults(t *testing.T) {
	t.Parallel()
	const src = `
module main
data Money = Cents(i64)
trait Add { (_+_): (Self, Self) -> Self }
impl Add for Money {
  (_+_) = (self, o) => match (self, o) { (Cents a, Cents b) => Cents(a + b) }
}
struct Vec2 { x: i64, y: i64 }
trait AddV { (_+_): (Self, Self) -> Self }
impl AddV for Vec2 { (_+_) = (self, o) => Vec2 { x: self.x + o.x, y: self.y + o.y } }
let main = () -> void => {
  match Cents(150) + Cents(275) { Cents(n) => println("${n}") };
  let a = Vec2 { x: 1, y: 2 };
  println("${(a + a).x} ${(1 + 2).wrapping_add(3)}");
}
`
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "425\n2 6" {
		t.Errorf("got %q; want \"425\\n2 6\"", got)
	}
}

// An operator whose operand is a **type parameter**, resolved through a `where` bound
// (08/08). This is `dispatchViaGenericBound` for a node that is not a call: the
// typechecker checks against the trait's declared signature and publishes one resolution
// per implementing type, and the specialization picks by the substituted operand type —
// exactly what a bound-dispatched `.method()` call has done since 08/07.
func TestExec_OperatorThroughAWhereBound(t *testing.T) {
	t.Parallel()
	const src = `
module main
struct Vec2 { x: i64 }
struct Money { c: i64 }
trait Add { (_+_): (Self, Self) -> Self }
impl Add for Vec2 { (_+_) = (self, o) => Vec2 { x: self.x + o.x } }
impl Add for Money { (_+_) = (self, o) => Money { c: self.c + o.c } }
let total<t> where t: Add = (a: t, b: t) -> t => a + b
let main = () -> void => {
  println("${total(Vec2 { x: 1 }, Vec2 { x: 2 }).x} ${total(Money { c: 5 }, Money { c: 6 }).c}");
}
`
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "3 11" {
		t.Errorf("bound-dispatched operator = %q; want \"3 11\"", got)
	}
}

// The prefix and compound-assignment forms take the same route. The compound one is the
// case that needed the typechecker to record the *target's* type on the left node: a
// compound assignment never infers its own left-hand side, so nothing else put a type
// there and the backend had no substituted receiver to look the candidate up by.
func TestExec_OperatorThroughABoundPrefixAndCompound(t *testing.T) {
	t.Parallel()
	const src = `
module main
struct Vec2 { x: i64 }
trait Add { (_+_): (Self, Self) -> Self }
impl Add for Vec2 { (_+_) = (self, o) => Vec2 { x: self.x + o.x } }
trait Neg { (-_): (Self) -> Self }
impl Neg for Vec2 { (-_) = (self) => Vec2 { x: 0 - self.x } }
let flip<t> where t: Neg = (a: t) -> t => -a
let triple<t> where t: Add = (a: t) -> t => a + a + a
let accum<t> where t: Add = (a: t, b: t) -> t => {
  var acc = a;
  acc += b;
  acc
}
let main = () -> void => {
  println("${flip(Vec2 { x: 7 }).x} ${triple(Vec2 { x: 3 }).x} ${accum(Vec2 { x: 4 }, Vec2 { x: 5 }).x}");
}
`
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "-7 9 9" {
		t.Errorf("bound prefix/compound = %q; want \"-7 9 9\"", got)
	}
}

// An operator impl on a *scalar newtype* dispatches (08/12). The dispatch guard used
// to strip the newtype before refusing scalar receivers, which made a scalar newtype
// operator-dead from both sides — the built-in numeric rule refused the nominal type
// ("operands must be numeric, got Cents and Cents") and the guard refused the base —
// so `impl Add for Cents` parsed, collected, and was silently inert. This is the
// opt-in path lyra-E043 points at now that the overflow-arithmetic builtins no longer
// reach through the wrapper, so the impl body demonstrates the whole loop: read the
// operands into the base (one-step documented assignability), do wrapping arithmetic
// there, and return through `-> Self`'s construction-assignability.
func TestExec_OperatorOnScalarNewtype(t *testing.T) {
	t.Parallel()
	const src = `
module main
newtype Cents = i64
trait Add { (_+_): (Self, Self) -> Self }
impl Add for Cents {
  (_+_) = (self, o) => {
    let a: i64 = self
    let b: i64 = o
    a.wrapping_add(b)
  }
}
let main = () -> void => {
  let x: Cents = 150
  let y: Cents = 275
  let sum: i64 = x + y
  println(sum)
}
`
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "425" {
		t.Errorf("newtype + = %q; want \"425\"", got)
	}
}
