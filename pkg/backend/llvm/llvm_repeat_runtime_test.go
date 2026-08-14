package llvm

import (
	"strings"
	"testing"
)

// `[v; n]` with a **runtime** count (08/14) — the buffer a window resize or a terminal
// width sizes, which had no spelling before: the count was a compile-time constant by
// grammar, so `push` in a loop was the only way to build one.
func TestExec_RuntimeRepeatCount(t *testing.T) {
	t.Parallel()
	src := `
let make_frame = (w: i64, h: i64) -> []u32 => [0; w * h]
let main = () -> void => {
  var out = ""
  for step in 0..<3 {
    let w = 40 + step * 37
    let h = 10 + step * 5
    var buf: []u32 = make_frame(w, h)
    buf[0] = 1
    buf[w * h - 1] = 9
    out = out ++ "${buf.len()}:${buf[0]}${buf[w * h - 1]} "
  }
  println(out);
}
`
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "400:19 1155:19 2280:19" {
		t.Errorf("got %q; want \"400:19 1155:19 2280:19\"", got)
	}
}

// Zero is not an error — it yields an empty array, exactly as `[]` does — and a managed
// element still gets one retain per slot beyond the first.
func TestExec_RuntimeRepeatEdges(t *testing.T) {
	t.Parallel()
	src := `
let main = () -> void => {
  let n = 0
  let empty: []i64 = [7; n]
  let five = 3 + 2
  let words: []string = ["hi"; five]
  println("${empty.len()} ${words.len()} ${words[4]}");
}
`
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "0 5 hi" {
		t.Errorf("got %q; want \"0 5 hi\"", got)
	}
}

// **A negative runtime count traps.** The constant form is refused at compile time with
// the number in hand; this is the same rule at the only moment a runtime count exists —
// the ladder a shift amount and a range step already ride.
func TestExec_RuntimeRepeatNegativeCountTraps(t *testing.T) {
	t.Parallel()
	src := `
let main = () -> void => {
  let n = 0 - 5
  let buf: []u32 = [0; n]
  println(buf.len());
}
`
	out, code := runPreludeCombined(t, src)
	if code != 101 || !strings.Contains(out, "must not be negative") {
		t.Errorf("want the negative-length trap, got exit %d and %q", code, out)
	}
}
