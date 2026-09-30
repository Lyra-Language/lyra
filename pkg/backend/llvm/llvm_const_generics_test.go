package llvm

import (
	"strings"
	"testing"
)

// Const generics run: one specialization per size, a generic calling a generic with its
// own size, and a `[N]t` built and returned (09/30).
func TestExec_ConstGenerics(t *testing.T) {
	t.Parallel()
	got := strings.TrimSpace(buildAndRunWithPrelude(t, `module main
let total<const N: i64> = pure (xs: ref [N]i64) -> i64 => {
  var s = 0
  for x in xs { s += x }
  s
}

let average<const M: i64> = pure (xs: ref [M]i64) -> i64 => total(xs) / xs.len()

let doubled<const N: i64> = pure (xs: [N]i64) -> [N]i64 => {
  var out: [N]i64 = xs
  for i in 0..<xs.len() { out[i] = xs[i] * 2 }
  out
}

const TABLE: [4]i64 = #[10, 20, 30, 40]

let main = () -> void => {
  let small = #[1, 2, 3]
  let d = doubled(small)
  println("${total(small)} ${total(TABLE)} ${average(#[2, 4, 6])} ${d[2]} ${total(doubled(TABLE))}")
}
`, ""))
	if want := "6 100 4 6 200"; got != want {
		t.Errorf("got %q; want %q", got, want)
	}
}
