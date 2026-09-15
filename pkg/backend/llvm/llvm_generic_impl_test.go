package llvm

import (
	"os/exec"
	"regexp"
	"strings"
	"testing"
)

// Monomorphized trait-impl methods: one emitted function per (impl method, bindings).
//
// Before 08/03 there was one function per impl method, full stop. A body that touched the
// impl's type variable could not lower at all (`match on Maybe<t> not implemented yet`),
// and — worse, because it was silent — a body that did *not* touch it lowered once and was
// called with every receiver type, passing a `%Box$boolean` into an i64-shaped parameter.
// Apple clang accepts that; opaque pointers make the two function types indistinguishable.

const optUnwrap = `data Opt<t> = Nil | Just t

trait Unwrap<e> { unwrap: (Self, e) -> e }

impl Unwrap<t> for Opt<t> {
  unwrap = (self, fallback) => match self {
    Just v => v,
    Nil => fallback,
  }
}
`

// The motivating case: a generic impl whose body matches on the receiver.
func TestExec_GenericImplMethodRuns(t *testing.T) {
	t.Parallel()
	src := optUnwrap + `
let main = () -> u8 => {
  let m: Opt<i64> = Just 7
  let n: Opt<i64> = Nil
  u8(m.unwrap(0) + n.unwrap(35))
}
`
	if got := buildAndRun(t, src); got != 42 {
		t.Errorf("exited %d; want 42 (7 from the Just, 35 from the Nil's fallback)", got)
	}
}

// Two instantiations produce two functions, and each call site reaches its own. This is
// the miscompile, pinned: the assertion is not merely that two definitions exist but that
// no call passes an argument type its callee does not declare.
func TestEmit_GenericImplMethodPerInstantiation(t *testing.T) {
	t.Parallel()
	const src = `struct Box<t> { value: t }
trait Sized { size: (Self) -> i64 }
impl Sized for Box<t> { size = (self) => 8 }

let main = () -> u8 => {
  let a = Box { value: 7 }
  let b = Box { value: true }
  u8(a.size() + b.size() - 16)
}
`
	ir, err := emitSource(t, src)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	text := string(ir)

	// One definition per receiver type…
	defs := regexp.MustCompile(`define i64 @Box\$Sized\$size\$\w+\((%Box\$\w+)`).FindAllStringSubmatch(text, -1)
	if len(defs) != 2 {
		t.Fatalf("expected two specializations of Box$Sized$size, got %d:\n%s", len(defs), text)
	}
	params := map[string]string{} // function name → declared receiver type
	for _, d := range regexp.MustCompile(`define i64 (@Box\$Sized\$size\$\w+)\((%Box\$\w+)`).FindAllStringSubmatch(text, -1) {
		params[d[1]] = d[2]
	}

	// …and every call passes the type that definition declares. Reading the argument
	// type at the call site is the whole point: the old single definition still had a
	// well-formed *signature*, and it was the callers that disagreed with it.
	calls := regexp.MustCompile(`call i64 (@Box\$Sized\$size\$\w+)\((%Box\$\w+)`).FindAllStringSubmatch(text, -1)
	if len(calls) != 2 {
		t.Fatalf("expected two calls, got %d:\n%s", len(calls), text)
	}
	for _, c := range calls {
		if want, ok := params[c[1]]; !ok || want != c[2] {
			t.Errorf("call to %s passes %s but that function takes %s", c[1], c[2], want)
		}
	}
}

// A field read through the impl's type variable lowers — it previously failed with "field
// access on non-struct type Box<t>".
func TestExec_GenericImplMethodReadsGenericField(t *testing.T) {
	t.Parallel()
	const src = `struct Box<t> { value: t }
trait Get<e> { get: (Self) -> e }
impl Get<t> for Box<t> { get = (self) => self.value }

let main = () -> u8 => {
  let a = Box { value: 7 }
  let b = Box { value: true }
  if b.get() { u8(a.get()) } else { u8(0) }
}
`
	if got := buildAndRun(t, src); got != 7 {
		t.Errorf("exited %d; want 7", got)
	}
}

// A managed type argument is the case the per-specialization ownership analysis exists
// for: the `string` body retains, the `i64` body does not, and the same source line is
// responsible for both.
func TestEmit_GenericImplMethodOwnershipIsPerInstantiation(t *testing.T) {
	t.Parallel()
	src := optUnwrap + `
let main = () -> u8 => {
  let s: Opt<string> = Just "hello"
  let n: Opt<i64> = Just 7
  println(s.unwrap("fallback"))
  u8(n.unwrap(0))
}
`
	ir, err := emitSource(t, src)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	strBody, ok := functionBody(string(ir), "@Opt$Unwrap$unwrap$string")
	if !ok {
		t.Fatal("no string specialization emitted")
	}
	intBody, ok := functionBody(string(ir), "@Opt$Unwrap$unwrap$i64")
	if !ok {
		t.Fatal("no i64 specialization emitted")
	}
	if !strings.Contains(strBody, "lyra_rc_retain") {
		t.Errorf("the string specialization must retain the value it returns:\n%s", strBody)
	}
	if strings.Contains(intBody, "lyra_rc_retain") || strings.Contains(intBody, "lyra_rc_release") {
		t.Errorf("an i64 is not reference-counted; this body should have no rc traffic:\n%s", intBody)
	}
}

