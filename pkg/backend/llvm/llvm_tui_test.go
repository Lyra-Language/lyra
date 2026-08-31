package llvm

import (
	"errors"
	"os/exec"
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

// SGR mouse reports: press, release, and the wheel. A terminal reports 1-based cells and
// `MouseEvent` is 0-based, so each coordinate comes back one lower than it was sent —
// which is the property a transposed or misplaced rebase would break. The pairing is
// `move_to`, which adds the one back on the way out.
func TestExec_TuiDecodesMouseButtons(t *testing.T) {
	t.Parallel()
	assertDecodes(t, 4,
		"\x1b[<0;10;5M\x1b[<0;10;5m\x1b[<2;8;2M\x1b[<1;5;5m",
		"Ldown@9,4 Lup@9,4 Rdown@7,1 Mup@4,4")
}

// Bit 6 (64) marks the wheel, so a notch is button code 64/65 rather than a low-bit
// button — and terminals send no matching release, which is why a notch is always Press.
func TestExec_TuiDecodesMouseWheel(t *testing.T) {
	t.Parallel()
	assertDecodes(t, 2, "\x1b[<64;3;7M\x1b[<65;1;1M", "WUdown@2,6 WDdown@0,0")
}

// Bit 5 (32) marks motion: with a button in the low bits that is a drag, and with 3
// (no button) it is a bare move. Both arrive only after mouse_enable_motion.
func TestExec_TuiDecodesMouseMotion(t *testing.T) {
	t.Parallel()
	assertDecodes(t, 2, "\x1b[<32;9;3M\x1b[<35;9;4M", "Lmove@8,2 -move@8,3")
}

// Keys and mouse reports share one file descriptor, so they interleave — the reason
// there is one reader and one `next_event` rather than two streams to poll.
func TestExec_TuiInterleavesKeysAndMouse(t *testing.T) {
	t.Parallel()
	assertDecodes(t, 3, "\x1b[<0;1;1Mq\x1b[A", "Ldown@0,0 Char(q) Up")
}

// `wait_for_key_ms` and the reason it answers a bool.
//
// The timeout path itself is tty-only and not asserted here — a test's stdin is a pipe
// that closes after its content, so it is always immediately readable. It was verified by
// hand through a pty on 08/17: a 300ms wait with nothing sent returned false after 300ms,
// and a lone ESC decoded as `Escape` half a second later *without* a second keypress
// being sent, which is the limitation this builtin exists to remove.
//
// What is reachable here is the property the whole design rests on: at end of input the
// poll answers **true** and the read that follows answers **None**, so "nothing yet" and
// "input ended" stay distinguishable. A timed read returning `Maybe<rune>` would collapse
// them into one `None`, which is the conflation `read_line`'s `Maybe` exists to avoid.
func TestExec_WaitForKeyIsTrueAtEOFAndTheReadIsNone(t *testing.T) {
	t.Parallel()
	const src = `
module main
let main = () -> void => {
  let ready = wait_for_key_ms(50);
  match read_key() {
    Some(k) => println("ready=${ready} key=${i64(k)}"),
    None => println("ready=${ready} key=none")
  }
}
`
	got := strings.TrimSpace(buildAndRunWithPrelude(t, src, ""))
	if got != "ready=true key=none" {
		t.Errorf("at EOF got %q, want \"ready=true key=none\"", got)
	}
}

// With input waiting, the same pair reports the key — the other half of the split.
func TestExec_WaitForKeyIsTrueWithInputWaiting(t *testing.T) {
	t.Parallel()
	const src = `
module main
let main = () -> void => {
  let ready = wait_for_key_ms(50);
  match read_key() {
    Some(k) => println("ready=${ready} key=${i64(k)}"),
    None => println("ready=${ready} key=none")
  }
}
`
	got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "A"))
	if got != "ready=true key=65" {
		t.Errorf("with a key waiting got %q, want \"ready=true key=65\"", got)
	}
}

// A negative timeout is clamped to zero rather than refused or treated as "wait forever".
// Deadline arithmetic (`deadline - now()`) produces negatives naturally once the deadline
// has passed, and "do not wait at all" is the meaning there — so this must return
// promptly rather than hang.
//
// It asserts *elapsed time* rather than the returned bool on purpose: with the timeout
// clamped to zero, whether the parent has closed the stdin pipe before the child polls is
// a genuine race, so the bool is not deterministic here. The contract being tested is that
// a negative timeout is not read as poll's \"wait forever\", and that is about duration.
func TestExec_WaitForKeyClampsANegativeTimeout(t *testing.T) {
	t.Parallel()
	const src = `
module main
let main = () -> void => {
  let t0 = wall_clock_nanos();
  let ready = wait_for_key_ms(-5);
  let ms = (wall_clock_nanos() - t0) / 1000000;
  println(if ms < 1000 { "prompt" } else { "slow" });
}
`
	got := strings.TrimSpace(buildAndRunWithPrelude(t, src, ""))
	if got != "prompt" {
		t.Errorf("a negative timeout took %q, want \"prompt\" — it must clamp to zero, "+
			"not be read as poll's \"wait forever\"", got)
	}
}

