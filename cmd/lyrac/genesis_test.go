package main

import (
	"bytes"
	"encoding/binary"
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

// screenOf runs rom in Sheliak for 10 frames and answers the colour most of the screen
// is, as hex RGB — or skips when there is no Sheliak to run.
func screenOf(t *testing.T, romPath string) string {
	t.Helper()
	emulator := os.Getenv("SHELIAK")
	if emulator == "" {
		t.Log("SHELIAK is not set; the ROM was built but not run")
		t.SkipNow()
	}
	ppm := romPath + ".ppm"
	cmd := exec.Command(emulator, romPath, "--frames", "10", "--ppm", ppm)
	cmd.Env = append(os.Environ(), "SDL_VIDEODRIVER=dummy")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("sheliak: %v\n%s", err, out)
	}
	data, err := os.ReadFile(ppm)
	if err != nil {
		t.Fatal(err)
	}
	// P6, width, height, 255, then RGB: the header is four whitespace-separated fields.
	fields, i := 0, 0
	for fields < 4 && i < len(data) {
		for i < len(data) && (data[i] == ' ' || data[i] == '\n') {
			i++
		}
		for i < len(data) && data[i] != ' ' && data[i] != '\n' {
			i++
		}
		fields++
	}
	pixels := data[i+1:]
	counts := map[string]int{}
	best := ""
	for k := 0; k+2 < len(pixels); k += 3 {
		c := string([]byte{hexDigit(pixels[k] >> 4), hexDigit(pixels[k] & 15), hexDigit(pixels[k+1] >> 4),
			hexDigit(pixels[k+1] & 15), hexDigit(pixels[k+2] >> 4), hexDigit(pixels[k+2] & 15)})
		counts[c]++
		if counts[c] > counts[best] {
			best = c
		}
	}
	return best
}

func hexDigit(v byte) byte { return "0123456789abcdef"[v] }
