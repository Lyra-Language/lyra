package main

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Lyra-Language/lyra/pkg/ast"
	"github.com/Lyra-Language/lyra/pkg/backend/llvm"
)

// liveProgram counts in `main` and asks functions below it what to print: the shape
// `--live` serves. `Mood` is used only outside `main`, so reordering its constructors
// leaves `main` alone and must restart on the type's own account.
const liveProgram = `extern usleep: (us: u32) -> i32

data Mood = Calm | Busy

let main = () => {
  var n = 0
  while n < 100000 {
    println("${n} ${label(n)}")
    n += 1
    unsafe { usleep(20000) }
  }
}

let label = pure (n: i64) -> string => "${name(mood(n))} first"
let mood = pure (n: i64) -> Mood => if n % 2 == 0 { Calm } else { Busy }
let name = pure (m: Mood) -> string => match m {
  Calm => "calm",
  Busy => "busy",
}
`

// One --live session: a function's body is swapped into the running program, which keeps
// counting from where it was; a type whose constructors moved restarts it, and so does a
// change to `main`, each saying why.
func TestLive_SwapsABodyAndRestartsOnATypeOrMain(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("--live swaps code only on macOS")
	}
	requireCC(t)
	t.Setenv("LYRA_STD", repoRoot(t))
	path := filepath.Join(t.TempDir(), "prog.lyra")
	saves := 0
	save := func(src string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
		saves++
		stamp := time.Now().Add(time.Duration(saves) * time.Second)
		if err := os.Chtimes(path, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	save(liveProgram)

	stdout, stderr, restore := captureStreams(t)
	stop := make(chan os.Signal, 1)
	result := make(chan int, 1)
	o := buildOptions{path: path, opt: "-O0", watch: true, live: true}
	go func() { result <- watch(o, stop, 20*time.Millisecond) }()
	finish := func() {
		t.Helper()
		stop <- os.Interrupt
		select {
		case <-result:
			restore()
		case <-time.After(30 * time.Second):
			restore()
			t.Fatal("watch did not return after the interrupt")
		}
	}
	defer func() {
		if !t.Failed() {
			return
		}
		t.Logf("stdout:\n%s\nstderr:\n%s", stdout, stderr)
	}()

	if !waitFor(stdout, "3 busy first\n") {
		finish()
		t.Fatal("generation 0 never ran")
	}

	// A body: applied, and the count goes on.
	save(strings.Replace(liveProgram, `"${name(mood(n))} first"`, `"${name(mood(n))} second"`, 1))
	if !waitFor(stderr, "prog.lyra changed; applied") || !waitFor(stdout, " second\n") {
		finish()
		t.Fatal("the new label was not applied")
	}
	firstSecond := firstNumberWith(stdout.String(), "second")
	if firstSecond < 4 {
		finish()
		t.Fatalf("the first line from the new code counts %d: the program started again rather than keeping its state", firstSecond)
	}

	// A type's constructors reordered, `main` untouched: restarted, naming the type.
	save(strings.Replace(liveProgram, "data Mood = Calm | Busy", "data Mood = Busy | Calm", 1))
	if !waitFor(stderr, "restarting: type `Mood` changed its fields or constructors") {
		finish()
		t.Fatal("a reordered data type was not refused")
	}
	if !waitFor(stdout, "0 calm first\n") {
		finish()
		t.Fatal("the restarted program did not start again from 0")
	}

	// `main` itself: restarted.
	save(strings.Replace(liveProgram, "n < 100000", "n < 100001", 1))
	if !waitFor(stderr, "restarting: `main` changed") {
		finish()
		t.Fatal("a change to main was not refused")
	}
	finish()
	if strings.Count(stderr.String(), "applied") != 1 {
		t.Errorf("want exactly one change applied\nstderr: %s", stderr)
	}
}

// liveCallbackProgram keeps a function value in `main`'s state — a button's `on_click`,
// made from `save` once before the loop — and asks `extra` for a third word.
const liveCallbackProgram = `extern usleep: (us: u32) -> i32

struct Button {
  on_click: () -> string,
}

let main = () => {
  let button = Button { on_click: save }
  var n = 0
  while n < 100000 {
    println("${n} ${(button.on_click)()} ${extra()}")
    n += 1
    unsafe { usleep(20000) }
  }
}

let save = pure () -> string => "saved first"
let extra = () -> string => "none"
`

// A callback stored before a swap reaches the newest body, since its thunk calls through
// a slot; a new top-level `var` is defined and initialized when applied, not restarted
// for, and keeps its value through the swap after that.
func TestLive_SwapsACallbackAndInitializesANewGlobal(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("--live swaps code only on macOS")
	}
	requireCC(t)
	t.Setenv("LYRA_STD", repoRoot(t))
	path := filepath.Join(t.TempDir(), "prog.lyra")
	saves := 0
	save := func(src string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
		saves++
		stamp := time.Now().Add(time.Duration(saves) * time.Second)
		if err := os.Chtimes(path, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	save(liveCallbackProgram)

	stdout, stderr, restore := captureStreams(t)
	stop := make(chan os.Signal, 1)
	result := make(chan int, 1)
	o := buildOptions{path: path, opt: "-O0", watch: true, live: true}
	go func() { result <- watch(o, stop, 20*time.Millisecond) }()
	finish := func() {
		t.Helper()
		stop <- os.Interrupt
		select {
		case <-result:
			restore()
		case <-time.After(30 * time.Second):
			restore()
			t.Fatal("watch did not return after the interrupt")
		}
	}
	defer func() {
		if !t.Failed() {
			return
		}
		t.Logf("stdout:\n%s\nstderr:\n%s", stdout, stderr)
	}()

	if !waitFor(stdout, "3 saved first none\n") {
		finish()
		t.Fatal("generation 0 never ran")
	}

	// The stored callback's function: applied, and the button calls the new body.
	edited := strings.Replace(liveCallbackProgram, `"saved first"`, `"saved second"`, 1)
	save(edited)
	if !waitFor(stderr, "prog.lyra changed; applied") || !waitFor(stdout, " saved second none\n") {
		finish()
		t.Fatal("the stored callback kept calling the old body")
	}

	// A new top-level `var`, counted up by `extra`: initialized on apply, from 3.
	counted := strings.Replace(edited, "let extra = () -> string => \"none\"", `var lives = 3
let extra = () -> string => {
  lives += 1
  "${lives}"
}`, 1)
	save(counted)
	if !waitFor(stderr, "top-level `lives` is new; initialized") || !waitFor(stdout, " saved second 4\n") {
		finish()
		t.Fatal("the new global was not initialized on apply")
	}

	// One more swap: `lives` is the program's now, so it counts on rather than starting
	// again from its initializer.
	waitFor(stdout, " saved second 6\n")
	save(strings.Replace(counted, `"saved second"`, `"saved third"`, 1))
	if !waitFor(stdout, " saved third ") {
		finish()
		t.Fatal("the third save was not applied")
	}
	if n := firstNumberAfter(stdout.String(), "saved third"); n < 7 {
		finish()
		t.Fatalf("`lives` read %d after the next swap: it was defined again rather than kept", n)
	}
	finish()
	if strings.Contains(stderr.String(), "restarting") {
		t.Errorf("want no restart\nstderr: %s", stderr)
	}
}

// firstNumberAfter is the last field, as a number, of the first line of out containing
// word.
func firstNumberAfter(out, word string) int {
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, word) {
			fields := strings.Fields(line)
			n, err := strconv.Atoi(fields[len(fields)-1])
			if err == nil {
				return n
			}
		}
	}
	return -1
}

