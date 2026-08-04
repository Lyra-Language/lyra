package llvm

import (
	"strings"
	"testing"
)

// A generic function calling *another* generic at a variable-dependent instantiation —
// `unwrap<t>` defined as `expect(self, "…")`.
//
// The typechecker records that inner call as `expect<t=t>`, where the right-hand `t` is
// the *caller's* variable. That is a template, not a specialization, and the monomorphizer
// used to reject it ("type variable t has no concrete type here") rather than guess. The
// driver now composes each template against every specialization that reaches it, so
// lowering `unwrap<t=i64>` finds `expect<t=i64>` already emitted.
//
// These run the program rather than reading IR: the failure mode being guarded against is
// a call reaching the wrong specialization, which produces a well-formed module that
// computes the wrong thing.

const chainDecls = `
data Maybe<t> = None | Some t

let expect<t> = (self: Maybe<t>, msg: string) -> t => match self {
  Some v => v,
  None => panic(msg),
}

let unwrap<t> = (self: Maybe<t>) -> t => expect(self, "unwrap on a None")
`

func TestExec_GenericCallsGenericMethodForm(t *testing.T) {
	t.Parallel()
	got := buildAndRun(t, chainDecls+`
let main = () -> u8 => {
  let m: Maybe<i64> = Some 7
  u8(m.unwrap())
}`)
	if got != 7 {
		t.Errorf("expected 7, got %d", got)
	}
}

// The free-function spelling of the same thing, which todo.md listed alongside the method
// form as equally refused.
func TestExec_GenericCallsGenericCallForm(t *testing.T) {
	t.Parallel()
	got := buildAndRun(t, `
data Opt<t> = Nil | One(t)

let unwrap_or<t> = (self: Opt<t>, fallback: t) -> t => match self {
  One v => v,
  Nil => fallback,
}

let get_or<t> = (o: Opt<t>, d: t) -> t => unwrap_or(o, d)

let main = () -> u8 => {
  let a: Opt<i64> = One 5
  let b: Opt<i64> = Nil
  u8(get_or(a, 0) + get_or(b, 3))
}`)
	if got != 8 {
		t.Errorf("expected 8 (5 from One, 3 from the Nil fallback), got %d", got)
	}
}

// **Two instantiations of one chain.** Each must get its own specialization the whole way
// down — if the composition collapsed them, both calls would run the same emitted body and
// the narrower one would compute at the wrong width.
func TestExec_GenericChainSpecializesPerType(t *testing.T) {
	t.Parallel()
	src := chainDecls + `
let main = () -> u8 => {
  let a: Maybe<i64> = Some 300
  let b: Maybe<u8> = Some 44
  u8(a.unwrap() - 256 + i64(b.unwrap()))
}`
	if got := buildAndRun(t, src); got != 88 {
		t.Errorf("expected 88 (300-256 + 44), got %d", got)
	}
	ir, err := emitSource(t, src)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	// The inner callee is specialized per type even though no call site in the program
	// names either one — they exist only by composition.
	for _, want := range []string{"expect$i64", "expect$u8"} {
		if !strings.Contains(ir, want) {
			t.Errorf("expected a composed specialization %s in the module", want)
		}
	}
}

// A **managed** payload through the composed specialization. This is the case the closure
// had to run before the ownership pass rather than after: a specialization discovered too
// late would fall back to the program-wide ownership table, which is analyzed generically
// — where a type variable is not reference-counted — so a `string` body would emit neither
// retains nor releases. Run under the leak checker (`./asan.sh`) this is also the case
// that would show a double free.
func TestExec_GenericChainWithManagedPayload(t *testing.T) {
	t.Parallel()
	got := buildAndRun(t, `
data Maybe<t> = None | Some t

let expect<t> = (self: Maybe<t>, msg: string) -> t => match self {
  Some v => v,
  None => panic(msg),
}

let unwrap<t> = (self: Maybe<t>) -> t => expect(self, "unwrap on a None")

let size = (s: string) -> i64 => 4

let main = () -> u8 => {
  let s: Maybe<string> = Some ("ab" ++ "cd")
  u8(size(s.unwrap()))
}`)
	if got != 4 {
		t.Errorf("expected 4, got %d", got)
	}
}

// Three deep, so the closure has to reach a specialization through another discovered one
// rather than only through a call site the program wrote.
func TestExec_GenericChainIsTransitive(t *testing.T) {
	t.Parallel()
	got := buildAndRun(t, `
data Maybe<t> = None | Some t

let base<t> = (self: Maybe<t>, fallback: t) -> t => match self {
  Some v => v,
  None => fallback,
}

let middle<t> = (self: Maybe<t>, fallback: t) -> t => base(self, fallback)
let outer<t> = (self: Maybe<t>, fallback: t) -> t => middle(self, fallback)

let main = () -> u8 => {
  let m: Maybe<i64> = Some 9
  let n: Maybe<i64> = None
  u8(outer(m, 0) + outer(n, 6))
}`)
	if got != 15 {
		t.Errorf("expected 15 (9 + 6), got %d", got)
	}
}
