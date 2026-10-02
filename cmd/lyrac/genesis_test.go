package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Lyra-Language/lyra/pkg/rom"
)

// **`lyrac build` for the Genesis makes a ROM that runs** (09/30): examples/genesis's
// backdrop, compiled by the M68k LLVM, linked with the runtime, and — where `$SHELIAK`
// names Sheliak — run headless, its screen the blue the program sets. A program that
// panics shows the runtime's red. Skipped without the toolchain (tools/llvm-m68k.sh),
// which CI does not build.

func genesisToolchainOrSkip(t *testing.T) {
	t.Helper()
	if _, err := findM68kToolchain(); err != nil {
		t.Skip(err)
	}
}

func TestGenesis_BuildsARunnableROM(t *testing.T) {
	genesisToolchainOrSkip(t)
	root := repoRoot(t)
	t.Setenv("LYRA_STD", root)
	out := filepath.Join(t.TempDir(), "backdrop.bin")
	_, stderr, code := captureRun(t, "build", "-o", out, filepath.Join(root, "examples", "genesis", "backdrop.lyra"))
	if code != 0 {
		t.Fatalf("build exited %d: %s", code, stderr)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if len(data)%0x20000 != 0 || string(data[0x100:0x10F]) != "SEGA MEGA DRIVE" {
		t.Fatalf("not a cartridge: %d bytes, system %q", len(data), data[0x100:0x110])
	}
	if binary.BigEndian.Uint16(data[0x18E:]) != rom.Checksum(data) {
		t.Errorf("the header's checksum is wrong")
	}
	if got := screenOf(t, out); got != "0000ff" {
		t.Errorf("the screen is %s, want the blue the program sets (0000ff)", got)
	}
}

func TestGenesis_APanicTurnsTheScreenRed(t *testing.T) {
	genesisToolchainOrSkip(t)
	root := repoRoot(t)
	t.Setenv("LYRA_STD", root)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "lyra.toml"), []byte("target = \"genesis\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(dir, "overflow.lyra")
	// The VDP's status word is read, so the overflow happens at run time, not in the
	// optimizer.
	if err := os.WriteFile(src, []byte(`let status = pure () -> ^u16 => unsafe { pointer_at(0xC00004) }
let main = () -> void => {
  var x: i16 = 0x7FFF
  let s = unsafe { status().read_volatile() }
  x += i16(s & 1) + 1
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "overflow.bin")
	if _, stderr, code := captureRun(t, "build", "-o", out, src); code != 0 {
		t.Fatalf("build exited %d: %s", code, stderr)
	}
	if got := screenOf(t, out); got != "ff0000" {
		t.Errorf("the screen is %s, want the runtime's panic red (ff0000)", got)
	}
}

// A heap allocation the front end does not see is still refused, by the link, with the
// reason — not a linker's "undefined symbol".
func TestGenesis_AnAllocationIsExplainedAtTheLink(t *testing.T) {
	genesisToolchainOrSkip(t)
	root := repoRoot(t)
	t.Setenv("LYRA_STD", root)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "lyra.toml"), []byte("target = \"genesis\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A module-level initializer is not target-checked yet (todo.md), so this reaches the
	// linker needing malloc.
	src := filepath.Join(dir, "table.lyra")
	if err := os.WriteFile(src, []byte(`let table: []u16 = [1, 2, 3]
let port = pure () -> ^mut u16 => unsafe { pointer_at(0xC00000) }
let main = () -> void => unsafe { port().write_volatile(table[1]) }
`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, stderr, code := captureRun(t, "build", "-o", filepath.Join(dir, "t.bin"), src)
	if code == 0 || !bytes.Contains([]byte(stderr), []byte("allocates on the heap (`malloc`), and the Genesis has none")) {
		t.Errorf("expected the link to explain the malloc; exit %d, stderr: %s", code, stderr)
	}
}

// TestGenesis_TheWalkerWalks builds examples/genesis/walker.lyra — std.genesis's vdp, pad
// and sprites together, the sprites shown by its vertical blank interrupt — and plays it:
// the hero stands in the middle with the pad let go, and ends pressed into the
// bottom-right corner with right and down held (the edge stop makes the place exact).
func TestGenesis_TheWalkerWalks(t *testing.T) {
	genesisToolchainOrSkip(t)
	root := repoRoot(t)
	t.Setenv("LYRA_STD", root)
	out := filepath.Join(t.TempDir(), "walker.bin")
	if _, stderr, code := captureRun(t, "build", "-o", out, filepath.Join(root, "examples", "genesis", "walker.lyra")); code != 0 {
		t.Fatalf("build exited %d: %s", code, stderr)
	}
	for _, c := range []struct {
		name   string
		frames string
		hold   string
		x0, y0 int
		x1, y1 int
	}{
		{"standing in the middle", "30", "", 152, 104, 168, 120},
		{"in the bottom-right corner", "300", "right,down", 304, 208, 320, 224},
	} {
		t.Run(c.name, func(t *testing.T) {
			screen := runROM(t, out, c.frames, c.hold)
			x0, y0, x1, y1 := screen.bounds(screen.dominant())
			// The art has transparent columns at its sides, so the drawn hero sits inside
			// its 16×16 box rather than filling it.
			if x0 < c.x0 || y0 < c.y0 || x1 > c.x1 || y1 > c.y1 || x0 >= x1 {
				t.Errorf("the hero is drawn in [%d,%d)–[%d,%d), want inside [%d,%d)–[%d,%d)",
					x0, y0, x1, y1, c.x0, c.y0, c.x1, c.y1)
			}
		})
	}
}

// screen is a frame Sheliak wrote: its size and RGB pixels.
type screen struct {
	width, height int
	pixels        []byte
}

// runROM runs rom in Sheliak for `frames` frames holding `hold` on pad 1 (none when
// empty), or skips when there is no Sheliak to run.
func runROM(t *testing.T, romPath, frames, hold string) screen {
	t.Helper()
	emulator := os.Getenv("SHELIAK")
	if emulator == "" {
		t.Log("SHELIAK is not set; the ROM was built but not run")
		t.SkipNow()
	}
	ppm := romPath + "." + frames + hold + ".ppm"
	args := []string{romPath, "--frames", frames, "--ppm", ppm}
	if hold != "" {
		args = append(args, "--hold", hold)
	}
	cmd := exec.Command(emulator, args...)
	cmd.Env = append(os.Environ(), "SDL_VIDEODRIVER=dummy")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("sheliak: %v\n%s", err, out)
	}
	data, err := os.ReadFile(ppm)
	if err != nil {
		t.Fatal(err)
	}
	// P6, width, height, 255, one whitespace byte, then RGB.
	var s screen
	var magic string
	var maxval int
	if n, err := fmt.Sscan(string(data), &magic, &s.width, &s.height, &maxval); err != nil || n != 4 || magic != "P6" {
		t.Fatalf("not a PPM: %v", err)
	}
	s.pixels = data[len(data)-s.width*s.height*3:]
	return s
}

// dominant is the colour most of the screen is, as hex RGB.
func (s screen) dominant() string {
	counts := map[string]int{}
	best := ""
	for k := 0; k+2 < len(s.pixels); k += 3 {
		c := fmt.Sprintf("%02x%02x%02x", s.pixels[k], s.pixels[k+1], s.pixels[k+2])
		counts[c]++
		if counts[c] > counts[best] {
			best = c
		}
	}
	return best
}

// bounds is the rectangle holding every pixel not of colour `background`.
func (s screen) bounds(background string) (x0, y0, x1, y1 int) {
	x0, y0 = s.width, s.height
	for y := 0; y < s.height; y++ {
		for x := 0; x < s.width; x++ {
			k := (y*s.width + x) * 3
			if fmt.Sprintf("%02x%02x%02x", s.pixels[k], s.pixels[k+1], s.pixels[k+2]) == background {
				continue
			}
			x0, y0 = min(x0, x), min(y0, y)
			x1, y1 = max(x1, x+1), max(y1, y+1)
		}
	}
	return
}

// screenOf runs rom in Sheliak for 10 frames and answers the colour most of the screen
// is — or skips when there is no Sheliak to run.
func screenOf(t *testing.T, romPath string) string {
	t.Helper()
	return runROM(t, romPath, "10", "").dominant()
}

// TestGenesis_AVBlankHandlerRuns plays examples/genesis/vblank.lyra: its `@interrupt(vblank)`
// handler counts frames, and `main` waits on the count in a plain loop — so the screen
// turns from red to blue only if the runtime's vector reaches the handler, the handler
// preserves what it interrupts, and the shared variable is read afresh each time round.
func TestGenesis_AVBlankHandlerRuns(t *testing.T) {
	genesisToolchainOrSkip(t)
	root := repoRoot(t)
	t.Setenv("LYRA_STD", root)
	out := filepath.Join(t.TempDir(), "vblank.bin")
	if _, stderr, code := captureRun(t, "build", "-o", out, filepath.Join(root, "examples", "genesis", "vblank.lyra")); code != 0 {
		t.Fatalf("build exited %d: %s", code, stderr)
	}
	if got := runROM(t, out, "30", "").dominant(); got != "ff0000" {
		t.Errorf("after 30 frames the screen is %s, want still red (ff0000)", got)
	}
	if got := runROM(t, out, "100", "").dominant(); got != "0000ff" {
		t.Errorf("after 100 frames the screen is %s, want blue (0000ff) — the handler has run 30 times", got)
	}
}

// TestGenesis_ProgressReportsEachStage builds with `--progress`, as Vega does to show a
// game's build: a line on stderr as each stage starts, never going backwards — checking
// first, linking last — and, on a first build into an empty cache, one for each runtime
// object compiled, which a second build reads from the cache instead.
func TestGenesis_ProgressReportsEachStage(t *testing.T) {
	genesisToolchainOrSkip(t)
	root := repoRoot(t)
	t.Setenv("LYRA_STD", root)
	// An empty cache, wherever os.UserCacheDir looks on this system — the toolchain,
	// found under the real home by default, named first.
	tc, _ := findM68kToolchain()
	t.Setenv("LYRA_M68K_LLVM", tc.root)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	src := filepath.Join(root, "examples", "genesis", "backdrop.lyra")
	out := filepath.Join(t.TempDir(), "backdrop.bin")
	stages := func() []string {
		_, stderr, code := captureRun(t, "build", "--progress", "-o", out, src)
		if code != 0 {
			t.Fatalf("build exited %d: %s", code, stderr)
		}
		var lines []string
		last := -1
		for _, line := range strings.Split(stderr, "\n") {
			var percent int
			if _, err := fmt.Sscanf(line, "progress: %d%%", &percent); err != nil {
				continue
			}
			if percent < last {
				t.Errorf("progress went back to %q after %d%%", line, last)
			}
			last = percent
			lines = append(lines, line)
		}
		return lines
	}
	compiles := func(lines []string) int {
		n := 0
		for _, line := range lines {
			if strings.Contains(line, "compiling the runtime library") {
				n++
			}
		}
		return n
	}
	cold := stages()
	if len(cold) == 0 || cold[0] != "progress: 0% checking backdrop.lyra" || cold[len(cold)-1] != "progress: 90% linking" {
		t.Fatalf("stages from checking to linking, got %q", cold)
	}
	if n := compiles(cold); n != 1+len(genesisHelpers) {
		t.Errorf("a first build compiles the runtime and %d helpers, reported %d", len(genesisHelpers), n)
	}
	if n := compiles(stages()); n != 0 {
		t.Errorf("a second build reads the runtime from the cache, but reported %d compiles", n)
	}
}

// TestGenesis_AVariableIndexIntoALocalArray reads a table at an index known only at run
// time — pad 1's right button, held, makes it 1 — and paints the backdrop with what it
// finds. LLVM's M68k backend addressed `xs[i]` on a stack array as `xs[0]`
// (the index register dropped beside a frame index), which `tools/llvm-m68k-patches`
// 0001 fixes; without it the screen is red, the table's first colour (09/30, found by
// Vega's animations).
func TestGenesis_AVariableIndexIntoALocalArray(t *testing.T) {
	genesisToolchainOrSkip(t)
	root := repoRoot(t)
	t.Setenv("LYRA_STD", root)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "lyra.toml"), []byte("target = \"genesis\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(dir, "index.lyra")
	// A table of structs copied to the stack — a `var`, since a `const` one is read in
	// place in ROM — and walked from a run-time start, as Vega's animation steps were
	// before const struct tables became data.
	if err := os.WriteFile(src, []byte(`import std.genesis.vdp
import std.genesis.pad

struct Swatch {
  name: u16,
  color: u16,
}

const SWATCHES: [4]Swatch = #[
  Swatch { name: 1, color: 0x000E },
  Swatch { name: 2, color: 0x00E0 },
  Swatch { name: 3, color: 0x0E00 },
  Swatch { name: 4, color: 0x0EEE },
]

let main = () -> void => {
  vdp.init()
  pad.init()
  vdp.set_color(0, 0, find((pad.read(1) >> 3) & 3, 0))
  vdp.display_on()
  for {}
}

/// The colour of the first swatch from `+"`first`"+` on named at least `+"`name`"+`, as Vega's
/// frame_of walks an animation's steps from its first.
let find = (first: u16, name: u16) -> u16 => {
  var table = SWATCHES
  for k: u16 in first..<4 {
    let s = table[k]
    if s.name >= name { return s.color }
  }
  0
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "index.bin")
	if _, stderr, code := captureRun(t, "build", "-o", out, src); code != 0 {
		t.Fatalf("build exited %d: %s", code, stderr)
	}
	if got := runROM(t, out, "30", "right").dominant(); got != "00ff00" {
		t.Errorf("with right held the screen is %s, want green (00ff00): SWATCHES[1], not SWATCHES[0]", got)
	}
}

// TestGenesis_WritePlaneShowsAMap writes a 4×3 map into plane B from its cell (1, 1):
// its one tile, at map cell (2, 1) in palette 1, must land on the plane's cell (1, 0) —
// the screen's pixels 8–16 across and 0–8 down — and the cells past the map's edge must
// be blank, so nothing else is drawn.
func TestGenesis_WritePlaneShowsAMap(t *testing.T) {
	genesisToolchainOrSkip(t)
	root := repoRoot(t)
	t.Setenv("LYRA_STD", root)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "lyra.toml"), []byte("target = \"genesis\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(dir, "plane.lyra")
	if err := os.WriteFile(src, []byte(`import std.genesis.vdp

/// One tile, every pixel colour 1.
const SOLID: [8]u32 = #[
  0x11111111, 0x11111111, 0x11111111, 0x11111111,
  0x11111111, 0x11111111, 0x11111111, 0x11111111,
]

/// Tile 1 in palette 1 at (2, 1); every other cell blank.
const MAP: [12]u16 = #[
  0, 0, 0, 0,
  0, 0, 0x2001, 0,
  0, 0, 0, 0,
]

let main = () -> void => {
  vdp.init()
  vdp.set_color(1, 1, vdp.rgb(0, 7, 0))
  vdp.load_tiles(1, SOLID)
  vdp.write_plane(vdp.PLANE_B, MAP, 4, 1, 1)
  vdp.display_on()
  for {}
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "plane.bin")
	if _, stderr, code := captureRun(t, "build", "-o", out, src); code != 0 {
		t.Fatalf("build exited %d: %s", code, stderr)
	}
	// 60 frames: vdp.init clears all of VRAM first, which takes most of 10.
	screen := runROM(t, out, "60", "")
	if x0, y0, x1, y1 := screen.bounds(screen.dominant()); x0 != 8 || y0 != 0 || x1 != 16 || y1 != 8 {
		t.Errorf("the tile is drawn in [%d,%d)–[%d,%d), want [8,0)–[16,8)", x0, y0, x1, y1)
	}
}

// genesisProgram builds `source` as a Genesis program and answers the ROM's path.
func genesisProgram(t *testing.T, name, source string) string {
	t.Helper()
	root := repoRoot(t)
	t.Setenv("LYRA_STD", root)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "lyra.toml"), []byte("target = \"genesis\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(dir, name+".lyra")
	if err := os.WriteFile(src, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, name+".bin")
	if _, stderr, code := captureRun(t, "build", "-o", out, src); code != 0 {
		t.Fatalf("build exited %d: %s", code, stderr)
	}
	return out
}

// TestGenesis_ACameraScrollsAMapBiggerThanAPlane scrolls an 80×40-cell map — bigger than
// the 64×32 plane both ways — right to x 480, then down to y 96, a few pixels a frame,
// with std.genesis.camera writing the columns and rows that come into view. Two tiles must
// then be where the view shows them: (70, 20), outside the plane's first 64 columns, only
// a column written as it came into view puts there; (62, 35), below its first 32 rows, only
// a row written as it came into view — each across the plane's wrap.
func TestGenesis_ACameraScrollsAMapBiggerThanAPlane(t *testing.T) {
	genesisToolchainOrSkip(t)
	out := genesisProgram(t, "scroll", `import std.genesis.vdp
import std.genesis.camera
import std.genesis.camera.{ Camera }

const SOLID: [8]u32 = #[
  0x11111111, 0x11111111, 0x11111111, 0x11111111,
  0x11111111, 0x11111111, 0x11111111, 0x11111111,
]

let main = () -> void => {
  vdp.init()
  vdp.set_color(0, 1, vdp.rgb(0, 7, 0))
  vdp.load_tiles(1, SOLID)
  var map: [3200]u16 = #[0; 3200]
  map[20 * 80 + 70] = 1
  map[35 * 80 + 62] = 1
  var view = Camera { world_width: 640, world_height: 320 }
  camera.show_map(view, vdp.PLANE_B, map, 80)
  vdp.display_on()
  for {
    view.last_x = view.x
    view.last_y = view.y
    if view.x < 480 { view.x += 4 } else { view.y = (view.y + 2).min(96) }
    vdp.wait_vblank()
    camera.scroll_map(view, vdp.PLANE_B, map, 80)
  }
}
`)
	// (70, 20) at (560 - 480, 160 - 96) and (62, 35) at (496 - 480, 280 - 96): together
	// they span [16, 64)–[88, 192), and with either missing the span is another.
	screen := runROM(t, out, "240", "")
	if x0, y0, x1, y1 := screen.bounds(screen.dominant()); x0 != 16 || y0 != 64 || x1 != 88 || y1 != 192 {
		t.Errorf("the tiles span [%d,%d)–[%d,%d), want [16,64)–[88,192)", x0, y0, x1, y1)
	}
}

// TestGenesis_TheCameraFollowsInTheMiddleThird walks an 8-pixel sprite right through a
// world 1024 wide: the view stays put while it is inside the camera's zone, then moves
// with it, so it stops on the screen at the zone's right edge — x 214 - 8 = 206 for the
// default, the middle third. A zone the game sets moves that edge, and one narrower than
// the sprite keeps it centred.
func TestGenesis_TheCameraFollowsInTheMiddleThird(t *testing.T) {
	genesisToolchainOrSkip(t)
	for _, c := range []struct {
		name string
		zone string
		x    int
	}{
		{"the middle third by default", "", 206},
		{"a zone the game sets", "view.zone = Zone { left: 40, top: 74, right: 280, bottom: 150 }", 272},
		{"a zone of nothing keeps it centred", "view.zone = camera.centred_zone(0, 0)", 156},
	} {
		t.Run(c.name, func(t *testing.T) {
			out := genesisProgram(t, "follow", `import std.genesis.vdp
import std.genesis.sprites
import std.genesis.sprites.{ Sprite }
import std.genesis.camera
import std.genesis.camera.{ Camera, Zone }

const SOLID: [8]u32 = #[
  0x11111111, 0x11111111, 0x11111111, 0x11111111,
  0x11111111, 0x11111111, 0x11111111, 0x11111111,
]

let main = () -> void => {
  vdp.init()
  vdp.set_color(0, 1, vdp.rgb(0, 7, 0))
  vdp.load_tiles(1, SOLID)
  vdp.display_on()
  var view = Camera { world_width: 1024, world_height: 224 }
  `+c.zone+`
  var x: i16 = 152
  for {
    x += 2
    camera.follow(view, x, 100, 8, 8)
    sprites.clear()
    sprites.add(Sprite { x: camera.on_screen_x(view, x), y: camera.on_screen_y(view, 100), tile: 1 })
    vdp.wait_vblank()
    sprites.show()
  }
}
`)
			screen := runROM(t, out, "160", "")
			if x0, y0, x1, y1 := screen.bounds(screen.dominant()); x0 != c.x || y0 != 100 || x1 != c.x+8 || y1 != 108 {
				t.Errorf("the sprite is drawn in [%d,%d)–[%d,%d), want [%d,100)–[%d,108)", x0, y0, x1, y1, c.x, c.x+8)
			}
		})
	}
}

// TestGenesis_ACameraScrollsBack is the scroll the other way: from (480, 96) left to x 0,
// then up to y 0. (10, 20) comes into view only by a column written going left, (30, 5)
// only by a row written going up — neither was in the plane the view started with.
func TestGenesis_ACameraScrollsBack(t *testing.T) {
	genesisToolchainOrSkip(t)
	out := genesisProgram(t, "back", `import std.genesis.vdp
import std.genesis.camera
import std.genesis.camera.{ Camera }

const SOLID: [8]u32 = #[
  0x11111111, 0x11111111, 0x11111111, 0x11111111,
  0x11111111, 0x11111111, 0x11111111, 0x11111111,
]

let main = () -> void => {
  vdp.init()
  vdp.set_color(0, 1, vdp.rgb(0, 7, 0))
  vdp.load_tiles(1, SOLID)
  var map: [3200]u16 = #[0; 3200]
  map[20 * 80 + 10] = 1
  map[5 * 80 + 30] = 1
  var view = Camera { x: 480, y: 96, last_x: 480, last_y: 96, world_width: 640, world_height: 320 }
  camera.show_map(view, vdp.PLANE_B, map, 80)
  vdp.display_on()
  for {
    view.last_x = view.x
    view.last_y = view.y
    if view.x > 0 { view.x -= 4 } else { view.y = (view.y - 2).max(0) }
    vdp.wait_vblank()
    camera.scroll_map(view, vdp.PLANE_B, map, 80)
  }
}
`)
	// (10, 20) at (80, 160) and (30, 5) at (240, 40).
	screen := runROM(t, out, "240", "")
	if x0, y0, x1, y1 := screen.bounds(screen.dominant()); x0 != 80 || y0 != 40 || x1 != 248 || y1 != 168 {
		t.Errorf("the tiles span [%d,%d)–[%d,%d), want [80,40)–[248,168)", x0, y0, x1, y1)
	}
}

// TestGenesis_ASolidTileStopsASprite walks an 8×8 sprite right, a pixel a frame, at a map
// tile with a collision shape, and after 100 frames back left: std.genesis.collision on
// the 68000 stops it touching the tile — the sprite at 88–96, the tile at 96–104 — and
// lets it walk away again. A box tile, and an ellipse one, whose test is the multiplies
// LLVM's M68k backend compiled to `muls.l`, a 68020 instruction: on the 68000 the first
// touch faulted, the runtime's red panic screen came up, and the sprite stood frozen
// (10/01, the hero at a rock). The backdrop must stay black — a panic is a test failure,
// not a sprite that happens to have stopped.
func TestGenesis_ASolidTileStopsASprite(t *testing.T) {
	genesisToolchainOrSkip(t)
	for _, c := range []struct{ name, shape string }{
		{"a box", "Shape { x: 0, y: 0, width: 8, height: 8 }"},
		{"an ellipse", "Shape { ellipse: true, x: 0, y: 0, width: 8, height: 7 }"},
	} {
		t.Run(c.name, func(t *testing.T) {
			out := genesisProgram(t, "collide", `import std.genesis.vdp
import std.genesis.sprites
import std.genesis.sprites.{ Sprite }
import std.genesis.collision
import std.genesis.collision.{ Shape }

const SOLID: [16]u32 = #[
  0x11111111, 0x11111111, 0x11111111, 0x11111111,
  0x11111111, 0x11111111, 0x11111111, 0x11111111,
  0x22222222, 0x22222222, 0x22222222, 0x22222222,
  0x22222222, 0x22222222, 0x22222222, 0x22222222,
]
const STARTS: [3]u16 = #[0, 0, 1]
const SHAPES: [1]Shape = #[`+c.shape+`]
const BODY: Shape = Shape { x: 0, y: 0, width: 8, height: 8 }

let main = () -> void => {
  vdp.init()
  vdp.set_color(0, 1, vdp.rgb(0, 7, 0))
  vdp.set_color(0, 2, vdp.rgb(7, 7, 7))
  vdp.load_tiles(1, SOLID)
  // A 20×8 map, its tile set from tile 1: the wall, the set's tile 1, at (12, 5).
  var map: [160]u16 = #[0; 160]
  map[5 * 20 + 12] = 2
  vdp.write_plane(vdp.PLANE_B, map, 20)
  vdp.display_on()
  var x: i16 = 40
  var ticks: u16 = 0
  for {
    ticks += 1
    let dx: i16 = if ticks < 100 { 1 } else { -1 }
    if !collision.hits_map(BODY, x + dx, 40, map, 20, 1, STARTS, SHAPES) { x += dx }
    sprites.clear()
    sprites.add(Sprite { x: x, y: 40, tile: 1 })
    vdp.wait_vblank()
    sprites.show()
  }
}
`)
			// At 90 frames it is held against the wall; by 170 it has walked well back —
			// how far exactly depends on when start-up ends, so only "away" is asked.
			for _, frames := range []string{"90", "170"} {
				screen := runROM(t, out, frames, "")
				if bg := screen.dominant(); bg != "000000" {
					t.Fatalf("at %s frames the screen is %s, not the black backdrop: the program panicked", frames, bg)
				}
				x0, y0, x1, y1 := spriteBounds(screen, "00ff00")
				against := x0 == 88 && x1 == 96
				if y0 != 40 || y1 != 48 || (frames == "90") != against || (frames == "170" && x0 > 60) {
					t.Errorf("at %s frames the sprite is in [%d,%d)–[%d,%d): want it against the wall [88,96) at 90, back past 60 at 170", frames, x0, y0, x1, y1)
				}
			}
		})
	}
}

// spriteBounds is the rectangle holding every pixel of colour `color`.
func spriteBounds(s screen, color string) (x0, y0, x1, y1 int) {
	x0, y0 = s.width, s.height
	for y := 0; y < s.height; y++ {
		for x := 0; x < s.width; x++ {
			k := (y*s.width + x) * 3
			if fmt.Sprintf("%02x%02x%02x", s.pixels[k], s.pixels[k+1], s.pixels[k+2]) != color {
				continue
			}
			x0, y0 = min(x0, x), min(y0, y)
			x1, y1 = max(x1, x+1), max(y1, y+1)
		}
	}
	return
}

// TestGenesis_CheckedMultipliesRunOnThe68000 multiplies run-time values — read from the
// pad, so nothing is folded — with Lyra's overflow checks: a chain of four i32 multiplies,
// which LLVM's M68k backend compiled to the 68020's `muls.l` and the 68000 faulted on;
// and an i16 multiply past i16, whose overflow flag that backend answered "never" for, so
// it wrapped in silence. The first must run (green), the second trap (the runtime's red).
func TestGenesis_CheckedMultipliesRunOnThe68000(t *testing.T) {
	genesisToolchainOrSkip(t)
	for _, c := range []struct{ name, body, want string }{
		{"four i32 multiplies run", `let zero = i32(pad.read(1))
  let a = zero - 6
  let b = zero + 7
  let ok = a * a * b * b == 1764`, "00ff00"},
		{"an i16 multiply past i16 traps", `let zero = i16(pad.read(1))
  let a = zero + 300
  let ok = a * a == 12345`, "ff0000"},
	} {
		t.Run(c.name, func(t *testing.T) {
			out := genesisProgram(t, "multiply", `import std.genesis.vdp
import std.genesis.pad

let main = () -> void => {
  vdp.init()
  pad.init()
  vdp.display_on()
  `+c.body+`
  // Green when right, blue when wrong; the runtime's panic screen is red.
  vdp.set_color(0, 0, if ok { vdp.rgb(0, 7, 0) } else { vdp.rgb(0, 0, 7) })
  for {}
}
`)
			if got := runROM(t, out, "60", "").dominant(); got != c.want {
				t.Errorf("the screen is %s, want %s", got, c.want)
			}
		})
	}
}
