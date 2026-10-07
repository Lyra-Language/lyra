package typechecker_test

import "testing"

// A `type` alias is **transparent**: the name and the type it names are
// interchangeable, with no conversion at the boundary and no distinct identity. That
// is the whole difference from `newtype`, which is nominal.
//
// It is implemented by registering the aliased type *itself* under the alias's name,
// so most of the language needs no knowledge of aliases at all — these tests exist to
// pin that the transparency actually holds at each place a type is compared, since
// "resolve one hop and stop" passes a naive test and fails on `type Point = Pt`.

func TestTypeAlias_FunctionTypeIsInterchangeable(t *testing.T) {
	// The motivating case: a function type is the shape Lyra reads worst, and the
	// double parens (one *tuple* parameter, since single parens would be two
	// arguments) cannot be spelled away — only named.
	res := parseCollectAndCheck(t, `type Op = ((i64, i64)) -> i64
let apply = (g: Op, p: (i64, i64)) -> i64 => g(p)
let use = () -> i64 => apply(((a, b)) => a * b, (3, 4))`, false)
	assertNoErrors(t, res)
}

// An alias naming another *named* type is the case that catches a one-hop resolver:
// `Point` registers `UnresolvedType{Pt}`, so stopping after one lookup hands back a
// name and assignability then rejects a real Pt with "cannot assign Pt to Point".
func TestTypeAlias_ToNamedTypeIsInterchangeable(t *testing.T) {
	res := parseCollectAndCheck(t, `struct Pt { x: i64, y: i64 }
type Point = Pt
let sum = (p: Point) -> i64 => p.x + p.y
let use = () -> i64 => sum(Pt { x: 3, y: 4 })`, false)
	assertNoErrors(t, res)
}

func TestTypeAlias_ChainResolves(t *testing.T) {
	res := parseCollectAndCheck(t, `struct Pt { x: i64 }
type A = Pt
type B = A
let get = (p: B) -> i64 => p.x
let use = () -> i64 => get(Pt { x: 9 })`, false)
	assertNoErrors(t, res)
}

func TestTypeAlias_InReturnPosition(t *testing.T) {
	res := parseCollectAndCheck(t, `type Op = (i64) -> i64
let mk = (n: i64) -> Op => (x: i64) -> i64 => x + n`, false)
	assertNoErrors(t, res)
}

// A cycle must be reported rather than resolved forever. The guard is the type-level
// twin of the one in inferExprType, and for the same reason: the alternative is a
// stack overflow that takes the language server with it.
func TestTypeAlias_CircularIsRejected(t *testing.T) {
	res := parseCollectAndCheck(t, `type A = B
type B = A`, false)
	assertHasErrorContaining(t, res, "is circular")
}

// Both of these are checked at the *declaration*, with no use site. An alias that
// nothing mentions would otherwise be checked by nobody, and a declaration that
// cannot mean anything should not need a use to be told so.
func TestTypeAlias_UnknownTargetIsRejectedWithoutAUse(t *testing.T) {
	res := parseCollectAndCheck(t, `type X = Nonexistent`, false)
	assertHasErrorContaining(t, res, `unknown type "Nonexistent"`)
}

func TestTypeAlias_UnusedButValidIsSilent(t *testing.T) {
	res := parseCollectAndCheck(t, `type Op = (i64) -> i64`, false)
	assertNoErrors(t, res)
}

// The alias is transparent, so it must *not* behave like `newtype`: a value of the
// aliased type needs no conversion, and — the other direction — an alias does not
// create a type that rejects its own base. This is the assertion that would fail if
// someone "improved" aliases into nominal types.
func TestTypeAlias_IsNotNominal(t *testing.T) {
	res := parseCollectAndCheck(t, `type Id = i64
let plain = (n: i64) -> i64 => n
let aliased = (n: Id) -> Id => n
let use = () -> i64 => plain(aliased(1)) + aliased(plain(2))`, false)
	assertNoErrors(t, res)
}

// **Transparency has to reach inside a composite**, and the pointee is where it did not:
// `^mut Id` and `^mut i64` were different types until 08/22, because resolveTypeWith had
// no RawPointerType case and so left the pointee an unresolved name. A pointee is
// invariant, so an unresolved name there is not a near-miss — it is a flat rejection.
//
// The shape it broke is the one that matters: a C in/out parameter is a pointer, so
// `std.ffi`'s `CULong` could not be used for `uLongf *destLen`, which is most of what the
// alias exists for. `&mut n` on an `i64` binding produced `^mut i64` and the parameter
// wanted `^mut Id`.
func TestTypeAlias_IsTransparentInsideAPointer(t *testing.T) {
	res := parseCollectAndCheck(t, `type Id = i64
let through = unsafe (p: ^mut Id) -> i64 => unsafe { p^ }
let use = () -> i64 => { var n: i64 = 1; unsafe { through(&mut n) } }`, false)
	assertNoErrors(t, res)
}

