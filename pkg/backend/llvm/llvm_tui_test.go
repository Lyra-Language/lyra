package llvm

import (
	"strings"
	"testing"
)

// The terminal builtins: `set_raw_mode`, `read_key`, `terminal_size` (tui.go).
//
// **What these tests can and cannot reach.** A test process has no controlling
// terminal, so the tty-only behaviour — `tcsetattr` actually taking effect, a keypress
// arriving without Enter, echo going off, `TIOCGWINSZ` returning a real window — cannot
// be asserted here and is *not* what is asserted below. What is reachable is everything
// that runs when there is no tty, which is the larger half and includes the whole of
// `read_key` except the blocking: a pipe delivers bytes to `read` immediately, so the
// UTF-8 decode, the multi-byte assembly and the EOF answer are all exercised for real.
//
// The tty half was verified by hand on 08/15 by driving each program through a pty
// (Python's `pty.fork`, with `TIOCSWINSZ` set to a distinctive 123x45): the size came
// back 123x45 in that order, `abcé` arrived as four keys with no Enter and no echo, and
// after `set_raw_mode(false)` the terminal echoed again and `read_line` waited for Enter.
// Recorded here because a reader should know which half the suite is guarding.

// terminal_size answers its documented fallback rather than failing when there is no
// window — and the two components differ (80 vs 24), so this pins the **order** too:
// columns first. A transposed pair is otherwise a silent bug that looks like a
// misrendered frame.
func TestExec_TerminalSizeFallsBackToEightyByTwentyFour(t *testing.T) {
	t.Parallel()
	const src = `
module main
let main = () -> void => {
  let (cols, rows) = terminal_size();
  println("${cols}x${rows}");
}
`
	got := strings.TrimSpace(buildAndRunWithPrelude(t, src, ""))
	if got != "80x24" {
		t.Errorf("terminal_size() without a tty = %q, want \"80x24\" (columns then rows)", got)
	}
}

// read_key returns one code point per call, and a multi-byte character is **one** key
// rather than one key per byte — the property that makes the result a `rune` rather than
// a byte. `é` is two UTF-8 bytes, so a byte-at-a-time reader prints two replacement
// characters here and a correct one prints the letter.
func TestExec_ReadKeyDecodesAMultiByteCodePoint(t *testing.T) {
	t.Parallel()
	const src = `
module main
let main = () -> void => {
  var i = 0;
  for i < 3 {
    match read_key() {
      Some(k) => print("[${k}]"),
      None => print("[none]")
    }
    i = i + 1;
  }
  println("");
}
`
	got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "abé"))
	if got != "[a][b][é]" {
		t.Errorf("read_key over \"abé\" = %q, want \"[a][b][é]\"", got)
	}
}

// Past the end of the input, read_key is None — the reason it answers a Maybe at all.
// A bare `rune` would have to invent a value here, and every sentinel is a real key.
func TestExec_ReadKeyIsNoneAtEOF(t *testing.T) {
	t.Parallel()
	const src = `
module main
let main = () -> void => {
  match read_key() {
    Some(k) => println("key ${k}"),
    None => println("none")
  }
}
`
	got := strings.TrimSpace(buildAndRunWithPrelude(t, src, ""))
	if got != "none" {
		t.Errorf("read_key() at EOF = %q, want \"none\"", got)
	}
}

// A truncated multi-byte sequence is None rather than a half-decoded rune: the leading
// byte promises continuation bytes that never arrive. Guards the continuation loop's
// short-read exit, which is the one path in read_key with two ways to leave it.
func TestExec_ReadKeyIsNoneOnATruncatedSequence(t *testing.T) {
	t.Parallel()
	const src = `
module main
let main = () -> void => {
  match read_key() {
    Some(k) => println("key ${k}"),
    None => println("none")
  }
}
`
	// 0xC3 leads a two-byte sequence; the continuation byte is missing.
	got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "\xc3"))
	if got != "none" {
		t.Errorf("read_key() on a truncated sequence = %q, want \"none\"", got)
	}
}

// set_raw_mode must be harmless where there is no terminal to put into raw mode:
// `tcsetattr` fails, the program carries on. A viewer run with its output piped to a
// file should render, not abort — and the restore path must be equally quiet, including
// the case where nothing was ever saved.
func TestExec_SetRawModeWithoutATTYIsHarmless(t *testing.T) {
	t.Parallel()
	const src = `
module main
let main = () -> void => {
  set_raw_mode(false);
  set_raw_mode(true);
  set_raw_mode(true);
  set_raw_mode(false);
  println("survived");
}
`
	got := strings.TrimSpace(buildAndRunWithPrelude(t, src, ""))
	if got != "survived" {
		t.Errorf("set_raw_mode round trip without a tty = %q, want \"survived\"", got)
	}
}
