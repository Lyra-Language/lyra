package llvm

import (
	"strings"
	"testing"
)

// **Every string a program can name is NUL-terminated past its own bytes**
// (STRING_LAYOUT.md), which is what lets one be handed to C without copying it. The
// terminator sits at `byte_len`, so no reader consults it: the length stays authoritative
// and an interior NUL stays legal.
//
// The invariant is "every producer allocates the extra byte and writes it", and it holds by
// construction at six sites — the four heap allocations through `rcAllocStringPayload`, the
// literal's global constant, and `read_line`, which reserves the byte in its growth test.
// **This test is what notices a seventh.** It asks C, through `strlen`, whether the
// terminator is where the length says it should be; a producer that forgot one reads past
// its own bytes into whatever follows, so the answer is wrong (or the run is).
func TestExec_EveryStringProducerIsNULTerminated(t *testing.T) {
	t.Parallel()
	out := buildAndRunWithPrelude(t, `
module main
unsafe extern pure strlen: (buf: ^u8) -> u64
/// Whether the terminator sits exactly at the string's own length.
let terminated = (s: string) -> bool => i64(unsafe { strlen(s.cstring_ptr()) }) == s.byte_len()
let main = () -> void => {
  let literal = "hello, world"
  let concat = "a" ++ "bcde"
  let interp = "n=${40 + 2}!"
  let sliced = "abcdefgh".slice(2, 5)
  let empty = ""
  let unicode = "héllo"
  let joined = concat ++ interp
  print("${terminated(literal)} ${terminated(concat)} ${terminated(interp)} ")
  print("${terminated(sliced)} ${terminated(empty)} ${terminated(unicode)} ${terminated(joined)}")
}
`, "")
	if got := strings.TrimSpace(out); got != "true true true true true true true" {
		t.Errorf("producers = %q; want every one terminated at its own length", got)
	}
}

// `read_line`'s buffer grows as it reads, so it is the one producer that has to *reserve*
// the byte rather than size it once — its fullness test is `len + 1 < cap`. A line that
// exactly filled the old capacity had nowhere to put the terminator.
func TestExec_ReadLineIsNULTerminated(t *testing.T) {
	t.Parallel()
	// Longer than readLineInitialCap so the growth path runs, and an exact power-of-two
	// length so a reserve-off-by-one lands on the boundary.
	line := strings.Repeat("x", 128)
	out := buildAndRunWithPrelude(t, `
module main
unsafe extern pure strlen: (buf: ^u8) -> u64
let main = () -> void => match read_line() {
  Some(s) => println("${s.byte_len()} ${unsafe { strlen(s.cstring_ptr()) }}"),
  None => println("eof"),
}
`, line+"\n")
	if got := strings.TrimSpace(out); got != "128 128" {
		t.Errorf("read_line = %q; want \"128 128\"", got)
	}
}

// **The terminator does not make an interior NUL legal**, and `cstring_ptr` is where that
// is enforced: C would see the string truncated at the zero byte, which is the
// silently-wrong answer this language traps for everywhere else. The scan is one `memchr`
// pass and allocates nothing — the promise `cstring()` used to carry with a copy.
func TestExec_CStringPtrTrapsOnAnInteriorNUL(t *testing.T) {
	t.Parallel()
	out, code := buildAndRunPanicWithPrelude(t, `
module main
unsafe extern pure strlen: (buf: ^u8) -> u64
let main = () -> void => {
  let s = "a${rune(0)}b"
  println(unsafe { strlen(s.cstring_ptr()) })
}
`)
	if code != 101 {
		t.Fatalf("exited %d; want the trap's 101", code)
	}
	if !strings.Contains(out, "cannot contain a NUL byte") {
		t.Errorf("output = %q; want the interior-NUL message", out)
	}
}

// A string with an interior NUL is still a perfectly ordinary Lyra string — the length is
// authoritative, so it indexes, measures and prints. Only the *crossing* is refused.
func TestExec_AnInteriorNULIsStillAValidString(t *testing.T) {
	t.Parallel()
	out := buildAndRunWithPrelude(t, `
module main
let main = () -> void => {
  let s = "a${rune(0)}b"
  print("${s.len()} ${s.byte_len()} ${s[2]}")
}
`, "")
	if got := strings.TrimSpace(out); got != "3 3 b" {
		t.Errorf("got %q; want \"3 3 b\" — the length is authoritative, not the terminator", got)
	}
}