// firstNumberWith is the count on the first line of out that contains word.
func firstNumberWith(out, word string) int {
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, word) {
			n, err := strconv.Atoi(strings.Fields(line)[0])
			if err == nil {
				return n
			}
		}
	}
	return -1
}

// Every row of the compatibility check, without running anything.
func TestLive_Incompatible(t *testing.T) {
	base := liveBase{
		info: &llvm.LiveInfo{
			Main:    "define i32 @main",
			Globals: map[string]string{"lyra_global_.score": "i64", "lyra_argc": "i64"},
			Types:   map[string]string{"%World": "{ i64, double }"},
		},
		added:  map[string]string{"lyra_global_.bonus": "i64"}, // a generation since defined it
		shapes: map[string]string{"::Mood": "Calm(); Busy(); "},
		links:  []string{"-lm"},
	}
	same := func() (*llvm.LiveInfo, map[string]string, []string) {
		info := *base.info
		info.Globals = map[string]string{"lyra_global_.score": "i64", "lyra_argc": "i64"}
		info.Types = map[string]string{"%World": "{ i64, double }", "%New": "{ i8 }"}
		return &info, map[string]string{"::Mood": "Calm(); Busy(); ", "::New": "x: i8; "}, []string{"-lm"}
	}
	cases := []struct {
		name   string
		change func(*llvm.LiveInfo, map[string]string, *[]string)
		want   string
	}{
		{"new functions and types", func(*llvm.LiveInfo, map[string]string, *[]string) {}, ""},
		{"main", func(i *llvm.LiveInfo, _ map[string]string, _ *[]string) { i.Main += " ; changed" }, "`main` changed"},
		{"shape", func(_ *llvm.LiveInfo, s map[string]string, _ *[]string) { s["::Mood"] = "Busy(); Calm(); " }, "type `Mood` changed"},
		{"layout", func(i *llvm.LiveInfo, _ map[string]string, _ *[]string) { i.Types["%World"] = "{ i64, float }" }, "layout of `World` changed"},
		{"new global", func(i *llvm.LiveInfo, _ map[string]string, _ *[]string) { i.Globals["lyra_global_.lives"] = "i64" }, ""},
		{"global type", func(i *llvm.LiveInfo, _ map[string]string, _ *[]string) { i.Globals["lyra_global_.score"] = "i32" }, "top-level `score` changed type"},
		{"added global's type", func(i *llvm.LiveInfo, _ map[string]string, _ *[]string) {
			i.Globals["lyra_global_.bonus"] = "double"
		}, "top-level `bonus` changed type"},
		{"runtime state", func(i *llvm.LiveInfo, _ map[string]string, _ *[]string) {
			i.Globals["lyra_termios_saved"] = "[64 x i8]"
		}, "the runtime needs state"},
		{"links", func(_ *llvm.LiveInfo, _ map[string]string, l *[]string) { *l = append(*l, "-lz") }, "libraries it links changed"},
	}
	for _, c := range cases {
		info, shapes, links := same()
		c.change(info, shapes, &links)
		got := base.incompatible(info, shapes, links)
		if c.want == "" && got != "" || c.want != "" && !strings.Contains(got, c.want) {
			t.Errorf("%s: incompatible = %q, want %q", c.name, got, c.want)
		}
	}
}

