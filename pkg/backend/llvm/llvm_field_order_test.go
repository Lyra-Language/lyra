package llvm

import (
	"strings"
	"testing"
)

// A literal's fields are evaluated as written, whatever order the type declares them in.
func TestExec_LiteralFieldsEvaluateAsWritten(t *testing.T) {
	t.Parallel()
	const lib = `
struct S { x: i64, y: i64, z: i64 = say(9) }
struct P { x: i64, y: i64 }
data D = Rec { x: i64, y: i64 } | Dot
let say = (n: i64) -> i64 => {
  print("${n} ")
  n
}
let base = () -> P => P { x: say(1), y: say(2) }
`
	cases := []struct{ name, src, want string }{
		{"a struct", `
let main = () -> void => {
  let t = P { y: say(2), x: say(1) }
  print("= ${t.x * 10 + t.y}")
}`, "2 1 = 12"},
		{"a struct in declared order", `
let main = () -> void => {
  let t = P { x: say(1), y: say(2) }
  print("= ${t.x * 10 + t.y}")
}`, "1 2 = 12"},
		{"a positional struct", `
let main = () -> void => {
  let t = P { say(1), say(2) }
  print("= ${t.x * 10 + t.y}")
}`, "1 2 = 12"},
		{"an anonymous struct", `
let main = () -> void => {
  let t = { y: say(2), x: say(1) }
  print("= ${t.x * 10 + t.y}")
}`, "2 1 = 12"},
		{"an anonymous struct whose annotation orders it the other way", `
let main = () -> void => {
  let t: { x: i64, y: i64 } = { y: say(2), x: say(1) }
  print("= ${t.x * 10 + t.y}")
}`, "2 1 = 12"},
		{"a data constructor's record", `
let main = () -> void => {
  let t = Rec { y: say(2), x: say(1) }
  print(match t {
    Rec { x, y } => "= ${x * 10 + y}",
    Dot => "dot",
  })
}`, "2 1 = 12"},
		{"a filled-in default, after what is written", `
let main = () -> void => {
  let t = S { y: say(2), x: say(1) }
  print("= ${t.x * 100 + t.y * 10 + t.z}")
}`, "2 1 9 = 129"},
		{"a record update, the base first", `
let main = () -> void => {
  let t = P { base() | y: say(7) }
  print("= ${t.x * 10 + t.y}")
}`, "1 2 7 = 17"},
		{"a shared struct", `
let main = () -> void => {
  let t: shared P = P { y: say(2), x: say(1) }
  print("= ${t.x * 10 + t.y}")
}`, "2 1 = 12"},
		{"nested, each its own", `
struct Outer { a: P, b: P }
let main = () -> void => {
  let t = Outer { b: P { y: say(4), x: say(3) }, a: P { y: say(2), x: say(1) } }
  print("= ${t.a.x + t.b.y}")
}`, "4 3 2 1 = 5"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := strings.TrimSpace(buildAndRunASanStdout(t, lib+c.src)); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

// Every pass walks a literal's fields as written, so the backend evaluating them in another
// order made their answers wrong. Each of these went wrong in safe code while it did, and the
// names say which way round the declaration has them.
func TestExec_LiteralFieldOrderAgreesWithThePasses(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, src, want string }{
		{"a bounds check dropped for a write written first, declared after", `
struct R { x: i64, y: i64 }
let main = () -> void => {
  let xs: [3]i64 = #[1, 2, 3]
  var i: i64 = 3
  let t = R { y: {
    i = 0
    1
  }, x: xs[i] }
  print("${t.x}")
}`, "1"},
		{"a value freed by an `own` call written last, declared first", `
struct U { y: i64, x: string }
let take = (s: own string) -> i64 => s.byte_len()
let main = () -> void => {
  let s = "a" ++ "b${1}"
  let t = U { x: s ++ "!", y: take(s) }
  print("${t.x} ${t.y}")
}`, "ab1! 3"},
		{"a kept field beside a later field reassigning the base", `
struct T { x: string, y: string }
let main = () -> void => {
  var s = T { x: "o" ++ "${1}", y: "p" ++ "${2}" }
  let t = T { s | y: {
    s = T { x: "n" ++ "${3}", y: "m" ++ "${4}" }
    "v" ++ "${5}"
  } }
  print("${t.x} ${t.y} ${s.x}")
}`, "o1 v5 n3"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := strings.TrimSpace(buildAndRunASanStdout(t, c.src)); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}
