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
