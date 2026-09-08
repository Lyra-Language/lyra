package llvm

import (
	"strings"
	"testing"
)

// A generic type's bare declaration joined with one of its instantiations settles on
// the instantiation, at every site that joins branch types.
//
// `Some(e.value)` infers to `Maybe<v>` and `None` to the bare `Maybe` — a nullary
// constructor solves nothing — and the two are assignable both ways, so the join used to
// answer whichever came second: `match … { Some(e) => Some(e.value), None => None }` with
// no annotation type-checked as the bare declaration and died in the backend with
// `unknown named type "Maybe"`, while the arms swapped compiled. The join now prefers
// the instantiation (`branchCommonType`) and each site pushes it back onto the arm that
// contributed the bare one, which is what an annotation would have done. One test per
// site, each inside a *generic* body, because a concrete body hid it — there the bare
// `Maybe` had a default to fall back on.
func TestExec_BareDeclarationJoinsToTheInstantiation(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ name, body string }{
		{"match arms, Some first", `match xs[i] { Some(e) => Some(e.value), None => None }`},
		{"match arms, None first", `match xs[i] { None => None, Some(e) => Some(e.value) }`},
		{"if branches", `if present { Some(fallback) } else { None }`},
		{"array literal", `[Some(fallback), None][if present { 0 } else { 1 }]`},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			out := buildAndRunWithPrelude(t, `module main
struct Slot<v> { value: v }
let pick<v> = pure (xs: []Maybe<Slot<v>>, i: i64, fallback: v, present: bool) -> Maybe<v> => {
  let got = `+c.body+`
  got
}
let main = () -> void => {
  var xs: []Maybe<Slot<i64>> = [None; 4]
  xs[1] = Some(Slot { value: 7 })
  print("${pick(xs, 1, 7, true).unwrap_or(-1)} ${pick(xs, 2, 7, false).unwrap_or(-1)}")
}
`, "")
			if got := strings.TrimSpace(out); got != "7 -1" {
				t.Errorf("%s: printed %q; want \"7 -1\"", c.name, got)
			}
		})
	}
}

// The join's answer must not outrank a context that arrives later. `Some(200)` beside
// `None` now joins to `Maybe<i64>` — the literal's default — and the first version of the
// join fix stamped that onto both arms, promoting the `200` before the `-> Maybe<u8>`
// return could narrow it. Two things keep it open: a join's push never re-stamps the arm
// that solved it, and what it does stamp is marked provisional, so the return context
// overrides it as it overrides the `Some(200)` itself. The second case never compiled
// before 09/07 at all: both arms solved to `Maybe<i64>`, the leaves were narrowed to u8
// by the return context, and the `if` node itself kept the join's `Maybe<i64>` —
// refreshBranchingRecord is what revisits it.
func TestExec_JoinedInstantiationYieldsToTheContext(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ name, body, want string }{
		{"Some(200) beside None", `if b { Some(200) } else { None }`, "200 0"},
		{"Some(200) beside Some(201)", `if b { Some(200) } else { Some(201) }`, "200 201"},
		{"match arms", `match b { true => Some(200), false => None }`, "200 0"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			out := buildAndRunWithPrelude(t, `module main
let get = pure (b: bool) -> Maybe<u8> => `+c.body+`
let main = () -> void => print("${get(true).unwrap_or(0)} ${get(false).unwrap_or(0)}")
`, "")
			if got := strings.TrimSpace(out); got != c.want {
				t.Errorf("%s: printed %q; want %q", c.name, got, c.want)
			}
		})
	}
}
