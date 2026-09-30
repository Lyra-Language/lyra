package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
// and sprites together — and plays it: the hero stands in the middle with the pad let go,
// and ends pressed into the bottom-right corner with right and down held (the edge stop
// makes the place exact).
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