// …and it runs correctly, which the emission test alone does not establish.
func TestExec_GenericImplMethodWithManagedPayload(t *testing.T) {
	t.Parallel()
	src := optUnwrap + `
let main = () -> u8 => {
  let s: Opt<string> = Just "hello"
  let e: Opt<string> = Nil
  let a = s.unwrap("fallback")
  let b = e.unwrap("fallback")
  println(a)
  println(b)
  u8(0)
}
`
	out, code := buildAndRunCapture(t, src)
	if code != 0 {
		t.Errorf("exited %d; want 0", code)
	}
	if want := "hello\nfallback\n"; out != want {
		t.Errorf("output %q; want %q", out, want)
	}
}

// functionBody returns the text of one LLVM function definition, so an assertion about a
// specialization cannot accidentally be satisfied by its sibling's instructions.
func functionBody(ir, name string) (string, bool) {
	start := strings.Index(ir, "define ")
	for start >= 0 {
		end := strings.Index(ir[start:], "\n}")
		if end < 0 {
			return "", false
		}
		body := ir[start : start+end+2]
		if strings.Contains(strings.SplitN(body, "(", 2)[0], name) {
			return body, true
		}
		next := strings.Index(ir[start+end:], "define ")
		if next < 0 {
			return "", false
		}
		start = start + end + next
	}
	return "", false
}

// The same program under AddressSanitizer, which is where a wrong answer about ownership
// shows up as a fault rather than as a number. Analyzed generically — one table for every
// instantiation — a returned payload records no retain, and at `t = string` that is a
// double free; at `t = i64` the identical absence is correct. This is the pairing that
// makes the per-specialization table necessary rather than tidy.
func TestExec_GenericImplMethodManagedPayloadASan(t *testing.T) {
	t.Parallel()
	clang, err := exec.LookPath("clang")
	if err != nil {
		t.Skip("clang not found on PATH")
	}
	if !asanAvailable(t, clang) {
		t.Skip("AddressSanitizer not available in this toolchain")
	}
	src := optUnwrap + `
let main = () -> u8 => {
  let s: Opt<string> = Just "hello"
  let e: Opt<string> = Nil
  println(s.unwrap("fallback"))
  println(e.unwrap("fallback"))
  let n: Opt<i64> = Just 7
  u8(n.unwrap(0))
}
`
	if got := buildAndRunASan(t, clang, src); got != 7 {
		t.Errorf("asan: exited %d; want 7", got)
	}
}

// A generic *body* calling a generic impl method — `getOr<t>` calling `o.unwrap(d)` on an
// `Opt<t>` — lowers at each specialization of the body. The dispatch records the impl's
// bindings in the body's vocabulary (`t = t`), and the resolution is composed with the
// body's own bindings both where the specialization set is closed and where the call is
// lowered (Resolution.Composed). This test pinned the refusal until 09/14, when "a body
// emitted at the wrong instantiation" was the risk it guarded; the string case under ASan
// is what shows each specialization got its own body and ownership table.
func TestExec_GenericImplMethodFromGenericBody(t *testing.T) {
	t.Parallel()
	src := optUnwrap + `
let getOr<t> = (o: Opt<t>, d: t) -> t => o.unwrap(d)

let main = () -> u8 => {
  let m: Opt<i64> = Just 7
  let s: Opt<string> = Just ("he" ++ "llo")
  let e: Opt<string> = Nil
  u8(getOr(m, 0) + getOr(s, "x").len() + getOr(e, "fall" ++ "back").len())
}
`
	// 7 + 5 + 8
	if got := buildAndRun(t, src); got != 20 {
		t.Errorf("exited %d; want 20", got)
	}
	if got := buildAndRunASan(t, lookClang(t), src); got != 20 {
		t.Errorf("under ASan: exited %d; want 20", got)
	}
}

