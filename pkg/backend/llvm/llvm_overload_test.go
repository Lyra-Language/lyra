package llvm

import (
	"strings"
	"testing"
)

// Receiver-keyed overloading, lowered.
//
// The front end picks the member; the backend's job is to emit each one under a symbol of
// its own and to send a call to the one that was picked. Both halves fail *silently* if
// they are wrong — a shared symbol is a clang redefinition error at best and the wrong
// body at worst, and a call resolved by name would reach whichever member was declared
// last — so these run the program and check the value, rather than reading the IR.

const overloadedUnwrap = `
data Maybe<t> = None | Some t
data Result<t, e> = Ok(t) | Err(e)

let unwrap_or<t> = (self: Maybe<t>, fallback: t) -> t => match self {
  Some v => v,
  None => fallback,
}

let unwrap_or<t, e> = (self: Result<t, e>, fallback: t) -> t => match self {
  Ok v => v,
  Err _ => fallback,
}
`

// Each overload is reached from a receiver of its own type, and each returns a different
// value, so picking the wrong member changes the exit code rather than merely the path.
// 7 from the Maybe, 9 from the Result.
func TestExec_OverloadDispatchesOnReceiver(t *testing.T) {
	t.Parallel()
	got := buildAndRun(t, overloadedUnwrap+`
let main = () -> u8 => {
  let m: Maybe<i64> = Some 7
  let r: Result<i64, string> = Ok 9
  u8(m.unwrap_or(0) + r.unwrap_or(0))
}
`)
	if got != 16 {
		t.Errorf("expected 16 (7 from the Maybe overload + 9 from the Result one), got %d", got)
	}
}

// The fallback arms, which run the *other* branch of each member's body — a member picked
// correctly for the `Some` case could still be wrong for `None` if the two bodies were
// somehow sharing an emitted function. 3 + 4.
func TestExec_OverloadFallbackArms(t *testing.T) {
	t.Parallel()
	got := buildAndRun(t, overloadedUnwrap+`
let main = () -> u8 => {
  let m: Maybe<i64> = None
  let r: Result<i64, string> = Err "no"
  u8(m.unwrap_or(3) + r.unwrap_or(4))
}
`)
	if got != 7 {
		t.Errorf("expected 7 from the two fallbacks, got %d", got)
	}
}

// Written as plain calls rather than method-style. The receiver is argument 0 either way,
// so this must lower to exactly the same two functions.
func TestExec_OverloadCallForm(t *testing.T) {
	t.Parallel()
	got := buildAndRun(t, overloadedUnwrap+`
let main = () -> u8 => {
  let m: Maybe<i64> = Some 5
  let r: Result<i64, string> = Ok 6
  u8(unwrap_or(m, 0) + unwrap_or(r, 0))
}
`)
	if got != 11 {
		t.Errorf("expected 11, got %d", got)
	}
}

// Non-generic overloads, which are emitted as themselves rather than as specializations —
// the path where two members would otherwise take the *same* module-qualified symbol.
// The distinct `$head` suffix is what keeps them apart, so this pins the symbols too.
func TestExec_OverloadDistinctSymbols(t *testing.T) {
	t.Parallel()
	src := `
struct Cat { n: i64 }
struct Dog { n: i64 }

let speak = (self: Cat) -> i64 => 1
let speak = (self: Dog) -> i64 => 2

let main = () -> u8 => {
  let c = Cat { n: 0 }
  let d = Dog { n: 0 }
  u8(c.speak() * 10 + d.speak())
}
`
	if got := buildAndRun(t, src); got != 12 {
		t.Errorf("expected 12 (Cat=1 then Dog=2), got %d", got)
	}
	ir, err := emitSource(t, src)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	for _, want := range []string{"@lyra.speak$Cat", "@lyra.speak$Dog"} {
		if !strings.Contains(ir, want) {
			t.Errorf("expected the emitted module to define %s\n%s", want, ir)
		}
	}
}

