package llvm

import (
	"strings"
	"testing"
)

// `?.` and `?[` — safe navigation. The collector desugars each into a match over the
// Maybe (postfix_expr.go, optionalChain) whose Some arm wraps the access in the
// prelude's `__optional_chain`, so these are tests of that desugaring end to end: the
// flattening rule, laziness, chains, and ownership of what passes through.
//
// Until 09/29 both forms parsed, collected with a flag nothing read, and failed: the
// typechecker treated `?.` as `.` ("member access on non-struct type Maybe") and the
// backend refused `?.`/`?[` by name.

const chainTypes = `struct Pet { name: string, age: Maybe<i64>, toys: []string }
struct Person { pet: Maybe<Pet> }

let rex = () -> Person => Person { pet: Some(Pet { name: "Rex", age: Some(3), toys: ["ball", "rope"] }) }
let nobody = () -> Person => Person { pet: None }
`

func TestExec_OptionalChain(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		main string
		want string
	}{
		// A plain field comes out as Some; None passes through untouched.
		{"field", `println(rex().pet?.name ?? "-")
  println(nobody().pet?.name ?? "-")`, "Rex\n-\n"},
		// A field that is already a Maybe is not wrapped again: `?.age` is a Maybe<i64>,
		// which `??` unwraps with an i64 default.
		{"optional field flattens", `println(rex().pet?.age ?? -1)
  println(nobody().pet?.age ?? -1)`, "3\n-1\n"},
		// Each `?.` reads through one Maybe, so a chain says it at every link.
		{"chain", `let m: Maybe<Person> = Some(rex())
  println(m?.pet?.name?.to_ascii_upper() ?? "-")
  let none: Maybe<Person> = None
  println(none?.pet?.name ?? "-")`, "REX\n-\n"},
		{"method call", `println(rex().pet?.toys?.len() ?? 0)
  println(nobody().pet?.toys?.len() ?? 0)`, "2\n0\n"},
		{"index", `println(rex().pet?.toys?[1] ?? "-")
  let none: Maybe<[]string> = None
  println(none?[7] ?? "not read")`, "rope\nnot read\n"},
		// A lambda argument inside the Some arm, and a result type the call chooses.
		{"method with a lambda", `let words: Maybe<[]string> = Some(["a", "bcd"])
  let lens = words?.map((w) => w.len())
  println(lens?[1] ?? 0)`, "3\n"},
		// Inferred without an annotation, and usable as an ordinary Maybe.
		{"as a value", `let name = rex().pet?.name
  match name {
    Some(n) => println("got ${n}"),
    None => println("none"),
  }`, "got Rex\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			src := chainTypes + "let main = () -> void => {\n  " + c.main + "\n}\n"
			if got := buildAndRunWithPrelude(t, src, ""); got != c.want {
				t.Errorf("printed %q; want %q\n%s", got, c.want, src)
			}
		})
	}
}

// A method's arguments are inside the Some arm: on None they are never evaluated, as a
// reader of `m?.f(g())` expects when `m` is empty.
func TestExec_OptionalChainArgumentsAreLazy(t *testing.T) {
	t.Parallel()
	src := `let loud = (s: string) -> string => {
  println("evaluated ${s}")
  s
}

let main = () -> void => {
  let some: Maybe<string> = Some("ab")
  let none: Maybe<string> = None
  println(some?.starts_with(loud("a")) ?? false)
  println(none?.starts_with(loud("b")) ?? false)
}
`
	want := "evaluated a\ntrue\nfalse\n"
	if got := buildAndRunWithPrelude(t, src, ""); got != want {
		t.Errorf("printed %q; want %q", got, want)
	}
}

// Managed values through every path: a string field out of a shared-buffer struct, a
// `[]string` element, a flattened Maybe<string>, and a None. ASan plus the harness's
// retain/release accounting check the desugared match releases what it binds.
func TestASan_OptionalChainManagedPayloads(t *testing.T) {
	t.Parallel()
	src := chainTypes + `struct Box { label: Maybe<string> }

let main = () -> u8 => {
  var total = 0
  for i in 0..<50 {
    let p = if i % 2 == 0 { rex() } else { nobody() }
    total += (p.pet?.name ?? "").len()
    total += (p.pet?.toys?[0] ?? "").len()
    let b: Maybe<Box> = Some(Box { label: Some("x${i}") })
    total += (b?.label ?? "").len()
  }
  if total == 25 * 3 + 25 * 4 + 10 * 2 + 40 * 3 { 0 } else { 1 }
}
`
	if code := buildAndRunASanWithPrelude(t, src); code != 0 {
		t.Errorf("exited %d; want 0", code)
	}
}

// On anything but a Maybe there is nothing to read through: one error (lyra-E083) in the
// program's own terms, and nothing about the `Some`/`None` arms or the binding the
// desugaring wrote.
func TestCheck_OptionalChainOnANonMaybe(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, expr, want string
	}{
		{"a struct", `p?.name`, "this is a Pet, which is always there: write `.` or `[`"},
		{"an array", `xs?[0]`, "this is a DynamicArray<i64>, which is always there"},
		{"a result", `r?.name`, "convert it with `.ok()`, or propagate the error with `?` first"},
		{"a plain field of a Maybe", `p.name?.len()`, "this is a string"},
		// The other mistake: a chain that drops a `?`, so `.len()` meets a Maybe.
		{"a link without its ?", `m?.name.len()`, "`?.` reads through a Maybe (each link of a chain needs its own)"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			src := `struct Pet { name: string }

let main = () -> void => {
  let p = Pet { name: "Rex" }
  let xs = [1, 2]
  let r: Result<Pet, string> = Ok(p)
  let m: Maybe<Pet> = Some(p)
  let v = ` + c.expr + `
  println(xs.len())
}
`
			errs := checkWithPrelude(t, src)
			if len(errs) != 1 || !strings.Contains(errs[0], c.want) {
				t.Errorf("errors %q; want exactly one containing %q", errs, c.want)
			}
		})
	}
}