// The rest of the generic-body shapes: an operator impl met through the caller's `where`
// bound, a generic impl reached through another generic impl's result, a method generic in
// its own variable, the body itself reached through a second generic function — and a trait
// *default* written in the trait's parameter, which failed even from a concrete call because
// the default's bindings lacked `e`. Managed values throughout, for ASan.
func TestExec_GenericImplShapesFromGenericBodies(t *testing.T) {
	t.Parallel()
	src := `struct Box<t> { v: t }
trait Val<e> { val: (Self) -> e }
impl Val<t> for Box<t> { val = (self) => self.v }
trait Wrap<e> { wrap: (Self) -> Box<Box<e>> }
impl Wrap<t> for Box<t> { wrap = (self) => Box { v: Box { v: self.v } } }
trait Add { (_+_): (Self, Self) -> Self }
impl Add for i64 { (_+_) = (self, o) => self + o }
impl Add for Box<t> where t: Add { (_+_) = (self, o) => Box { v: self.v + o.v } }
trait Mapper { mapv: (Self, (i64) -> b) -> b }
impl Mapper for Box<t> { mapv = (self, f) => f(3) }
let len3 = (n: i64) -> string => if n == 3 { "x" ++ "yz" } else { "" }
trait Twice<e> {
  one: (Self) -> e
  both: (Self) -> (e, e) = (self) => (self.one(), self.one())
}
impl Twice<t> for Box<t> { one = (self) => self.v }
let deep<u> = (b: Box<u>) -> u => b.wrap().v.val()
let add<u> where u: Add = (a: Box<u>, b: Box<u>) -> u => (a + b).v
let viaMap<u, w> = (b: Box<u>, f: (i64) -> w) -> w => b.mapv(f)
let outer<u> = (x: u) -> u => deep(Box { v: x })
let main = () -> u8 => {
  let a = deep(Box { v: "ab" ++ "c" }).len()
  let b = add(Box { v: 40 }, Box { v: 2 })
  let c = viaMap(Box { v: true }, len3).len()
  let d = outer("q" ++ "rs").len() + outer(4)
  let p = Box { v: "de" ++ "f" }.both()
  u8(a + b + c + d + p.0.len() + p.1.len())
}`
	// 3 + 42 + 3 + (3 + 4) + 3 + 3
	const want = 61
	if got := buildAndRun(t, src); got != want {
		t.Errorf("exited %d; want %d", got, want)
	}
	if got := buildAndRunASan(t, lookClang(t), src); got != want {
		t.Errorf("under ASan: exited %d; want %d", got, want)
	}
}

// A `where`-bound call on a generic impl, reached through a second generic: `outer<w>` calls
// `g(Box { v: y })`, and `g<u> where u: Get` calls `x.get()`. The typechecker publishes bound
// candidates where it sees a concrete type, and saw only `u = Box<w>`; the specialization the
// driver composes, `u = Box<i64>`, had no candidate (`no impl of Get for Box$i64`). The driver
// now asks the typechecker to publish for each specialization it discovers — here through
// three generics, an operator bound, an impl's own `where` clause and a trait default.
func TestExec_BoundCallThroughASecondGeneric(t *testing.T) {
	t.Parallel()
	src := `struct Box<t> { v: t }
trait Get { get: (Self) -> string }
impl Get for Box<t> { get = (self) => "g" ++ "et" }
trait Add { (_+_): (Self, Self) -> Self }
impl Add for i64 { (_+_) = (self, o) => self + o }
impl Add for Box<t> where t: Add { (_+_) = (self, o) => Box { v: self.v + o.v } }
trait Named { name: (Self) -> string }
impl Named for i64 { name = (self) => "int" }
impl Named for Box<t> where t: Named { name = (self) => "box " ++ self.v.name() }
trait Twice<e> {
  one: (Self) -> e
  both: (Self) -> (e, e) = (self) => (self.one(), self.one())
}
impl Twice<t> for Box<t> { one = (self) => self.v }
let g<u> where u: Get = (x: u) -> string => x.get()
let mid<m> = (y: m) -> string => g(Box { v: y })
let top<k> = (z: k) -> string => mid(z) ++ mid(#[z])
let sum2<u> where u: Add = (a: u, b: u) -> u => a + b
let viaBox<w> where w: Add = (x: w, y: w) -> w => sum2(Box { v: x }, Box { v: y }).v
let say<u> where u: Named = (x: u) -> string => x.name()
let wrapSay<w> where w: Named = (y: w) -> string => say(Box { v: Box { v: y } })
let pair<u> = (b: Box<u>) -> (u, u) => b.both()
let main = () -> u8 => {
  let a = top(1).len() + top("s" ++ "t").len()
  let b = viaBox(40, 2)
  let c = wrapSay(5).len()
  let p = pair(Box { v: "pq" ++ "r" })
  u8(a + b + c + p.0.len() + p.1.len())
}`
	// (6 + 6) + 42 + len("box box int") 11 + 3 + 3
	const want = 71
	if got := buildAndRun(t, src); got != want {
		t.Errorf("exited %d; want %d", got, want)
	}
	if got := buildAndRunASan(t, lookClang(t), src); got != want {
		t.Errorf("under ASan: exited %d; want %d", got, want)
	}
}