// The same walk, one composite over — a guard against fixing only the case that was
// reported. An alias inside an array, a tuple and a function type all have to resolve, and
// each of those cases was added to the walk for a failure of its own.
func TestTypeAlias_IsTransparentInsideEveryComposite(t *testing.T) {
	res := parseCollectAndCheck(t, `type Id = i64
let arr = pure (xs: []Id) -> i64 => xs.len()
let fixed = pure (xs: [2]Id) -> i64 => xs.len()
let tup = pure (p: (Id, Id)) -> i64 => p.0
let fn = pure (f: (Id) -> Id) -> i64 => f(1)
let use = () -> i64 => {
  var d: []i64 = [1]
  let s: [2]i64 = #[1, 2]
  arr(d) + fixed(s) + tup((1, 2)) + fn((n: i64) -> i64 => n)
}`, false)
	assertNoErrors(t, res)
}

// **An alias applied to an operand is lyra-E064**, and the message names the spelling
// that was wanted. Before 08/22 it was "CULong: not a tuple type" — `Name(x)` parses as a
// tuple literal, so the diagnostic reported the *parse* rather than the language, about a
// type that is not a tuple and was never going to be. That is the same wording lyra-E044's
// history records having already replaced once, for `newtype`.
func TestTypeAlias_ConstructionNamesTheConversion(t *testing.T) {
	res := parseCollectAndCheck(t, `type CULong = u64
let use = () -> u64 => CULong(5)`, false)
	assertHasErrorContaining(t, res, "has no constructor")
	assertHasErrorContaining(t, res, "write `u64(...)`")
}

// The juxtaposed spelling is the same node — the collector erases `CULong 5` into the
// call form — so it must reach the same arm rather than falling through to a second,
// worse message.
func TestTypeAlias_JuxtaposedConstructionIsTheSameError(t *testing.T) {
	res := parseCollectAndCheck(t, `type CULong = u64
let use = () -> u64 => CULong 5`, false)
	assertHasErrorContaining(t, res, "has no constructor")
}

// Where the aliased type is one no conversion can *name*, there is no spelling to offer
// and none is needed — so the message says the operand already has that type instead of
// pointing at a wrapper that does not exist. Naming `[]i64(…)` would be worse than saying
// nothing: it does not parse.
func TestTypeAlias_ConstructionOfAnUnconvertibleBaseSaysDropIt(t *testing.T) {
	res := parseCollectAndCheck(t, `type Row = []i64
let use = () -> i64 => { let r = Row([1, 2]); r[0] }`, false)
	assertHasErrorContaining(t, res, "Drop the wrapper")
}

// The arm must not swallow the case it sits next to: a `newtype` **does** have a
// constructor, and `Cents(150)` is how a base value becomes one.
func TestTypeAlias_ANewtypeStillConstructs(t *testing.T) {
	res := parseCollectAndCheck(t, `newtype Cents = i64
let use = () -> Cents => Cents(150)`, false)
	assertNoErrors(t, res)
}

// A `self` receiver written as an alias takes a method call — on a value of the alias
// and of the type it names, as the plain call `first(ys)` always did. receiverAccepts
// unified the written `self` type, where `A` was still a *name*, against the receiver's
// resolved type, and nominal matching read `A` as a head of its own: `ys.first()` was
// refused with "…; first takes DynamicArray<i64>", naming the type it had just refused
// (10/07). Each spelling a receiver match compares: a bare alias, an alias of a
// struct (heads `Point` and `Pt`), an overload set, and an alias inside a type argument.
func TestTypeAlias_ReceiverTakesAMethodCall(t *testing.T) {
	cases := map[string]string{
		"alias of a dynamic array": `type A = []i64
let first = (self: A) -> i64 => self[0]
let use = (ys: A, zs: []i64) -> i64 => ys.first() + zs.first() + first(ys)`,
		"alias of a struct": `struct Pt { x: i64 }
type Point = Pt
let getx = (self: Point) -> i64 => self.x
let use = (p: Pt) -> i64 => p.getx()`,
		"a member of an overload set": `type A = []i64
let total = (self: A) -> i64 => self[0]
let total = (self: string) -> i64 => self.len()
let use = (ys: A) -> i64 => ys.total() + total(ys) + "ab".total()`,
		"an alias inside a type argument": `type A = []i64
type Pair = (A, A)
let width = (self: [2]A) -> i64 => self[0].len()
let left = (self: Pair) -> i64 => self.0[0]
let use = (g: [2][]i64, p: ([]i64, []i64)) -> i64 => g.width() + p.left()`,
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			assertNoErrors(t, parseCollectAndCheck(t, src, false))
		})
	}
}

// Two overloads whose receivers are one type spelled through an alias overlap, and the
// overlap is the declaration's error — registration compared the written heads (`A`, `[]`)
// before the alias could be resolved, so the pair was admitted, and every call was then
// an ambiguity. One error, at the second declaration, in registration's wording.
func TestTypeAlias_OverloadsOverlappingThroughAnAliasAreRefused(t *testing.T) {
	res := parseCollectAndCheck(t, `type A = []i64
let total = (self: A) -> i64 => self[0]
let total = (self: []i64) -> i64 => self[1]
let use = (ys: A) -> i64 => ys.total() + total(ys)`, false)
	if len(res.errors) != 1 {
		t.Fatalf("want exactly one error, got %d: %v", len(res.errors), res.errors)
	}
	assertHasErrorContaining(t, res, "Both take a `[]` receiver once the alias is expanded")
}
