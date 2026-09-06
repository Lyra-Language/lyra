package llvm

import (
	"strings"
	"testing"
)

// Return-type-driven inference, end to end — the front end solving it is only half the
// claim, since a variable left unbound reaches the backend as a type with no
// representation and the failure would be a lowering crash rather than a type error.
func TestExec_ReturnTypeInference(t *testing.T) {
	t.Parallel()
	cases := []struct{ name, src, want string }{
		{
			name: "a generic constructor solved from its annotation",
			src: `
struct HashMap<k, v> { slots: []Maybe<Entry<k, v>>, count: i64 }
struct Entry<k, v> { key: k, value: v }
let with_capacity<k,v> = pure (cap: i64) -> HashMap<k,v> =>
  HashMap { slots: [None; cap], count: 0 }
let main = () -> void => {
  var m: HashMap<string, i64> = with_capacity(8)
  m.slots[3] = Some(Entry { key: "a", value: 7 })
  match m.slots[3] { Some(e) => println("${e.key}=${e.value}"), None => println("-") }
  println(m.slots.len())
}`,
			want: "a=7\n8",
		},
		{
			// The instantiation the *context* chose has to be the one that lowers: a
			// u8 element here rather than the i64 an unconstrained solve would default
			// to, which is visible because 300 does not fit a u8.
			name: "the context's width is the one lowered",
			src: `
let empty<t> = pure () -> []t => []
let main = () -> void => {
  var xs: []u8 = empty()
  xs.push(200)
  xs.push(7)
  println("${xs[0]} ${xs[1]} ${xs.len()}")
}`,
			want: "200 7 2",
		},
		{
			name: "solved from a declared return type",
			src: `
let empty<t> = pure () -> []t => []
let make = pure () -> []string => empty()
let main = () -> void => {
  var xs = make()
  xs.push("hi")
  println(xs.join("-"))
}`,
			want: "hi",
		},
		{
			name: "solved from the parameter slot it is passed into",
			src: `
let empty<t> = pure () -> []t => []
let count = pure (xs: []i64) -> i64 => xs.len()
let main = () -> void => { println(count(empty())) }`,
			want: "0",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := strings.TrimSpace(buildAndRunWithPrelude(t, c.src, "")); got != c.want {
				t.Errorf("got %q; want %q", got, c.want)
			}
		})
	}
}