// std.tui's wrapper consults the lookahead buffer, which the bare builtin cannot see: a
// code point already read and held is an event that is ready even when the terminal has
// nothing new. Here `\ex` leaves `x` pending once the Escape is reported.
func TestExec_EventAvailableSeesTheLookaheadBuffer(t *testing.T) {
	t.Parallel()
	const src = `
module main
import std.tui.{ event_reader, Event }
let main = () -> void => {
  var ev = event_reader();
  match ev.next_event() { Some(_) => print("first "), None => print("none ") }
  println("pending-ready=${ev.event_available(0)}");
}
`
	got := strings.TrimSpace(buildAndRunWithPrelude(t, src, "\x1bx"))
	if got != "first pending-ready=true" {
		t.Errorf("got %q, want \"first pending-ready=true\"", got)
	}
}

// `std.tui`'s frame layer (frame.lyra / box.lyra / status.lyra), which needs no terminal
// at all: every one of these is string arithmetic, so what a test process lacks — a tty —
// is not what they depend on. The escape sequences are asserted as bytes, since a wrong
// row number or a missing reset is invisible in rendered output.

// **A renderer writes only the rows that differ, and adjacent ones share a cursor move.**
// That is the whole point of the layer: a viewer where only the status line changes should
// write one row, not the screen. Asserted against the exact bytes, because a wrong row
// number renders as a frame drawn one line off and reads as a flicker rather than a fault.
func TestExec_RendererWritesOnlyChangedRows(t *testing.T) {
	t.Parallel()
	out := buildAndRunWithPrelude(t, `
module main
import std.tui.{ renderer, render, invalidate }
let main = () -> void => {
  var r = renderer()
  var a: []string = ["aaa", "bbb", "ccc", "ddd"]
  var b: []string = ["aaa", "XXX", "YYY", "ddd"]
  var c: []string = ["ZZZ", "XXX", "YYY", "ddd"]
  r.render(a)   // nothing known on screen: the whole frame
  r.render(b)   // two adjacent rows: one cursor move
  r.render(c)   // one row
  r.render(c)   // identical: no write at all
  r.invalidate()
  r.render(c)   // told to forget: the whole frame again
}
`, "")
	got := strings.ReplaceAll(strings.ReplaceAll(out, "\x1b", "<E>"), "\r\n", "|")
	want := "<E>[1;1Haaa|bbb|ccc|ddd" + // whole frame, one move
		"<E>[2;1HXXX|YYY" + // the changed band, one move for the pair
		"<E>[1;1HZZZ" + // one row
		// the identical frame writes nothing
		"<E>[1;1HZZZ|XXX|YYY|ddd" // after invalidate
	if got != want {
		t.Errorf("renderer wrote\n  %q\nwant\n  %q", got, want)
	}
}

// A frame with fewer rows than the last must not compare against rows it no longer draws:
// the record is truncated, so a row that comes *back* at its old content is still written.
// Without that, shrinking and regrowing a frame leaves a row blank on screen while the
// renderer believes it is painted.
func TestExec_RendererForgetsRowsAFrameNoLongerDraws(t *testing.T) {
	t.Parallel()
	out := buildAndRunWithPrelude(t, `
module main
import std.tui.{ renderer, render }
let main = () -> void => {
  var r = renderer()
  var tall: []string = ["aaa", "bbb"]
  var short: []string = ["aaa"]
  r.render(tall)
  r.render(short)
  r.render(tall)
}
`, "")
	got := strings.ReplaceAll(strings.ReplaceAll(out, "\x1b", "<E>"), "\r\n", "|")
	// The second frame writes nothing — row 0 is unchanged and row 1 is simply not drawn —
	// and the third must write row 1 again rather than think it is still there.
	want := "<E>[1;1Haaa|bbb" + "<E>[2;1Hbbb"
	if got != want {
		t.Errorf("renderer wrote\n  %q\nwant\n  %q", got, want)
	}
}

