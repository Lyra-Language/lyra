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

// The two shapes still refused, each by name rather than by "type variable t has no
// concrete type here": a local generic inside a *generic* function, whose body could mention
// both functions' variables while an instantiation carries only its own; and a local
// generic that captures a binding, called from inside another lambda.
func TestEmit_LocalGenericRefusals(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ name, src, want string }{
		{"inside a generic function", `
let outer<u> = (v: u) -> u => {
  let idf<t> = (a: t) -> t => a
  idf(v)
}
let main = () -> void => { println(outer(3)) }`, `a generic declared inside the generic function "outer"`},
		{"a capturing one called from a lambda", `
let main = () -> void => {
  let k = 1
  let add<t> where t: Show = (a: t) -> string => "${a}${k}"
  let call = () -> string => add(2)
  println(call())
}`, `"add" is a generic declared inside "main" that captures a binding`},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			_, err := emitWithPreludeErr(t, c.src)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("want an error containing %q, got %v", c.want, err)
			}
		})
	}
}
