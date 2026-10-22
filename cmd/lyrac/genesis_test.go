package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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
	t.Parallel()
	genesisToolchainOrSkip(t)
	root := repoRoot(t)
	setenv(t, "LYRA_STD", root)
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

// A function taking a struct too big for registers and returning a tuple too big for them,
// with two callers so it is not inlined: on the 68000 the M68k LLVM demoted the tuple's
// return to a hidden pointer that the caller passed in a0 and the fastcc callee read from
// d0, every argument after it a register off. The backend passes and returns every
// aggregate through memory there; the screen is green when each tuple is right.
func TestGenesis_AnAggregateCallIsRightWhenNotInlined(t *testing.T) {
	t.Parallel()
	genesisToolchainOrSkip(t)
	root := repoRoot(t)
	setenv(t, "LYRA_STD", root)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "lyra.toml"), []byte("target = \"genesis\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(dir, "tuple.lyra")
	if err := os.WriteFile(src, []byte(`import std.genesis.vdp

struct Big {
  bytes: [28]u8,
  a: u8,
  b: u8,
  c: u8,
}

/// Write k of the struct: a bank, a register, a value — the shape of the YM2612's patch writes.
let pick = pure (channel: u8, big: Big, k: i64) -> (u8, u8, u8) => {
  if k < 28 { return (channel / 3, 0x30 + u8(k % 7) * 16 + u8(k / 7) * 4, big.bytes[k]) }
  if k == 28 { return (channel / 3, 0xB0, big.a) }
  if k == 29 { return (channel / 3, 0xB4, 0xC0 | big.b) }
  (0, 0x22, big.c)
}

let status = pure () -> ^u16 => unsafe { pointer_at(0xC00004) }

let main = () -> void => {
  vdp.init()
  vdp.set_color(0, 1, vdp.rgb(7, 0, 0))
  vdp.set_color(0, 2, vdp.rgb(0, 7, 0))
  var big = Big { bytes: #[0; 28], a: 7, b: 5, c: 9 }
  for i in 0..<28 { big.bytes[i] = u8(i * 3 + 1) }
  let zero = unsafe { status().read_volatile() } & 0
  var ok = true
  for k in 0..<31 {
    let (bank, reg, value) = pick(u8(zero), big, k + i64(zero))
    let want: u8 = if k < 28 { u8(k * 3 + 1) } else if k == 28 { 7 } else if k == 29 { 0xC5 } else { 9 }
    if bank != 0 || value != want { ok = false }
    if k == 0 && reg != 0x30 { ok = false }
  }
  for k in 0..<31 {
    let (bank, _, value) = pick(u8(zero) + 3, big, k)
    if k < 28 && (bank != 1 || value != u8(k * 3 + 1)) { ok = false }
  }
  vdp.set_backdrop(0, if ok { 2 } else { 1 })
  vdp.display_on()
  loop {}
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "tuple.bin")
	if _, stderr, code := captureRun(t, "build", "-o", out, src); code != 0 {
		t.Fatalf("build exited %d: %s", code, stderr)
	}
	if got := runROM(t, out, "60", "").dominant(); got != "00ff00" {
		t.Errorf("the screen is %s, want the green of every tuple right (00ff00)", got)
	}
}

func TestGenesis_APanicTurnsTheScreenRed(t *testing.T) {
	t.Parallel()
	genesisToolchainOrSkip(t)
	root := repoRoot(t)
	setenv(t, "LYRA_STD", root)
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

// An index out of bounds writes its index and length with lyra_trap_write_int, whose
// 64-bit division the 68000 has no instruction for: the ROM must link it from
// compiler-rt and still reach the panic screen.
func TestGenesis_AnIndexOutOfBoundsTurnsTheScreenRed(t *testing.T) {
	t.Parallel()
	genesisToolchainOrSkip(t)
	root := repoRoot(t)
	setenv(t, "LYRA_STD", root)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "lyra.toml"), []byte("target = \"genesis\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(dir, "index.lyra")
	if err := os.WriteFile(src, []byte(`let status = pure () -> ^u16 => unsafe { pointer_at(0xC00004) }
let port = pure () -> ^mut u16 => unsafe { pointer_at(0xC00000) }
let main = () -> void => {
  let xs: [2]u16 = #[1, 2]
  let s = unsafe { status().read_volatile() }
  unsafe { port().write_volatile(xs[i64(s & 1) + 2]) }
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "index.bin")
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
	t.Parallel()
	genesisToolchainOrSkip(t)
	root := repoRoot(t)
	setenv(t, "LYRA_STD", root)
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
	t.Parallel()
	genesisToolchainOrSkip(t)
	root := repoRoot(t)
	setenv(t, "LYRA_STD", root)
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
	cmd := command(t, emulator, args...)
	cmd.Env = append(cmd.Env, "SDL_VIDEODRIVER=dummy")
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
	t.Parallel()
	genesisToolchainOrSkip(t)
	root := repoRoot(t)
	setenv(t, "LYRA_STD", root)
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
	t.Parallel()
	genesisToolchainOrSkip(t)
	root := repoRoot(t)
	setenv(t, "LYRA_STD", root)
	// An empty cache, wherever os.UserCacheDir looks on this system — the toolchain,
	// found under the real home by default, named first.
	tc, _ := findM68kToolchain()
	setenv(t, "LYRA_M68K_LLVM", tc.root)
	home := t.TempDir()
	setenv(t, "HOME", home)
	setenv(t, "XDG_CACHE_HOME", filepath.Join(home, ".cache"))
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
	t.Parallel()
	genesisToolchainOrSkip(t)
	root := repoRoot(t)
	setenv(t, "LYRA_STD", root)
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
  loop {}
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
	t.Parallel()
	genesisToolchainOrSkip(t)
	root := repoRoot(t)
	setenv(t, "LYRA_STD", root)
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
  loop {}
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

// TestGenesis_APlaneDrawnUnderTheVBlankInterruptKeepsEveryCell draws two planes whole with
// the display off and the vertical blank interrupt on — its handler writing the sprite
// list, as every Vega game's does — the second long enough that the interrupt falls due
// partway. The handler's writes move the VDP's address, so a row they landed in went on at
// theirs and lost the rest of its cells: black bars across the platformer's level (10/06).
// vdp's writers hold the interrupt off for their runs ([vdp.hold_vblank]). Every cell must
// be green — tile 1's colour, set only once a later blank has come, so an interrupt held
// and never let go fails too, the screen left red.
func TestGenesis_APlaneDrawnUnderTheVBlankInterruptKeepsEveryCell(t *testing.T) {
	t.Parallel()
	genesisToolchainOrSkip(t)
	rom := genesisProgram(t, "race", `import std.genesis.vdp
import std.genesis.sprites
import std.genesis.camera.{ Camera }

const SOLID: [8]u32 = #[
  0x11111111, 0x11111111, 0x11111111, 0x11111111,
  0x11111111, 0x11111111, 0x11111111, 0x11111111,
]
const FULL: [2304]u16 = #[0x2001; 2304]
const EMPTY: [2304]u16 = #[0; 2304]

var blanks: u16 = 0

@interrupt(vblank)
let each_frame = () -> void => {
  sprites.show()
  blanks = blanks.wrapping_add(1)
}

let main = () -> void => {
  vdp.init()
  vdp.set_color(1, 1, vdp.rgb(7, 0, 0))
  vdp.load_tiles(1, SOLID)
  vdp.display_on()
  vdp.enable_vblank_interrupt()
  let view = Camera { world_width: 512, world_height: 288 }
  let seen = blanks
  while blanks == seen {}
  vdp.display_off()
  view.show_map(vdp.PLANE_A, EMPTY, 64)
  view.show_map(vdp.PLANE_B, FULL, 64)
  vdp.display_on()
  let drawn = blanks
  while blanks == drawn {}
  vdp.set_color(1, 1, vdp.rgb(0, 7, 0))
  loop {}
}
`)
	screen := runROM(t, rom, "120", "")
	for k := 0; k+2 < len(screen.pixels); k += 3 {
		if screen.pixels[k] != 0 || screen.pixels[k+1] != 0xff || screen.pixels[k+2] != 0 {
			x, y := (k/3)%screen.width, (k/3)/screen.width
			t.Fatalf("pixel (%d, %d) is %02x%02x%02x, not green: a cell lost, or the interrupt never back", x, y, screen.pixels[k], screen.pixels[k+1], screen.pixels[k+2])
		}
	}
}

// genesisProgram builds `source` as a Genesis program and answers the ROM's path.
func genesisProgram(t *testing.T, name, source string) string {
	t.Helper()
	root := repoRoot(t)
	setenv(t, "LYRA_STD", root)
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
	t.Parallel()
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
  loop {
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
	t.Parallel()
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
  loop {
    x += 2
    view.follow(x, 100, 8, 8)
    let (sx, sy) = view.on_screen(x, 100)
    sprites.clear()
    sprites.add(Sprite { x: sx, y: sy, tile: 1 })
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
	t.Parallel()
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
  loop {
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
// not a sprite that happens to have stopped. Then by colliders (10/02): a tile on a layer
// the sprite's collider looks for stops it, and one on another layer does not.
func TestGenesis_ASolidTileStopsASprite(t *testing.T) {
	t.Parallel()
	genesisToolchainOrSkip(t)
	// The collider cases: the wall's tile is on layer 2 (water); a collider looking for it
	// stops there, one looking only for layer 1 (walls) walks through.
	const box = "Shape { x: 0, y: 0, width: 8, height: 8 }"
	const tileOnWater = "Collider { shape: " + box + ", layers: 2, mask: 0 }"
	for _, c := range []struct {
		name, shapesType, shape, bodyType, body string
		blocked                                 bool
	}{
		{"a box", "Shape", box, "Shape", box, true},
		{"an ellipse", "Shape", "Shape { ellipse: true, x: 0, y: 0, width: 8, height: 7 }", "Shape", box, true},
		{"a collider looking for its layer", "Collider", tileOnWater, "Collider", "Collider { shape: " + box + ", layers: 4, mask: 2 }", true},
		{"a collider looking past its layer", "Collider", tileOnWater, "Collider", "Collider { shape: " + box + ", layers: 4, mask: 1 }", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			out := genesisProgram(t, "collide", `import std.genesis.vdp
import std.genesis.sprites
import std.genesis.sprites.{ Sprite }
import std.genesis.collision
import std.genesis.collision.{ Shape, Collider }

const SOLID: [16]u32 = #[
  0x11111111, 0x11111111, 0x11111111, 0x11111111,
  0x11111111, 0x11111111, 0x11111111, 0x11111111,
  0x22222222, 0x22222222, 0x22222222, 0x22222222,
  0x22222222, 0x22222222, 0x22222222, 0x22222222,
]
const STARTS: [3]u16 = #[0, 0, 1]
const SHAPES: [1]`+c.shapesType+` = #[`+c.shape+`]
const BODY: `+c.bodyType+` = `+c.body+`

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
  loop {
    ticks += 1
    let dx: i16 = if ticks < 100 { 1 } else { -1 }
    if !BODY.hits_map(x + dx, 40, map, 20, 1, STARTS, SHAPES) { x += dx }
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
				if !c.blocked {
					// Looking past the wall's layer: at 90 frames it is through, beyond it.
					if frames == "90" && x0 <= 96 {
						t.Errorf("at 90 frames the sprite is at x %d: a collider not looking for the wall's layer should have walked through it", x0)
					}
					continue
				}
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
	t.Parallel()
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
  loop {}
}
`)
			if got := runROM(t, out, "60", "").dominant(); got != c.want {
				t.Errorf("the screen is %s, want %s", got, c.want)
			}
		})
	}
}

// TestGenesis_ArithmeticHelpersAgreeWithTheHost runs the runtime's own 32-bit division and
// 32- and 64-bit multiplies (runtime.c, `divu` and `mulu` where compiler-rt's C was some
// thousands of cycles each) on operands of every width — divisors under 16 bits and past
// them, signs both ways — and folds the answers into one word, which must be the host's.
func TestGenesis_ArithmeticHelpersAgreeWithTheHost(t *testing.T) {
	t.Parallel()
	genesisToolchainOrSkip(t)
	seed := uint32(2463534242)
	next := func() uint32 {
		seed ^= seed << 13
		seed ^= seed >> 17
		seed ^= seed << 5
		return seed
	}
	signed := func(v uint32) int32 {
		if v&1 == 1 {
			return -int32(v >> 1)
		}
		return int32(v >> 1)
	}
	var sum uint32
	for range 300 {
		shape := next()
		a := next() >> (shape & 31)
		b := max(next()>>((shape>>5)&31), 1)
		sa, sb := signed(a), signed(b)
		if sb == 0 {
			sb = 1
		}
		// Each remainder of other operands than a division's: LLVM makes the remainder of a
		// division's own `a - (a/b)·b`, and __modsi3 went untested.
		sum ^= a/b ^ ((a>>1)%b)<<3
		sum ^= uint32(sa/sb) ^ uint32((sa/2)%sb)<<7
		sum ^= a * b
		p := int64(sa) * int64(sb)
		sum ^= uint32(p) ^ uint32(p>>32)
		q := uint64(a) * (uint64(b)*3 + 1)
		sum ^= uint32(q) ^ uint32(q>>32)
	}
	out := genesisProgram(t, "arithmetic", fmt.Sprintf(`import std.genesis.vdp

var seed: u32 = 2463534242

let main = () -> void => {
  vdp.init()
  vdp.display_on()
  var sum: u32 = 0
  for _: i16 in 0..<300 {
    let shape = next()
    let a = next() >> (shape & 31)
    let b = (next() >> ((shape >> 5) & 31)).max(1)
    let sa = signed(a)
    let sb = if signed(b) == 0 { 1 } else { signed(b) }
    sum = sum ~ (a / b) ~ (((a >> 1) %% b) << 3)
    sum = sum ~ word(i64(sa / sb)) ~ (word(i64((sa / 2) %% sb)) << 7)
    sum = sum ~ a.wrapping_mul(b)
    let p = i64(sa).wrapping_mul(i64(sb))
    sum = sum ~ word(p) ~ word(p >> 32)
    let q = u64(a).wrapping_mul(u64(b) * 3 + 1)
    sum = sum ~ u32(q & 0xFFFFFFFF) ~ u32(q >> 32)
  }
  // Green when right, blue when wrong; the runtime's panic screen is red.
  vdp.set_color(0, 0, if sum == %d { vdp.rgb(0, 7, 0) } else { vdp.rgb(0, 0, 7) })
  loop {}
}

let next = () -> u32 => {
  seed = seed ~ (seed << 13)
  seed = seed ~ (seed >> 17)
  seed = seed ~ (seed << 5)
  seed
}

let signed = (v: u32) -> i32 => if v & 1 == 1 { -i32(v >> 1) } else { i32(v >> 1) }

/// The low 32 bits of v.
let word = (v: i64) -> u32 => u32(v & 0xFFFFFFFF)
`, sum))
	if got := runROM(t, out, "60", "").dominant(); got != "00ff00" {
		t.Errorf("the screen is %s, want 00ff00 (blue: an answer differs from the host's)", got)
	}
}

// TestGenesis_WriteCellSetsOneCell puts one tile into plane A's cell (2, 1) with
// vdp.write_cell: it must show at pixels 16–24 across and 8–16 down, and nothing else.
func TestGenesis_WriteCellSetsOneCell(t *testing.T) {
	t.Parallel()
	genesisToolchainOrSkip(t)
	out := genesisProgram(t, "cell", `import std.genesis.vdp

const SOLID: [8]u32 = #[
  0x11111111, 0x11111111, 0x11111111, 0x11111111,
  0x11111111, 0x11111111, 0x11111111, 0x11111111,
]

let main = () -> void => {
  vdp.init()
  vdp.set_color(0, 1, vdp.rgb(0, 7, 0))
  vdp.load_tiles(1, SOLID)
  vdp.write_cell(vdp.PLANE_A, 2, 1, 1)
  vdp.display_on()
  loop {}
}
`)
	screen := runROM(t, out, "60", "")
	if x0, y0, x1, y1 := screen.bounds("000000"); x0 != 16 || y0 != 8 || x1 != 24 || y1 != 16 {
		t.Errorf("the cell is drawn in [%d,%d)–[%d,%d), want [16,8)–[24,16)", x0, y0, x1, y1)
	}
}

// TestGenesis_TheWindowDoesNotScroll shows the window over the top two rows: a cell written
// to it stays at pixels 16–24 across and 8–16 down while plane A scrolls 40 pixels, and plane
// A's own cell in that row is not shown at all — the window is drawn there instead. Where
// a game keeps its score once plane A is a map (vdp.window_rows, 10/02).
func TestGenesis_TheWindowDoesNotScroll(t *testing.T) {
	t.Parallel()
	genesisToolchainOrSkip(t)
	out := genesisProgram(t, "window", `import std.genesis.vdp

const TILES: [16]u32 = #[
  0x11111111, 0x11111111, 0x11111111, 0x11111111,
  0x11111111, 0x11111111, 0x11111111, 0x11111111,
  0x22222222, 0x22222222, 0x22222222, 0x22222222,
  0x22222222, 0x22222222, 0x22222222, 0x22222222,
]

let main = () -> void => {
  vdp.init()
  vdp.set_color(0, 1, vdp.rgb(0, 7, 0))
  vdp.set_color(0, 2, vdp.rgb(7, 0, 0))
  vdp.load_tiles(1, TILES)
  vdp.write_cell(vdp.WINDOW, 2, 1, 1)
  vdp.write_cell(vdp.PLANE_A, 2, 1, 2)
  vdp.set_scroll(vdp.PLANE_A, 40, 0)
  vdp.window_rows(2)
  vdp.display_on()
  loop {}
}
`)
	screen := runROM(t, out, "60", "")
	if x0, y0, x1, y1 := spriteBounds(screen, "00ff00"); x0 != 16 || y0 != 8 || x1 != 24 || y1 != 16 {
		t.Errorf("the window's cell is drawn in [%d,%d)–[%d,%d), want [16,8)–[24,16)", x0, y0, x1, y1)
	}
	if x0, _, x1, _ := spriteBounds(screen, "ff0000"); x1 > x0 {
		t.Errorf("plane A shows under the window, from x %d", x0)
	}
}

// TestGenesis_NoExtendIntoAnAddressRegister compiles, with the toolchain's llc, the IR
// llvm-reduce cut from the hero's game: a 16-bit value shifted and zero-extended for a
// call. LLVM's M68k backend let that extension's destination be an address register and
// expanded it as `and.l #65535, %aN` — no 68000 instruction; written to an object file it
// is another one, and the hero's position came out as garbage once his game grew stars
// (10/01). Patch 0003 keeps the 16→32 extensions to data registers.
func TestGenesis_NoExtendIntoAnAddressRegister(t *testing.T) {
	t.Parallel()
	genesisToolchainOrSkip(t)
	tc, err := findM68kToolchain()
	if err != nil {
		t.Fatal(err)
	}
	ll := filepath.Join(t.TempDir(), "extend.ll")
	if err := os.WriteFile(ll, []byte(`target datalayout = "E-m:e-p:32:16:32-i8:8:8-i16:16:16-i32:16:32-n8:16:32-a:0:16-S16"
target triple = "m68k-unknown-unknown-elf"

%Shape = type { i1, i16, i16, i16, i16 }

define fastcc i1 @extend(%Shape %p.shape) {
entry:
  %y = extractvalue %Shape %p.shape, 2
  %0 = shl i16 %y, 1
  %1 = tail call fastcc i1 null(%Shape %p.shape, i16 0, i16 0, %Shape zeroinitializer, i16 0, i16 %0)
  ret i1 %1
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	asm, err := command(t, tc.tool("llc"), "-mtriple=m68k-unknown-elf", "-mcpu=M68000", "-O2", ll, "-o", "-").CombinedOutput()
	if err != nil {
		t.Fatalf("llc: %v\n%s", err, asm)
	}
	if bad := regexp.MustCompile(`and\.[lw]\s+#\d+, %a[0-7]`).Find(asm); bad != nil {
		t.Errorf("llc extended into an address register (%q): rebuild it with tools/llvm-m68k.sh's patches", bad)
	}
}

// TestGenesis_ARegisterStructReturnIsNotPopped holds llc to tools/llvm-m68k-patches/0004: a
// `fastcc` function returning a struct through memory gets its hidden pointer in A0, but
// returned popping four bytes for it as if the caller had pushed it, and its caller did not
// make them up — the stack pointer crept up a word a call, into the caller's locals, until a
// count kept there read garbage and the music player ran past its buffer (10/16; seen once
// LLVM inlined less, as a large game makes it).
func TestGenesis_ARegisterStructReturnIsNotPopped(t *testing.T) {
	t.Parallel()
	genesisToolchainOrSkip(t)
	tc, err := findM68kToolchain()
	if err != nil {
		t.Fatal(err)
	}
	ll := filepath.Join(t.TempDir(), "sret.ll")
	if err := os.WriteFile(ll, []byte(`target datalayout = "E-m:e-p:32:16:32-i8:8:8-i16:16:16-i32:16:32-n8:16:32-a:0:16-S16"
target triple = "m68k-unknown-unknown-elf"

%Pair = type { i32, i32 }

define internal fastcc void @make(ptr sret(%Pair) %out, i32 %n) noinline {
entry:
  store i32 %n, ptr %out
  %b = getelementptr i8, ptr %out, i32 4
  store i32 %n, ptr %b
  ret void
}

define void @caller(ptr %p, i32 %n) {
entry:
  call fastcc void @make(ptr sret(%Pair) %p, i32 %n)
  ret void
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	asm, err := command(t, tc.tool("llc"), "-mtriple=m68k-unknown-elf", "-mcpu=M68000", "-O2", ll, "-o", "-").CombinedOutput()
	if err != nil {
		t.Fatalf("llc: %v\n%s", err, asm)
	}
	text := string(asm)
	start := strings.Index(text, "make:")
	end := strings.Index(text[start:], "rts")
	if start < 0 || end < 0 {
		t.Fatalf("no make in llc's output:\n%s", asm)
	}
	if body := text[start : start+end]; strings.Contains(body, "#4, %sp") {
		t.Errorf("make pops a struct-return pointer passed in A0: rebuild llc with tools/llvm-m68k.sh's patches\n%s", body)
	}
}

// A large struct returned through memory and passed by copy runs on the 68000. Its copies
// called a `memcpy(i8*, i8*, i64)` the backend declared, and the runtime's reads a 32-bit
// `size_t`: the eight bytes pushed were read as their high half, 0, so nothing was copied and
// the struct came back as whatever its slot held. The copies are LLVM's `llvm.memcpy` and
// `llvm.memset` now, which take the target's own length (10/11).
func TestGenesis_ALargeStructReturnsAndPassesThroughMemory(t *testing.T) {
	t.Parallel()
	genesisToolchainOrSkip(t)
	out := genesisProgram(t, "large", `import std.genesis.vdp
import std.genesis.pad

struct Big {
  cells: [70]i32,
  tag: i32,
}

let make = pure (n: i32) -> Big => {
  var b = Big { cells: #[0; 70], tag: n }
  for i: u16 in 0..<70 { b.cells[i] = n + i32(i) }
  b
}

let last = pure (b: Big) -> i32 => b.cells[69] + b.tag

let main = () -> void => {
  vdp.init()
  pad.init()
  vdp.display_on()
  let zero = i32(pad.read(1))
  let b = make(zero + 5)
  let right = b.tag == 5 && b.cells[0] == 5 && b.cells[69] == 74 && last(b) == 79
  // Green when right, blue when wrong; the runtime's panic screen is red.
  vdp.set_color(0, 0, if right { vdp.rgb(0, 7, 0) } else { vdp.rgb(0, 0, 7) })
  loop {}
}
`)
	if got := runROM(t, out, "60", "").dominant(); got != "00ff00" {
		t.Errorf("the screen is %s, want 00ff00", got)
	}
}