// Box drawing is exact-width arithmetic, which is the part worth pinning: an off-by-one
// puts the right border in the wrong column, and on a titled top it did — the row came
// back a column short and the title touched the rule.
func TestExec_BoxPiecesAreExactlyTheirWidth(t *testing.T) {
	t.Parallel()
	out := buildAndRunWithPrelude(t, `
module main
import std.tui.{ box_top, box_bottom, box_row, box_top_titled }
let main = () -> void => {
  print("${box_top(12)}|${box_row("hi", 12)}|${box_bottom(12)}|")
  print("${box_top_titled("Title", 20)}|${box_top_titled("A title far too long", 12)}|")
  print("${box_top(2)}|${box_row("", 2)}|")
  print("${box_top(12).len()} ${box_top_titled("Title", 20).len()} ${box_top_titled("A title far too long", 12).len()}")
}
`, "")
	want := "┌──────────┐|│hi        │|└──────────┘|" +
		"┌─ Title ──────────┐|┌─ A title ┐|" +
		"┌┐|││|" +
		"12 20 12"
	if got := strings.TrimSpace(out); got != want {
		t.Errorf("box pieces = %q; want %q", got, want)
	}
}

// A box narrower than its own borders is a wrong call rather than a tight one, and every
// form goes through the same check — box_top(1) returned a two-column "┌┐" while its own
// documentation said it trapped, for as long as the check lived at one call site.
func TestExec_ABoxNarrowerThanItsBordersTraps(t *testing.T) {
	t.Parallel()
	for _, form := range []string{`box_top(1)`, `box_bottom(1)`, `box_row("x", 1)`, `box_top_titled("t", 1)`} {
		out, err := exec.Command(preludeBinary(t, `
module main
import std.tui.{ box_top, box_bottom, box_row, box_top_titled }
let main = () -> void => { println(`+form+`) }
`)).CombinedOutput()
		var ee *exec.ExitError
		if !errors.As(err, &ee) || ee.ExitCode() != 101 {
			t.Fatalf("%s must trap; got %v", form, err)
		}
		if !strings.Contains(string(out), "at least 2 columns") {
			t.Errorf("%s: output = %q; want the too-narrow message", form, out)
		}
	}
}

// The status bar is always the full width — a bar redrawn shorter leaves the tail of the
// old one, and inverse video makes that stale text obvious. `status_split` keeps the right
// segment when the two do not fit, since it is the one a reader is tracking.
func TestExec_StatusBarIsAlwaysFullWidth(t *testing.T) {
	t.Parallel()
	out := buildAndRunWithPrelude(t, `
module main
import std.tui.{ status_bar, status_split }
let visible = pure (s: string) -> i64 => s.len() - 8
let main = () -> void => {
  print("[${status_bar("q quits", 16)}]")
  print("[${status_split("name.txt", "3:14", 20)}]")
  print("[${status_split("a-very-long-name.txt", "3:14", 12)}]")
  print("${visible(status_bar("q quits", 16))} ${visible(status_split("name.txt", "3:14", 20))}")
}
`, "")
	// Eight bytes of escape per bar: `\e[7m` in and `\e[0m` out.
	want := "[\x1b[7mq quits         \x1b[0m]" +
		"[\x1b[7mname.txt        3:14\x1b[0m]" +
		"[\x1b[7ma-very-l3:14\x1b[0m]" +
		"16 20"
	if got := strings.TrimSpace(out); got != want {
		t.Errorf("status bars = %q; want %q", got, want)
	}
}

// `render` accumulates its frame as a `[]u8` and decodes once, rather than `++`-ing a
// string per row (08/30). The diff logic is unchanged and the tests above pin it byte for
// byte; what the byte path newly puts at risk is the *encoding*, since the frame's bytes
// are now assembled by hand and handed to `decode_utf8` at the end.

// Multi-byte rows through the buffer. Box-drawing characters are three bytes each and are
// what this library's own `box_top` emits, so a frame of them is the ordinary case rather
// than a contrived one — a piece pushed short or out of order shows up as mojibake.
func TestExec_RendererMultiByteRows(t *testing.T) {
	t.Parallel()
	out := buildAndRunWithPrelude(t, `
module main
import std.tui.{ renderer, render }
let main = () -> void => {
  var r = renderer()
  var a: []string = ["┌───┐", "│héllo│", "└───┘"]
  var b: []string = ["┌───┐", "│wörld│", "└───┘"]
  r.render(a)
  r.render(b)   // only the middle row differs
}
`, "")
	got := strings.ReplaceAll(strings.ReplaceAll(out, "\x1b", "<E>"), "\r\n", "|")
	want := "<E>[1;1H┌───┐|│héllo│|└───┘" + "<E>[2;1H│wörld│"
	if got != want {
		t.Errorf("renderer wrote\n  %q\nwant\n  %q", got, want)
	}
}