// An overload that panics: each member traps with its own message, so this pins that the
// call reached the member the receiver selected and not merely *a* member with the right
// signature. The two bodies are otherwise identical, which is the point — a wrong
// resolution would still compile, still trap, and still exit 101, and only the message
// distinguishes them.
func TestExec_OverloadPanicsFromTheSelectedMember(t *testing.T) {
	t.Parallel()
	const decls = `
data Maybe<t> = None | Some t
data Result<t, e> = Ok(t) | Err(e)

let unwrap<t> = (self: Maybe<t>) -> t => match self {
  Some v => v,
  None => panic("unwrap on a None"),
}

let unwrap<t, e> = (self: Result<t, e>) -> t => match self {
  Ok v => v,
  Err _ => panic("unwrap on an Err"),
}
`
	cases := []struct {
		name, main, want string
	}{
		{"maybe", `let m: Maybe<i64> = None
  u8(m.unwrap())`, "lyra: panic: unwrap on a None\n"},
		{"result", `let r: Result<i64, string> = Err "boom"
  u8(r.unwrap())`, "lyra: panic: unwrap on an Err\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			stderr, code := buildAndRunPanic(t, decls+"\nlet main = () -> u8 => {\n  "+c.main+"\n}")
			if code != trapExitCode {
				t.Errorf("exit code: got %d, want %d", code, trapExitCode)
			}
			if stderr != c.want {
				t.Errorf("stderr: got %q, want %q", stderr, c.want)
			}
		})
	}
}

// `expect` takes its message from the caller, which is the reason it exists beside
// `unwrap`: the message can name the concrete situation, including values interpolated at
// the call site. Interpolating inside the combinator is what is *not* expressible — the
// receiver's payload is a type variable, and only the printable primitives have a
// formatter — so this pins the shape that works rather than the one that does not.
func TestExec_ExpectCarriesTheCallersMessage(t *testing.T) {
	t.Parallel()
	stderr, code := buildAndRunPanic(t, `
data Maybe<t> = None | Some t

let expect<t> = (self: Maybe<t>, msg: string) -> t => match self {
  Some v => v,
  None => panic(msg),
}

let main = () -> u8 => {
  let name = "database"
  let port = 5432
  let m: Maybe<i64> = None
  u8(m.expect("config for ${name} on port ${port} is required"))
}`)
	if code != trapExitCode {
		t.Errorf("exit code: got %d, want %d", code, trapExitCode)
	}
	if want := "lyra: panic: config for database on port 5432 is required\n"; stderr != want {
		t.Errorf("stderr: got %q, want %q", stderr, want)
	}
}

// **Two generic overloads of one name must not share a specialization.**
//
// A specialization was keyed by name plus bindings, which stopped identifying a function
// the moment one name could mean several: `map<t=i64,u=i64>` named both the `Maybe`
// overload and the array one, so they collapsed onto a single emitted function and a call
// to one reached the other. It surfaced as an invalid GEP against the wrong type — in the
// emitted IR, not anywhere a diagnostic could reach — and only when a program used *both*
// at the same type arguments, which is why neither overload's own tests caught it.
//
// The two here are deliberately the same generic shape at the same bindings; only the
// receiver differs.
func TestExec_GenericOverloadsDoNotShareASpecialization(t *testing.T) {
	t.Parallel()
	src := `
data Maybe<t> = None | Some t

let convert<t,u> = (self: Maybe<t>, f: (t) -> u, fallback: u) -> u => match self {
  Some v => f(v),
  None => fallback,
}

let convert<t,u> = (self: []t, f: (t) -> u, fallback: u) -> u => fallback

let main = () -> u8 => {
  let m: Maybe<i64> = Some 7
  let xs: []i64 = [1, 2]
  u8(m.convert((x: i64) -> i64 => x, 0) + xs.convert((x: i64) -> i64 => x, 5))
}`
	if got := buildAndRun(t, src); got != 12 {
		t.Errorf("expected 12 (7 through the Maybe overload + 5 through the array one), got %d", got)
	}
	ir, err := emitSource(t, src)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	// Same name, same bindings, different receiver head — so two emitted functions.
	if n := strings.Count(ir, "define "); n < 2 {
		t.Fatalf("expected several emitted functions, got %d", n)
	}
	if !strings.Contains(ir, "convert$Maybe$i64$i64") {
		t.Errorf("expected the Maybe overload to carry its receiver head in its symbol:\n%s", ir)
	}
	if strings.Count(ir, "@lyra.convert") < 2 {
		t.Errorf("expected two distinct convert specializations:\n%s", ir)
	}
}
