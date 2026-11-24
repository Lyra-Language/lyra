package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// ui.sdl3's platform half, headless: SDL's own events pushed onto its queue (bindings.sdl3's
// push_event) in a real window on the dummy video driver, and what SdlPlatform.next_events
// makes of them — positions in logical units, the buttons, a key's code and the modifiers it
// was made with, the keypad, typed and composed text, the wheel (natural scrolling's flipped
// amounts turned back), the window's focus, size, place, close and quit, a drop's files
// gathered into one input, and an event of a window it does not have going nowhere. A key's
// primary modifier is Command on a Mac, Control elsewhere.
func TestRun_UiSdl3PlatformTranslatesSDLsEvents(t *testing.T) {
	t.Parallel()
	if err := command(t, "pkg-config", "--exists", "sdl3").Run(); err != nil {
		t.Skip("no SDL3")
	}
	skipWithoutText(t)
	setenv(t, "LYRA_STD", repoRoot(t))
	dir := t.TempDir()
	src := filepath.Join(dir, "main.lyra")
	if err := os.WriteFile(src, []byte(uiSdl3EventsProgram), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "main")
	if _, stderr, code := captureRun(t, "build", "-o", bin, src); code != 0 {
		t.Fatalf("building exited %d\nstderr: %s", code, stderr)
	}
	cmd := command(t, bin)
	cmd.Env = append(cmd.Env, "SDL_VIDEODRIVER=dummy", "SDL_RENDER_DRIVER=software")
	out, err := cmd.Output()
	if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 2 {
		t.Skipf("SDL could not open a dummy window here: %s", out)
	} else if err != nil {
		t.Fatalf("running: %v\n%s", err, out)
	}
	primary := "shift command primary"
	ctrl := "control"
	if runtime.GOOS != "darwin" {
		primary = "shift command"
		ctrl = "control primary"
	}
	want := `pushed 23, all taken
moved 10.5,20
down primary 10.5,20 clicks 2
up secondary 10.5,20
key left [` + primary + `]
key a [` + ctrl + `]
released keypad 5 []
typed é👍
composing かな at 1 for 1
scrolled 0,-1
scrolled -0.5,1
unfocused
focused
resized 400x300
placed 30,40
dropped /a b.txt|/c
close
quit
`
	if string(out) != want {
		t.Errorf("got\n%s\nwant\n%s", out, want)
	}
}

const uiSdl3EventsProgram = `import ui.sdl3
import ui.backend.{
  Platform,
  InputEvent,
  CloseRequested,
  QuitRequested,
  Resized,
  WindowPlaced,
  PointerMoved,
  PointerDown,
  PointerUp,
  Scrolled,
  KeyPressed,
  KeyReleased,
  TextTyped,
  TextComposing,
  WindowFocused,
  WindowUnfocused,
  FilesDropped,
  PointerButton,
  Primary,
  Secondary,
  Key,
  ArrowLeft,
  Character,
  Keypad,
  Modifiers,
}
import bindings.sdl3.{
  push_event,
  window_id,
  Event,
  Quit,
  KeyDown,
  KeyUp,
  MouseMotion,
  MouseDown,
  MouseUp,
  MouseWheel,
  TextInput,
  TextEditing,
  WindowFocusGained,
  WindowFocusLost,
  WindowResized,
  WindowMoved,
  WindowCloseRequested,
  DropBegin,
  DropFile,
  DropComplete,
  KEY_LEFT,
  KEY_KP_5,
  MOD_LSHIFT,
  MOD_LGUI,
  MOD_LCTRL,
}

let main = () -> u8 => {
  let Ok(opened) = sdl3.open("events", 300.0, 200.0) else { return 2 }
  var platform = opened.platform
  let window = window_id(platform.window)
  // What SDL said as the window opened (its focus), read first so only what is pushed follows.
  let _ = platform.next_events(0)
  // A window this platform does not have: its event goes nowhere.
  let stranger = window + 99
  let pushes: [](Event, u16, u32) = [
    (MouseMotion(10.5, 20.0), 0, window),
    (MouseDown(1, 10.5, 20.0, 2), 0, window),
    (MouseUp(3, 10.5, 20.0), 0, window),
    (KeyDown(KEY_LEFT), MOD_LSHIFT | MOD_LGUI, window),
    (KeyDown(97), MOD_LCTRL, window),
    (KeyUp(KEY_KP_5), 0, window),
    (TextInput("é👍"), 0, window),
    (TextEditing("かな", 1, 1), 0, window),
    (MouseWheel(0.0, 1.0, false), 0, window),
    (MouseWheel(0.5, 1.0, true), 0, window),
    (WindowFocusLost, 0, window),
    (WindowFocusGained, 0, window),
    (WindowResized(400, 300), 0, window),
    (WindowMoved(30, 40), 0, window),
    (DropBegin, 0, window),
    (DropFile("/a b.txt"), 0, window),
    (DropFile("/c"), 0, window),
    (DropComplete, 0, window),
    (MouseMotion(1.0, 1.0), 0, stranger),
    (KeyDown(97), 0, stranger),
    (TextInput("lost"), 0, stranger),
    (WindowCloseRequested, 0, window),
    (Quit, 0, 0),
  ]
  var taken = 0
  for (event, held, to) in pushes {
    if push_event(event, held, to) { taken += 1 }
  }
  println("pushed ${pushes.len()}, ${if taken == pushes.len() { "all" } else { "${taken}" }} taken")
  for (_, input) in platform.next_events(0) {
    if let Some(line) = shown(input) { println(line) }
  }
  0
}

/// An input as a line; None for what the platform says of itself whatever was pushed — the
/// look, the motion setting, the window's state.
let shown = (input: InputEvent) -> Maybe<string> => match input {
  CloseRequested => Some("close"),
  QuitRequested => Some("quit"),
  Resized(size) => Some("resized ${size.w}x${size.h}"),
  WindowPlaced(at) => Some("placed ${at.x},${at.y}"),
  PointerMoved(at) => Some("moved ${at.x},${at.y}"),
  PointerDown(button, at, clicks) => Some("down ${button_name(button)} ${at.x},${at.y} clicks ${clicks}"),
  PointerUp(button, at) => Some("up ${button_name(button)} ${at.x},${at.y}"),
  Scrolled(x, y) => Some("scrolled ${x},${y}"),
  KeyPressed(key, mods) => Some("key ${key_name(key)} [${held(mods)}]"),
  KeyReleased(key, mods) => Some("released ${key_name(key)} [${held(mods)}]"),
  TextTyped(text) => Some("typed ${text}"),
  TextComposing(text, at, length) => Some("composing ${text} at ${at} for ${length}"),
  WindowFocused => Some("focused"),
  WindowUnfocused => Some("unfocused"),
  FilesDropped(paths) => Some("dropped ${paths.join("|")}"),
  _ => None,
}

let button_name = (button: PointerButton) -> string => match button {
  Primary => "primary",
  Secondary => "secondary",
  _ => "other",
}

let key_name = (key: Key) -> string => match key {
  ArrowLeft => "left",
  Character(c) => "${c}",
  Keypad(c) => "keypad ${c}",
  _ => "other",
}

let held = (mods: Modifiers) -> string => {
  var out: []string = []
  if mods.shift { out.push("shift") }
  if mods.control { out.push("control") }
  if mods.alt { out.push("alt") }
  if mods.command { out.push("command") }
  if mods.primary { out.push("primary") }
  out.join(" ")
}
`