// A frame large enough that the byte buffer reallocates many times — 40 rows of 100 runes
// is ~4 KB, so it doubles about ten times from the floor of 4. A lost or mis-stored
// reallocation truncates the frame or corrupts a seam, neither of which is visible on the
// three-row frames above. Asserted by length and by both ends rather than in full.
func TestExec_RendererLargeFrameAcrossReallocations(t *testing.T) {
	t.Parallel()
	out := buildAndRunWithPrelude(t, `
module main
import std.tui.{ renderer, render }
let main = () -> void => {
  var rows: []string = []
  for y in 0..<40 {
    var row = ""
    for _ in 0..<99 { row = row ++ "x" }
    rows.push("${y % 10}" ++ row)
  }
  var r = renderer()
  r.render(rows)
  print("|")
}
`, "")
	frame := strings.Split(out, "|")[0]
	// One cursor move (6 bytes), 40 rows of 100 runes, 39 "\r\n" between them.
	if wantLen := 6 + 40*100 + 39*2; len(frame) != wantLen {
		t.Errorf("frame is %d bytes; want %d", len(frame), wantLen)
	}
	if !strings.HasPrefix(frame, "\x1b[1;1H0xxx") {
		t.Errorf("frame starts %q; want a cursor move then row 0", frame[:12])
	}
	if !strings.HasSuffix(frame, "\r\n9"+strings.Repeat("x", 99)) {
		t.Errorf("frame ends %q; want the last row intact", frame[len(frame)-12:])
	}
}

// `fit` and `rule` build their padding through one `repeat` helper (text.lyra, 08/30),
// which assembles bytes and decodes once rather than growing a string a character at a
// time. The box tests above pin the exact widths; these cover what the byte path and the
// two branches inside `repeat` newly put at risk.

// **The multi-byte branch.** A `─` is three bytes, so the pad is built by a two-generator
// comprehension rather than the single-byte repeat form. Width is counted in *runes*, so
// the assertion is the rune length as well as the bytes — a pad built per byte that
// reported per byte would give a rule three times too long and still look plausible.
func TestExec_RuleIsExactAtWidth(t *testing.T) {
	t.Parallel()
	out := buildAndRunWithPrelude(t, `
module main
import std.tui.{ box_top }
let main = () -> void => {
  let wide = box_top(60)
  print("${wide.len()} ${wide[0]} ${wide[1]} ${wide.from_end(1)}")
}
`, "")
	if got := strings.TrimSpace(out); got != "60 ┌ ─ ┐" {
		t.Errorf("box_top(60) = %q; want %q", got, "60 ┌ ─ ┐")
	}
}

// **The single-byte branch**, which is the one `fit` always takes, and the rune count
// again: padding a multi-byte string to a width must count the pad in columns, not bytes.
func TestExec_FitPadsMultiByteTextToRuneWidth(t *testing.T) {
	t.Parallel()
	out := buildAndRunWithPrelude(t, `
module main
import std.tui.{ fit }
let main = () -> void => {
  let padded = fit("héllo", 10)
  print("[${padded}] ${padded.len()} ${padded.byte_len()}")
}
`, "")
	// 10 runes; "héllo" is 6 bytes, plus 5 spaces is 11.
	if got := strings.TrimSpace(out); got != "[héllo     ] 10 11" {
		t.Errorf("fit = %q; want %q", got, "[héllo     ] 10 11")
	}
}

// Text already at the width is returned as it is, rather than concatenated with an empty
// pad — `++` allocates and copies whatever it is handed. `box_top_titled` reaches this on
// every title that fits, since it fits to `title.len().min(room)`.
func TestExec_FitAtExactWidthIsUnchanged(t *testing.T) {
	t.Parallel()
	out := buildAndRunWithPrelude(t, `
module main
import std.tui.{ fit }
let main = () -> void => {
  print("[${fit("exact", 5)}][${fit("日本", 2)}][${fit("", 0)}]")
}
`, "")
	if got := strings.TrimSpace(out); got != "[exact][日本][]" {
		t.Errorf("fit at exact width = %q; want %q", got, "[exact][日本][]")
	}
}

// A non-positive count is the empty string and not a trap, which matters because the
// repeat form traps on a negative one — the guard in `repeat` is what stands between. A
// two-column box has a zero interior and a title with no room left has a negative one.
func TestExec_RuleOfNonPositiveWidthIsEmpty(t *testing.T) {
	t.Parallel()
	out := buildAndRunWithPrelude(t, `
module main
import std.tui.{ box_top, box_top_titled }
let main = () -> void => {
  print("[${box_top(2)}][${box_top_titled("Title", 7)}][${box_top_titled("Title", 8)}]")
}
`, "")
	// width 7: room is 2, the title cuts to "Ti", and the rule gets 0.
	if got := strings.TrimSpace(out); got != "[┌┐][┌─ Ti ┐][┌─ Tit ┐]" {
		t.Errorf("degenerate rules = %q; want %q", got, "[┌┐][┌─ Ti ┐][┌─ Tit ┐]")
	}
}
