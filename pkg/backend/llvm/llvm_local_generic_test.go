package llvm

import (
	"strings"
	"testing"
)

// A generic declared inside a function — `let idf<t> = (a: t) -> t => a` in `main` — is
// emitted as one closure per instantiation (local_generic.go). Until 09/13 it was lowered as
// an ordinary closure at its unsolved type variables, and every program declaring one
// checked clean and failed to build with "type variable t has no concrete type here".
//
// Run under LeakSanitizer as well where it exists: each instantiation is a closure value in
// a framed slot, and a captured `string` is retained into each environment.
func TestExec_LocalGenerics(t *testing.T) {
	t.Parallel()
	src := `
let pick<t> = pure (a: t, b: t) -> t => b
let first_of = (n: i64) -> i64 => {
  let first<t> = (xs: []t) -> t => xs[0]
  first([n, 2]) + first([3])
}
let main = () -> u8 => {
  let k = 10
  let tag = "#" ++ "!"
  let idf<t> = (a: t) -> t => a
  let show_k<t> where t: Show = (a: t) -> string => "${a}+${k}${tag}"
  let twice<t> = (x: t) -> t => pick(x, x)
  let bracket<t> where t: Show = (x: t) -> string => {
    let inner = (s: string) -> string => "[" ++ s ++ "]"
    inner("${twice(x)}")
  }
  let unused<t> = (x: t) -> t => x
  let via_closure = () -> i64 => idf(4)
  let parts: []string = [
    "${idf(5)}", idf("s" ++ "t"), show_k(1), show_k("x" ++ "y"),
    "${twice(7)}", bracket(3), bracket("z" ++ "z"), "${via_closure()}",
    "${idf::<u8>(200)}", "${first_of(1)}",
  ]
  println(parts.join(" "))
  if parts.join(" ") == "5 st 1+10#! xy+10#! 7 [3] [zz] 4 200 4" { 3 } else { 1 }
}`
	if got := buildAndRunASanWithPrelude(t, src); got != 3 {
		out := buildAndRunWithPrelude(t, src, "")
		t.Errorf("exited %d; want 3 — printed %q", got, strings.TrimSpace(out))
	}
}

// The two shapes refused until the day after they were first lowered. A local generic inside
// a *generic* function carries the enclosing function's variables in its instantiation, so
// `outer<i64>` and `outer<string>` each get their own; and a local generic that captures a
// binding is captured, by a lambda calling it, as the closure values its declaration built.
//
// The `var` is the semantic point of the second: `add` captured `k` when it was declared, so
// every call sees 1 — directly or through `call` — exactly as an ordinary closure would, and
// the plain closure created later sees the later value.
func TestExec_LocalGenericsInsideGenericsAndCaptured(t *testing.T) {
	t.Parallel()
	src := `
let outer<u> where u: Show = (v: u, n: i64) -> string => {
  let tag = "<" ++ "${v}" ++ ">"
  let idf<t> = (a: t) -> t => a
  let both<t> where t: Show = (a: t, b: u) -> string => "${a}/${b}${tag}"
  let twice<t> where t: Show = (a: t) -> string => both(a, v) ++ both(idf(n), idf(v))
  twice("x" ++ "y")
}
let main = () -> u8 => {
  var k = 1
  let add<t> where t: Show = (a: t) -> string => "${a}${k}"
  k = 5
  let call = () -> string => add(2) ++ add("s")
  k = 9
  let plain = (a: i64) -> string => "${a}${k}"
  let nested = () -> string => { let inner = () -> string => call() ++ plain(4); inner() }
  let line = outer(3, 1) ++ " " ++ outer("s" ++ "t", 2) ++ " " ++ nested() ++ " " ++ add(3)
  println(line)
  if line == "xy/3<3>1/3<3> xy/st<st>2/st<st> 21s149 31" { 3 } else { 1 }
}`
	if got := buildAndRunASanWithPrelude(t, src); got != 3 {
		out := buildAndRunWithPrelude(t, src, "")
		t.Errorf("exited %d; want 3 — printed %q", got, strings.TrimSpace(out))
	}
}

// A generic declared inside a local generic, mentioning the middle one's variable and — in a
// generic function — the outermost one's too. Its instantiation binds every enclosing
// generic's variables, and it is lifted per its own instantiations rather than with the
// middle one's lambdas. Before 09/13 lyra-E031 refused the `m`.
func TestExec_LocalGenericInsideALocalGeneric(t *testing.T) {
	t.Parallel()
	src := `
let outer<u> where u: Show = (v: u) -> string => {
  let mid<m> where m: Show = (a: m) -> string => {
    let inner<t> where t: Show = (x: t, y: m, z: u) -> string => "${x}${y}${z}"
    inner(1, a, v) ++ inner("s", a, v)
  }
  mid(2) ++ mid("q")
}
let main = () -> u8 => {
  let k = "k" ++ "!"
  let mid<m> where m: Show = (a: m) -> string => {
    let inner<t> where t: Show = (x: t, y: m) -> string => "${x}${y}${k}"
    let call = () -> string => inner(0, a)
    inner(1, a) ++ call()
  }
  let line = mid(7) ++ " " ++ mid("w") ++ " " ++ outer(3) ++ " " ++ outer("z")
  println(line)
  if line == "17k!07k! 1wk!0wk! 123s231q3sq3 12zs2z1qzsqz" { 3 } else { 1 }
}`
	if got := buildAndRunASanWithPrelude(t, src); got != 3 {
		out := buildAndRunWithPrelude(t, src, "")
		t.Errorf("exited %d; want 3 — printed %q", got, strings.TrimSpace(out))
	}
}

// A closure capturing another closure. It could not be done at all before: a function value
// had no size, so "cannot size captured binding f" — found when a lambda first had to capture
// a local generic's closures.
func TestExec_AClosureCapturesAClosure(t *testing.T) {
	t.Parallel()
	src := `
let main = () -> u8 => {
  let s = "a" ++ "b"
  let f = (x: i64) -> string => "${x}" ++ s
  let g = () -> string => f(1) ++ f(2)
  let h = () -> string => g() ++ "!"
  if h() == "1ab2ab!" { 3 } else { 1 }
}`
	if got := buildAndRunASanWithPrelude(t, src); got != 3 {
		t.Errorf("exited %d; want 3", got)
	}
}
