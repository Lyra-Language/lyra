package checker_test

import (
	"strings"
	"testing"

	diag "github.com/Lyra-Language/lyra/pkg/diagnostic"
)

// A write through a `mut` parameter lands on the caller's argument, so a call is charged
// by what that argument is rooted in — as if the caller had written there itself. Until
// 10/16 EffectMut was one bit and such a callee looked like one writing a capture, so a
// `pure` function could not call a helper on its own `var` (Sheliak's `new_cartridge`
// calling `declare_save(cart)`; LZMA's range decoder).

const mutParamPrelude = `
struct Cart { saves: i64 }
struct Pair { a: Cart, b: Cart }
var global_cart = Cart { saves: 0 }
let bump = (cart: mut Cart) -> void => { cart.saves += 1 }
let bump_twice = (cart: mut Cart) -> void => {
    bump(cart)
    bump(cart)
}
let bump_global = (cart: mut Cart) -> void => {
    cart.saves += 1
    global_cart.saves += 1
}
`

// assertPurityMessages checks the purity errors, in order, each by a fragment of its text.
func assertPurityMessages(t *testing.T, errs []diag.Diagnostic, want ...string) {
	t.Helper()
	if len(errs) != len(want) {
		t.Fatalf("expected %d purity error(s), got %d: %v", len(want), len(errs), errs)
	}
	for i, w := range want {
		if !strings.Contains(errs[i].Message, w) {
			t.Fatalf("error %d: want %q in %q", i, w, errs[i].Message)
		}
	}
}

// The case the entry was written for: a helper on the function's own local.
func TestPurity_MutParamCallOnALocal_Ok(t *testing.T) {
	assertPurityCount(t, checkPurity(t, mutParamPrelude+`
let fresh = pure () -> i64 => {
    var c = Cart { saves: 0 }
    bump(c)
    c.saves
}`), 0)
}

// The write is passed on through an unmarked function's own `mut` parameter, and
// discharged where the argument is finally a local — two calls away.
func TestPurity_MutParamWriteThroughTwoLevels_Ok(t *testing.T) {
	assertPurityCount(t, checkPurity(t, mutParamPrelude+`
let fresh = pure () -> i64 => {
    var c = Cart { saves: 0 }
    bump_twice(c)
    c.saves
}`), 0)
}

// A field or element of a local is the local's.
func TestPurity_MutParamCallOnALocalsField_Ok(t *testing.T) {
	assertPurityCount(t, checkPurity(t, mutParamPrelude+`
let fresh = pure () -> i64 => {
    var p = Pair { a: Cart { saves: 0 }, b: Cart { saves: 1 } }
    bump(p.a)
    var cs = [Cart { saves: 2 }]
    bump(cs[0])
    p.a.saves + cs[0].saves
}`), 0)
}

// A captured binding handed to a writer is the captured write it always was — reported
// once, as the write, not again as a read of mutable state.
func TestPurity_MutParamCallOnACapture_Error(t *testing.T) {
	assertPurityMessages(t, checkPurity(t, mutParamPrelude+`
let shared = pure () -> i64 => {
    bump(global_cart)
    0
}`), `passes captured binding "global_cart" to "bump"`)
}

// Through two levels too: the unmarked middle function passes its parameter on, and the
// capture is still what the `pure` caller supplied.
func TestPurity_MutParamWriteThroughTwoLevelsOnACapture_Error(t *testing.T) {
	assertPurityMessages(t, checkPurity(t, mutParamPrelude+`
let shared = pure () -> i64 => {
    bump_twice(global_cart)
    0
}`), `passes captured binding "global_cart" to "bump_twice"`)
}

// A `pure` function handing its *own* `mut` parameter on writes its caller's value, which
// is the direct write `pure` refuses — so the indirect one is refused the same way.
func TestPurity_MutParamPassedOnFromAPureFunction_Error(t *testing.T) {
	assertPurityMessages(t, checkPurity(t, mutParamPrelude+`
let passes_on = pure (c: mut Cart) -> void => {
    bump(c)
}`), "passes `mut`-borrowed parameter \"c\" to \"bump\"")
}

