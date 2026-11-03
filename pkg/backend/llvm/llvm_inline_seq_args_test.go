package llvm

import (
	"strings"
	"testing"
)

// A `gen` call's arguments, where the call is inlined at its consumer. Its parameters are bound
// in a frame of the callee's, so what an argument owes a way out is a call's question, answered
// where that frame takes over. ASan, and on Linux the leak check, are the point.
const inlineSeqLib = `
let one = (n: i64) -> Maybe<i64> => if n > 1 { None } else { Some(n) }
let times = gen (s: own string, n: i64) -> Seq<string> => {
  for i in 0..<n { yield s }
}
struct P { n: i64, m: i64 }
let each = gen (x: ref P, n: i64) -> Seq<i64> => {
  for i in 0..<n { yield x.n }
}
`

func TestExec_InlinedSeqArguments(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, src, want string }{
		{"an `own` argument beside a `?`, in a loop", `
let run = (n: i64) -> Maybe<i64> => {
  var c = 0
  for s in times("a" ++ "${n}", one(n)?) { c += s.len() }
  Some(c)
}
let main = () -> void => { print("${run(1) ?? -1} ${run(5) ?? -1}") }`, "2 -1"},
		{"an `own` argument beside a `?`, as a comprehension's source", `
let run = (n: i64) -> Maybe<[]string> => Some([s in times("a" ++ "${n}", one(n)?) | s])
let main = () -> void => { print("${(run(1) ?? []).len()} ${(run(5) ?? []).len()}") }`, "1 0"},
		{"an `own` argument beside a `return`", `
let run = (n: i64) -> i64 => {
  var c = 0
  for s in times("a" ++ "${n}", if n > 1 { return -1 } else { n }) { c += s.len() }
  c
}
let main = () -> void => { print("${run(1)} ${run(5)}") }`, "2 -1"},
		{"an `own` argument beside a `break`", `
let main = () -> void => {
  var c = 0
  for k in 0..<3 {
    for s in times("a" ++ "${k}", if k > 0 { break } else { 2 }) { c += s.len() }
  }
  print("${c}")
}`, "4"},
		{"a plain function naming the producer", `
let alias = (s: own string, n: i64) -> Seq<string> => times(s, n)
let run = (n: i64) -> Maybe<i64> => {
  var c = 0
  for s in alias("a" ++ "${n}", one(n)?) { c += s.len() }
  Some(c)
}
let main = () -> void => { print("${run(1) ?? -1} ${run(5) ?? -1}") }`, "2 -1"},
		{"a function taking a sequence, inlined for its value", `
let total = (s: own string, xs: Seq<i64>, n: i64) -> i64 => {
  var c = s.len()
  for x in xs { c += x + n }
  c
}
let upto = gen (n: i64) -> Seq<i64> => { for i in 0..<n { yield i } }
let run = (n: i64) -> Maybe<i64> => Some(total("a" ++ "${n}", upto(2), one(n)?))
let main = () -> void => { print("${run(1) ?? -1} ${run(5) ?? -1}") }`, "5 -1"},
		{"an `own` argument into a coroutine", `
let run = (n: i64) -> Maybe<i64> => {
  var s = times("a" ++ "${n}", one(n)?)
  Some((s.next() ?? "").len())
}
let main = () -> void => { print("${run(1) ?? -1} ${run(5) ?? -1}") }`, "2 -1"},
		{"a coroutine as a sequence argument beside a `?`", `
let drain = (xs: Seq<string>, n: i64) -> i64 => {
  var c = n
  for x in xs { c += x.len() }
  c
}
let run = (n: i64) -> Maybe<i64> => {
  var c = 0
  for x in chain(times("a" ++ "${n}", 1), one(n)?) { c += x.len() }
  Some(c)
}
let chain = gen (xs: Seq<string>, n: i64) -> Seq<string> => {
  var rest = xs
  yield rest.next() ?? ""
}
let main = () -> void => { print("${run(1) ?? -1} ${run(5) ?? -1}") }`, "2 -1"},
		{"a `return` in the consumer releases the argument once", `
let run = () -> i64 => {
  for s in times("a" ++ "${1}", 3) { return s.len() }
  0
}
let main = () -> void => { print("${run()}") }`, "2"},
		{"a `ref` argument's index runs in its turn", `
var trail = ""
let at = (n: i64) -> i64 => { trail = trail ++ "a"; n }
let later = () -> i64 => { trail = trail ++ "b"; 1 }
let main = () -> void => {
  var ps: [2]P = #[P { n: 1, m: 0 }, P { n: 2, m: 0 }]
  for v in each(ps[at(1)], later()) { print("${v} ${trail}") }
}`, "2 ab"},
		{"a local `ref` argument sees the consumer's writes", `
let main = () -> void => {
  var p = P { n: 1, m: 0 }
  var seen = ""
  for v in each(p, 2) {
    seen = seen ++ "${v},"
    p = P { n: v + 1, m: 0 }
  }
  print(seen)
}`, "1,2,"},
		{"a `ref` argument that is a temporary", `
let make = () -> P => P { n: 9, m: 0 }
let main = () -> void => { print("${[v in each(make(), 2) | "${v}"].join(",")}") }`, "9,9"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := strings.TrimSpace(buildAndRunASanStdout(t, inlineSeqLib+c.src)); got != c.want {
				t.Errorf("got %q; want %q", got, c.want)
			}
		})
	}
}

// A sequence held as a value outlives the call that made it, so a place lent to it would too:
// refused, as `mut` is. Used to be a crash on a managed `ref` and invalid IR on any other.
func TestEmit_SeqValueTakesNoByRefParameter(t *testing.T) {
	t.Parallel()
	for _, ty := range []string{"string", "P"} {
		_, err := emitSource(t, `
struct P { n: i64, m: i64 }
let each = gen (x: ref `+ty+`, n: i64) -> Seq<i64> => { for i in 0..<n { yield n } }
let hold = (x: ref `+ty+`) -> u8 => {
  var s = each(x, 2)
  u8(s.next() ?? 0)
}
let main = () -> u8 => 0`)
		if err == nil || !strings.Contains(err.Error(), "takes a `ref` parameter") {
			t.Errorf("%s: expected a refusal, got %v", ty, err)
		}
	}
}
