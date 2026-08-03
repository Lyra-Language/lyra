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
// `Opt<t>` — is refused, loudly, rather than mis-specialized. Substitutions are not
// composed (see monomorphize.go), so the callee's bindings would map t to t, and the same
// refusal already governs a generic free function calling another one. Pinned because the
// two must stay in step: the failure mode of getting this wrong is a body emitted at the
// wrong instantiation, which is silent.
func TestEmit_GenericImplMethodFromGenericBodyIsRefused(t *testing.T) {
	t.Parallel()
	src := optUnwrap + `
let getOr<t> = (o: Opt<t>, d: t) -> t => o.unwrap(d)

let main = () -> u8 => {
  let m: Opt<i64> = Just 7
  u8(getOr(m, 0))
}
`
	_, err := emitSource(t, src)
	if err == nil {
		t.Fatal("expected a refusal: the callee's bindings are variable-dependent")
	}
	if !strings.Contains(err.Error(), "no concrete type here") {
		t.Errorf("expected the uninstantiated-generic error, got %v", err)
	}
}
