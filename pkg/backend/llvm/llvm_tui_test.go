package llvm

import (
	"strconv"
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

// std.tui's decoder, exercised end to end against the real library.
//
// These run over a pipe rather than a terminal, which costs nothing here: the decoder's
// whole job is turning a *byte sequence* into a named event, and a pipe delivers the same
// bytes a terminal would. Only the blocking behaviour differs, and that is `read_key`'s,
// tested above.

// eventNamerSrc is a program that names the first n events it decodes, so a test asserts
// on the decoder's output rather than on escape bytes.
func eventNamerSrc(n int) string {
	return `
module main
import std.tui.{ event_reader, Event, Key, MouseButton, MouseAction }
let btn = (b: MouseButton) -> string => match b {
  MouseLeft => "L", MouseMiddle => "M", MouseRight => "R",
  WheelUp => "WU", WheelDown => "WD", NoButton => "-"
}
let act = (a: MouseAction) -> string => match a { Press => "down", Release => "up", Move => "move" }
let kname = (k: Key) -> string => match k {
  Char c => "Char(${c})", Up => "Up", Down => "Down", Left => "Left", Right => "Right",
  Home => "Home", End => "End", PageUp => "PageUp", PageDown => "PageDown",
  Delete => "Delete", Enter => "Enter", Tab => "Tab", Backspace => "Backspace",
  Escape => "Escape"
}
let describe = (e: Event) -> string => match e {
  Keyboard(k) => kname(k),
  Mouse(m) => "${btn(m.button)}${act(m.action)}@${m.col},${m.row}"
}
let main = () -> void => {
  var ev = event_reader();
  var i = 0;
  for i < ` + strconv.Itoa(n) + ` {
    match ev.next_event() { Some(e) => print("${describe(e)} "), None => print("EOF ") }
    i = i + 1;
  }
  println("");
}
`
}

func assertDecodes(t *testing.T, n int, input, want string) {
	t.Helper()
	got := strings.TrimSpace(buildAndRunWithPrelude(t, eventNamerSrc(n), input))
	if got != want {
		t.Errorf("decoded %q as %q, want %q", input, got, want)
	}
}

// The four arrows in the ordinary `\e[X` form — the keys the whole decoder exists for.
func TestExec_TuiDecodesArrowKeys(t *testing.T) {
	t.Parallel()
	assertDecodes(t, 4, "\x1b[A\x1b[B\x1b[C\x1b[D", "Up Down Right Left")
}

// The `\eOX` form, which a terminal in "application cursor keys" mode sends instead.
// Handling only `\e[` loses arrows outright on a real terminal in that mode.
func TestExec_TuiDecodesApplicationModeArrows(t *testing.T) {
	t.Parallel()
	assertDecodes(t, 2, "\x1bOA\x1bOD", "Up Left")
}

// The numbered `\e[<n>~` family, whose body is read digit by digit to its `~`.
func TestExec_TuiDecodesNumericSequences(t *testing.T) {
	t.Parallel()
	assertDecodes(t, 3, "\x1b[5~\x1b[6~\x1b[3~", "PageUp PageDown Delete")
}

// The lookahead buffer: recognizing a sequence means reading past the `\e`, so when what
// follows is not one, that key has already been consumed. It must come back on the next
// call rather than being dropped — which is the entire reason EventReader holds state.
func TestExec_TuiEscapeLookaheadLosesNothing(t *testing.T) {
	t.Parallel()
	assertDecodes(t, 3, "\x1bxZ", "Escape Char(x) Char(Z)")
}

// The control bytes a terminal sends for keys a reader thinks of as named: CR for Enter
// (raw mode sends CR, not the LF it would outside one) and DEL for Backspace.
func TestExec_TuiDecodesControlKeys(t *testing.T) {
	t.Parallel()
	assertDecodes(t, 3, "\r\t\x7f", "Enter Tab Backspace")
}

// SGR mouse reports: press, release, and the wheel. The coordinates are 1-based and come
// back as written, which is the property a transposed or off-by-one decode would break.
func TestExec_TuiDecodesMouseButtons(t *testing.T) {
	t.Parallel()
	assertDecodes(t, 4,
		"\x1b[<0;10;5M\x1b[<0;10;5m\x1b[<2;8;2M\x1b[<1;5;5m",
		"Ldown@10,5 Lup@10,5 Rdown@8,2 Mup@5,5")
}

// Bit 6 (64) marks the wheel, so a notch is button code 64/65 rather than a low-bit
// button — and terminals send no matching release, which is why a notch is always Press.
func TestExec_TuiDecodesMouseWheel(t *testing.T) {
	t.Parallel()
	assertDecodes(t, 2, "\x1b[<64;3;7M\x1b[<65;1;1M", "WUdown@3,7 WDdown@1,1")
}

// Bit 5 (32) marks motion: with a button in the low bits that is a drag, and with 3
// (no button) it is a bare move. Both arrive only after mouse_enable_motion.
func TestExec_TuiDecodesMouseMotion(t *testing.T) {
	t.Parallel()
	assertDecodes(t, 2, "\x1b[<32;9;3M\x1b[<35;9;4M", "Lmove@9,3 -move@9,4")
}

// Keys and mouse reports share one file descriptor, so they interleave — the reason
// there is one reader and one `next_event` rather than two streams to poll.
func TestExec_TuiInterleavesKeysAndMouse(t *testing.T) {
	t.Parallel()
	assertDecodes(t, 3, "\x1b[<0;1;1Mq\x1b[A", "Ldown@1,1 Char(q) Up")
}
