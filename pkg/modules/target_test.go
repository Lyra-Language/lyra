package modules_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Lyra-Language/lyra/pkg/diagnostic"
	"github.com/Lyra-Language/lyra/pkg/driver"
)

// **A `lyra.toml` naming a console target makes the compiler say what cannot work
// there** (09/29, the step towards Vega's engine): code reaching outside Lyra, heap
// allocation, and arithmetic the 68000 emulates — against the shipped standard library,
// since "reaches the host through `read_file`" is only true of the real one.

// analyzeGame writes a project — lyra.toml (when given) and app.lyra — and analyzes it
// against the repository's std, as lyrac does.
func analyzeGame(t *testing.T, config, app string) *driver.Result {
	t.Helper()
	root, _ := shippedPreludePath(t)
	files := map[string]string{"app.lyra": app}
	if config != "" {
		files["lyra.toml"] = config
	}
	game := buildTree(t, files)
	return analyzeWith(t, filepath.Join(game, "app.lyra"), game, root)
}

const genesis = "target = \"genesis\"\n"

// linesWith lists the lines a code was reported on, in app.lyra only — and fails if it was
// reported anywhere else, since the standard library is not the game's to fix.
func linesWith(t *testing.T, res *driver.Result, code string) []int {
	t.Helper()
	var lines []int
	for _, d := range res.Diagnostics {
		if d.Code != code {
			continue
		}
		if !strings.HasSuffix(d.Location.File, "app.lyra") {
			t.Errorf("%s reported outside the program's own code: %s %s", code, d.Location, d.Message)
			continue
		}
		lines = append(lines, d.Location.StartLine)
	}
	return lines
}

func TestTarget_ReportsWhatTheGenesisCannotRun(t *testing.T) {
	res := analyzeGame(t, genesis, `import std.io.{ read_file }

let total = (n: i64) -> i64 => {
  var t = 0
  for i in 0..<n { t += i * 2 }
  t + n * 3
}

let main = () -> void => {
  println("x")
  let names = ["a", "b"]
  let level = read_file("level1.bin")
  let doubled = names.map((n) => n)
  let fixed = #[1, 2, 3]
  let speed: f32 = 1.5
  let fast = speed * 2.0
}
`)
	if got := linesWith(t, res, diagnostic.CodeTargetHost); !equal(got, []int{10, 12}) {
		t.Errorf("E084 (the host) on println and read_file, lines 10 and 12; got %v", got)
	}
	if got := linesWith(t, res, diagnostic.CodeTargetAlloc); !equal(got, []int{11, 13}) {
		t.Errorf("E085 (the heap) on the array literal and map, lines 11 and 13; got %v", got)
	}
	// Once per function and type: `total`'s two i64 sums are one warning, the f32 another.
	if got := linesWith(t, res, diagnostic.CodeTargetEmulated); !equal(got, []int{5, 16}) {
		t.Errorf("W026 (emulated) once for total's i64 and once for main's f32; got %v", got)
	}
}

// What a console game is made of — 16-bit values, structs, fixed arrays, `noalloc`
// functions — reports nothing.
func TestTarget_AConsoleProgramIsClean(t *testing.T) {
	res := analyzeGame(t, genesis, `struct Pos {
  x: i16,
  y: i16,
}

let step = pure noalloc (p: Pos, pad: u8) -> Pos => {
  var x = p.x
  if (pad & 4) == 0 { x = (x - 2).max(128) }
  Pos { p | x: x }
}

let main = () -> void => {
  let table = #[1, 2, 3]
  var sum: i16 = 0
  for v in table { sum += i16(v) }
  let moved = step(Pos { x: 200, y: sum }, 0xFB)
}
`)
	for _, code := range []string{diagnostic.CodeTargetHost, diagnostic.CodeTargetAlloc, diagnostic.CodeTargetEmulated} {
		if got := linesWith(t, res, code); len(got) != 0 {
			t.Errorf("%s reported on lines %v of a program the Genesis can run", code, got)
		}
	}
}

// A callback's effect is reported inside the callback — the line that does it — and not
// again at the higher-order call it is handed to.
func TestTarget_ACallbackReportsItsOwnLine(t *testing.T) {
	res := analyzeGame(t, genesis, `let twice = (f: () -> void) -> void => {
  f()
  f()
}

let main = () -> void => {
  twice(() => {
    println("hi")
  })
}
`)
	if got := linesWith(t, res, diagnostic.CodeTargetHost); !equal(got, []int{8}) {
		t.Errorf("E084 on println inside the callback (line 8) only; got %v", got)
	}
}

// With no lyra.toml, or `target = "host"`, nothing changes.
func TestTarget_TheHostChecksNothingNew(t *testing.T) {
	app := `let main = () -> void => {
  println("${[1, 2].len()}")
}
`
	for _, config := range []string{"", "target = \"host\"\n"} {
		res := analyzeGame(t, config, app)
		for _, code := range []string{diagnostic.CodeTargetHost, diagnostic.CodeTargetAlloc, diagnostic.CodeTargetEmulated} {
			if got := linesWith(t, res, code); len(got) != 0 {
				t.Errorf("config %q: %s reported on lines %v for the host", config, code, got)
			}
		}
		if !res.Target.Host {
			t.Errorf("config %q: expected the host target, got %q", config, res.Target.Name)
		}
	}
}

// A lyra.toml that cannot be read is an error at the top of the entry file, naming the
// file and line — not silently the host.
func TestTarget_ABadConfigIsReported(t *testing.T) {
	res := analyzeGame(t, "target = \"gameboy\"\n", "let main = () -> void => {}\n")
	var found bool
	for _, d := range res.Errors() {
		if d.Code == diagnostic.CodeProjectConfig && strings.Contains(d.Message, "lyra.toml:1: unknown target \"gameboy\"") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected lyra-E086 naming lyra.toml:1; got %v", res.Diagnostics)
	}
}

func equal(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