// Only the parameter writes are discharged: a callee that also writes a global is impure
// whatever its argument is.
func TestPurity_MutParamCalleeWithAnotherEffect_Error(t *testing.T) {
	assertPurityMessages(t, checkPurity(t, mutParamPrelude+`
let fresh = pure () -> i64 => {
    var c = Cart { saves: 0 }
    bump_global(c)
    c.saves
}`), `calls impure function "bump_global"`)
}

// A trait method's `mut Self` is a written parameter like any other, through the method
// form (receiver outside the arguments) and the path form (receiver first among them).
func TestPurity_MutSelfMethodOnALocal_Ok(t *testing.T) {
	assertPurityCount(t, checkPurity(t, `
struct Counter { n: i64 }
trait Tick { tick: (mut Self, i64) -> void }
impl Tick for Counter {
    tick = (self, k) => {
        self.n += k
    }
}
let fresh = pure () -> i64 => {
    var c = Counter { n: 0 }
    c.tick(3)
    Tick::tick(c, 4)
    c.n
}`), 0)
}

func TestPurity_MutSelfMethodOnACapture_Error(t *testing.T) {
	assertPurityMessages(t, checkPurity(t, `
struct Counter { n: i64 }
trait Tick { tick: (mut Self, i64) -> void }
impl Tick for Counter {
    tick = (self, k) => {
        self.n += k
    }
}
var counter = Counter { n: 0 }
let shared = pure () -> i64 => {
    counter.tick(3)
    Tick::tick(counter, 4)
    0
}`),
		`passes captured binding "counter" to "tick"`,
		`passes captured binding "counter" to "tick"`)
}

// An `impl` block's `self: mut` member is a free function called method-style, whose
// receiver the desugaring puts first among the arguments.
func TestPurity_ImplBlockMutSelfOnALocal_Ok(t *testing.T) {
	assertPurityCount(t, checkPurity(t, `
struct Counter { n: i64 }
impl Counter {
    let bump = (self: mut) => {
        self.n += 1
    }
}
let fresh = pure () -> i64 => {
    var c = Counter { n: 0 }
    c.bump()
    c.n
}`), 0)
}

// A function whose only effect was a helper on its own local is now a W018 candidate; one
// that writes through its own `mut` parameter, directly or by handing it on, is not.
func TestMissingPureBound_MutParamCallOnALocal(t *testing.T) {
	assertWarned(t, missingPureBounds(t, `
struct Cart { saves: i64 }
let bump = (cart: mut Cart) -> void => { cart.saves += 1 }
let bump_twice = (cart: mut Cart) -> void => { bump(cart) }
let fresh = () -> i64 => {
    var c = Cart { saves: 0 }
    bump(c)
    c.saves
}
`), "fresh")
}

// `Trait::method(r, f)` passes its receiver first, where `r.method(f)` keeps it in the
// callee, and until 10/16 both were read with the method form's offset — so the path
// form's callback was read from the receiver's position. A pure callback was refused
// (the receiver is no function) and the impure one was refused for the wrong reason.
func TestPurity_TraitPathCallReadsItsCallbackInPlace(t *testing.T) {
	const runner = `
trait Runner { run: (Self, () -> i64) -> i64 }
struct Runbox { n: i64 }
impl Runner for Runbox { run = (self, f) => f() + self.n }
let quiet = pure () -> i64 => 42
let noisy = () -> i64 => {
  println("noise")
  42
}
`
	assertPurityCount(t, checkPurity(t, runner+`
let calm = pure (r: Runbox) -> i64 => Runner::run(r, quiet)
`), 0)
	assertPurityMessages(t, checkPurity(t, runner+`
let loud = pure (r: Runbox) -> i64 => Runner::run(r, noisy)
`), "impure callback argument")
}