// An initializer's text, which the note on a changed one compares, spans its lines.
func TestLive_SpanText(t *testing.T) {
	lines := []string{"let a = 1", "let b = foo(", "  2)"}
	re := regexp.MustCompile(`\s+`)
	got := spanText(lines, ast.Location{StartLine: 2, StartCol: 9, EndLine: 3, EndCol: 5})
	if re.ReplaceAllString(got, " ") != "foo( 2)" {
		t.Errorf("spanText = %q", got)
	}
}

// liveUiProgram is a `ui.app` program whose loop is `main`'s own — `host.step` — counting
// with an `Every` subscription, on a script's platform that waits for good, and printing the
// count and the text its view last showed: what the host laid out, through the `view` in its
// `App`, a function value.
const liveUiProgram = `import ui.app.{ App, Command, Subscription, Every, host_of }
import ui.script.{ timed_platform, Waited }
import ui.backend.{ Renderer }
import ui.draw.{ Size, Color, TextureId }
import ui.text.{ fonts_new }
import ui.layout.{ Node, text }

extern usleep: (us: u32) -> i32

struct Blank {
  textures: i64 = 0,
}

impl Renderer for Blank {
  upload = (self, image) => {
    self.textures += 1
    Some(TextureId(self.textures - 1))
  }
  update = pure (self, id, at, image) => true
  draw = pure (self, background, ops) => {}
}

data Ev = Tick

struct State {
  count: i64 = 0,
}

let main = () => {
  var platform = timed_platform([Waited(1000000000)], Size { w: 100.0, h: 100.0 })
  var renderer = Blank {}
  var fonts = fonts_new()
  let app = App { view: view, update: update, subscriptions: subscriptions, background: Color { r: 0, g: 0, b: 0 } }
  var host = host_of(app, State {}, platform)
  var n = 0
  while n < 100000 && host.step(platform, renderer, fonts) {
    if host.shown.len() > 0 { println("${host.state.count} ${host.shown[0].label}") }
    n += 1
    unsafe { usleep(20000) }
  }
}

let view = pure (s: State) -> Node<Ev> => text("first ${s.count}")

let update = pure (s: State, _: Ev) -> (State, []Command<Ev>) => (State { count: s.count + 1 }, [])

let subscriptions = pure (_: State) -> []Subscription<Ev> => [Every(10, Tick)]
`

