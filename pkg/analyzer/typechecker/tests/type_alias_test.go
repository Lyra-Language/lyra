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
  let s: [2]i64 = [1, 2]
  arr(d) + fixed(s) + tup((1, 2)) + fn((n: i64) -> i64 => n)
}`, false)
	assertNoErrors(t, res)
}
