package llvm

import (
	"strings"
	"testing"
)

// `[v; n]` is emitted as a **loop** above repeatUnrollLimit, and unrolled below it
// (08/14).
//
// One store per slot is better code for `[0; 3]` — no counter, no branch, and the
// optimizer sees straight through it — and it is a compile-time bomb at any size a frame
// buffer reaches, because the IR then grows *linearly in n*. `[0; 200000]` produced a
// 43 MB `.ll` file and clang had not finished with it after five minutes, with nothing
// diagnosing the cause: the build simply never returned.
func TestEmit_LargeRepeatLiteralIsALoop(t *testing.T) {
	t.Parallel()
	ir, err := emitSource(t, `
const N = 200000
let main = () -> void => {
  let buf: []u32 = [0; N]
  println(buf.len());
}
`)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	// The whole point is that the IR does not scale with the count. A per-slot store
	// would put 200000 of these in the module.
	if n := strings.Count(ir, "store i32 0,"); n > 4 {
		t.Errorf("want a loop, got %d element stores — the repeat literal is still unrolled", n)
	}
	if len(ir) > 400_000 {
		t.Errorf("IR is %d bytes; a loop should keep it independent of the count", len(ir))
	}
}

// Below the limit it stays straight-line, which is the better code and what every repeat
// literal anyone writes by hand actually is.
func TestEmit_SmallRepeatLiteralStaysUnrolled(t *testing.T) {
	t.Parallel()
	ir, err := emitSource(t, `
let main = () -> void => {
  let buf: []u32 = [7; 8]
  println(buf.len());
}
`)
	if err != nil {
		t.Fatalf("emit: %v", err)
	}
	if n := strings.Count(ir, "store i32 7,"); n != 8 {
		t.Errorf("want 8 unrolled stores, got %d", n)
	}
}

// The loop has to produce the same array the unrolled form did — every slot written, the
// first and last included, which is where an off-by-one in the bounds would show.
func TestExec_LargeRepeatLiteralFillsEverySlot(t *testing.T) {
	t.Parallel()
	src := `
const BIG = 100000
let main = () -> void => {
  let buf: []i64 = [3; BIG]
  var sum = 0
  for i in 0..<BIG { sum += buf[i] }
  println("${buf.len()} ${buf[0]} ${buf[BIG - 1]} ${sum}");
}
`
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "100000 3 3 300000" {
		t.Errorf("got %q; want \"100000 3 3 300000\"", got)
	}
}

// **A managed element is retained once per slot beyond the first**, and the looped path
// has to keep that promise as exactly as the unrolled one — every slot is an owner, so a
// count that is one low is a use-after-free and one high is a leak. The ASan suite is what
// would catch the first; this asserts the values survive being read back at all.
func TestExec_LargeRepeatLiteralOfAManagedElement(t *testing.T) {
	t.Parallel()
	src := `
const BIG = 50000
let main = () -> void => {
  let words: []string = ["hi"; BIG]
  println("${words.len()} ${words[0]} ${words[BIG - 1]}");
}
`
	if got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "")); got != "50000 hi hi" {
		t.Errorf("got %q; want \"50000 hi hi\"", got)
	}
}