// An idle `ui.app` program — a real SDL window (the dummy driver), no subscriptions, nothing
// happening — takes a change rather than being restarted: under `--live` its host waits for
// input at most a tenth of a second, so `main`'s poll comes round. The host knows it is live
// by llvm.LivePIDEnv, which generation 0 leaves naming itself. Until 10/22 it looked for a
// variable generation 0 had already unset, an idle window waited for input for good, and
// every save restarted it after lyrac's wait — a whole rebuild. (The swap test below ticks
// every 10ms, which woke it anyway.)
func TestLive_AnIdleUiProgramTakesAChange(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("--live swaps code only on macOS")
	}
	requireCC(t)
	skipWithoutText(t)
	if err := command(t, "pkg-config", "--exists", "sdl3").Run(); err != nil {
		t.Skip("no SDL3")
	}
	t.Setenv("LYRA_STD", repoRoot(t))
	t.Setenv("SDL_VIDEODRIVER", "dummy")
	t.Setenv("SDL_RENDER_DRIVER", "software")
	path := filepath.Join(t.TempDir(), "app.lyra")
	saves := 0
	save := func(src string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
		saves++
		stamp := time.Now().Add(time.Duration(saves) * time.Second)
		if err := os.Chtimes(path, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	save(liveIdleUiProgram)

	stdout, stderr, restore := captureStreams(t)
	stop := make(chan os.Signal, 1)
	result := make(chan int, 1)
	o := buildOptions{path: path, opt: "-O0", watch: true, live: true}
	go func() { result <- watch(o, stop, 20*time.Millisecond) }()
	finish := func() {
		t.Helper()
		stop <- os.Interrupt
		select {
		case <-result:
			restore()
		case <-time.After(30 * time.Second):
			restore()
			t.Fatal("watch did not return after the interrupt")
		}
	}
	defer func() {
		if !t.Failed() {
			return
		}
		t.Logf("stdout:\n%s\nstderr:\n%s", stdout, stderr)
	}()

	if !waitFor(stdout, "first, live true\n") {
		finish()
		if strings.Contains(stdout.String(), "no window") {
			t.Skip("SDL could not open a dummy window here")
		}
		t.Fatal("generation 0 never ran, or its host did not know it was live")
	}
	save(strings.Replace(liveIdleUiProgram, `"first"`, `"second"`, 1))
	if !waitFor(stderr, "app.lyra changed; ") {
		finish()
		t.Fatal("the change was never taken up")
	}
	if !waitFor(stderr, "app.lyra changed; applied") {
		finish()
		t.Fatal("the idle program was restarted rather than given the change")
	}
	finish()
}

// liveIdleUiProgram is a `ui.app` program in an SDL window whose loop is `main`'s own and
// which nothing wakes: no subscription, no input. It says once what `label` answers and
// whether its host knows it runs under `--live`.
const liveIdleUiProgram = `import ui.sdl3
import ui.app.{ App, Command, Subscription, running, step_running }
import ui.draw.{ Color }
import ui.text.{ fonts_new }
import ui.layout.{ Node, text }

data Ev = Never

struct State {
  n: i64 = 0,
}

let main = () -> u8 => {
  let Ok(window) = sdl3.open("idle", 100.0, 100.0) else {
    println("no window")
    return 2
  }
  let app = App { view: view, update: update, subscriptions: subscriptions, background: Color { r: 0, g: 0, b: 0 } }
  let w = running(app, State {}, window.platform, window.renderer, fonts_new())
  println("${label()}, live ${w.host.live}")
  while step_running(w) {}
  0
}

let label = () -> string => "first"

let view = pure (s: State) -> Node<Ev> => text("${s.n}")

let update = pure (s: State, _: Ev) -> (State, []Command<Ev>) => (s, [])

let subscriptions = pure (_: State) -> []Subscription<Ev> => []
`

// Live reload of a `ui.app` program (phase 4): a changed view is swapped in while the host
// keeps its state — the count goes on from where it was, and the new text is the host's own
// layout's — and a change to the state's fields restarts it, saying so.
func TestLive_SwapsAUiViewAndRestartsOnItsState(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("--live swaps code only on macOS")
	}
	requireCC(t)
	skipWithoutText(t)
	t.Setenv("LYRA_STD", repoRoot(t))
	path := filepath.Join(t.TempDir(), "app.lyra")
	saves := 0
	save := func(src string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
		saves++
		stamp := time.Now().Add(time.Duration(saves) * time.Second)
		if err := os.Chtimes(path, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	save(liveUiProgram)

	stdout, stderr, restore := captureStreams(t)
	stop := make(chan os.Signal, 1)
	result := make(chan int, 1)
	o := buildOptions{path: path, opt: "-O0", watch: true, live: true}
	go func() { result <- watch(o, stop, 20*time.Millisecond) }()
	finish := func() {
		t.Helper()
		stop <- os.Interrupt
		select {
		case <-result:
			restore()
		case <-time.After(30 * time.Second):
			restore()
			t.Fatal("watch did not return after the interrupt")
		}
	}
	defer func() {
		if !t.Failed() {
			return
		}
		t.Logf("stdout:\n%s\nstderr:\n%s", stdout, stderr)
	}()

	if !waitFor(stdout, " first 3\n") {
		finish()
		t.Fatal("generation 0 never ran")
	}
	save(strings.Replace(liveUiProgram, `text("first ${s.count}")`, `text("second ${s.count}")`, 1))
	if !waitFor(stderr, "app.lyra changed; applied") || !waitFor(stdout, " second ") {
		finish()
		t.Fatal("the new view was not applied")
	}
	if n := firstNumberWith(stdout.String(), "second"); n < 4 {
		finish()
		t.Fatalf("the first line from the new view counts %d: the state did not survive the swap", n)
	}
	// The state's fields changed: a restart, said, and the count from 0 again.
	save(strings.Replace(liveUiProgram, "  count: i64 = 0,\n", "  count: i64 = 0,\n  extra: bool = false,\n", 1))
	if !waitFor(stderr, "restarting:") || !waitFor(stdout, "0 first 0\n") {
		finish()
		t.Fatal("a change to the state's fields did not restart the program")
	}
	finish()
}
